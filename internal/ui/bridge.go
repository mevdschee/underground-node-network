package ui

import (
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/crypto/ssh"
)

// InputBridge manages a single background pump from an ssh.Channel
// and provides bytes to multiple consecutive consumers.
type InputBridge struct {
	channel    ssh.Channel
	dataChan   chan byte
	err        error
	mu         sync.Mutex
	oscHandler func(action string, params map[string]interface{})
	oscBuf     strings.Builder
	inOSC      bool
}

func NewInputBridge(channel ssh.Channel) *InputBridge {
	b := &InputBridge{
		channel:  channel,
		dataChan: make(chan byte, 2048),
	}
	go b.pump()
	return b
}

func (b *InputBridge) pump() {
	buf := make([]byte, 1024)
	for {
		n, err := b.channel.Read(buf)
		if n > 0 {
			b.mu.Lock()
			handler := b.oscHandler
			b.mu.Unlock()

			for i := 0; i < n; i++ {
				charByte := buf[i]
				if !b.inOSC {
					if charByte == 0x1b {
						b.inOSC = true
						b.oscBuf.Reset()
						b.oscBuf.WriteByte(charByte)
						continue
					}
					b.dataChan <- charByte
				} else {
					b.oscBuf.WriteByte(charByte)
					if b.oscBuf.Len() == 2 && b.oscBuf.String() != "\x1b]" {
						// Not an OSC sequence after all, push what we have
						b.inOSC = false
						for _, char := range b.oscBuf.String() {
							b.dataChan <- byte(char)
						}
						continue
					}
					if charByte == 0x07 { // BEL - terminator
						b.inOSC = false
						oscStr := b.oscBuf.String()
						if strings.HasPrefix(oscStr, "\x1b]9;") && handler != nil {
							jsonStr := strings.TrimPrefix(oscStr, "\x1b]9;")
							jsonStr = strings.TrimSuffix(jsonStr, "\x07")
							var payload map[string]interface{}
							if err := json.Unmarshal([]byte(jsonStr), &payload); err == nil {
								if action, ok := payload["action"].(string); ok {
									delete(payload, "action")
									handler(action, payload)
								}
							}
						} else {
							// Not our OSC sequence, push it back
							for _, char := range oscStr {
								b.dataChan <- byte(char)
							}
						}
					}
				}
			}
		}
		if err != nil {
			b.mu.Lock()
			b.err = err
			b.mu.Unlock()
			close(b.dataChan)
			return
		}
	}
}

func (b *InputBridge) SetOSCHandler(handler func(action string, params map[string]interface{})) {
	b.mu.Lock()
	b.oscHandler = handler
	b.mu.Unlock()
}

func (b *InputBridge) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for {
		select {
		case <-b.dataChan:
		default:
			return
		}
	}
}

func (b *InputBridge) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	select {
	case data, ok := <-b.dataChan:
		if !ok {
			b.mu.Lock()
			defer b.mu.Unlock()
			return 0, b.err
		}
		p[0] = data
		n := 1
		// Try to fill the rest of the buffer without blocking
		for n < len(p) {
			select {
			case data, ok := <-b.dataChan:
				if !ok {
					return n, nil
				}
				p[n] = data
				n++
			default:
				return n, nil
			}
		}
		return n, nil
	}
}

// SSHBus implements tcell.Tty for an ssh.Channel by reading from an InputBridge
type SSHBus struct {
	bridge   *InputBridge
	width    int
	height   int
	resize   chan struct{}
	mu       sync.Mutex
	cb       func()
	doneChan chan struct{}
}

func NewSSHBus(bridge *InputBridge, width, height int) *SSHBus {
	return &SSHBus{
		bridge:   bridge,
		width:    width,
		height:   height,
		resize:   make(chan struct{}, 1),
		doneChan: make(chan struct{}),
	}
}

func (b *SSHBus) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	select {
	case <-b.doneChan:
		return 0, io.EOF
	default:
	}

	// Double select to prioritize the done signal
	select {
	case <-b.doneChan:
		return 0, io.EOF
	default:
		// Check for data but always respect doneChan
		select {
		case <-b.doneChan:
			return 0, io.EOF
		case data, ok := <-b.bridge.dataChan:
			if !ok {
				b.bridge.mu.Lock()
				err := b.bridge.err
				b.bridge.mu.Unlock()
				return 0, err
			}
			p[0] = data
			n := 1
			// Fill as much as possible without blocking
			for n < len(p) {
				select {
				case <-b.doneChan:
					return n, nil
				case data, ok := <-b.bridge.dataChan:
					if !ok {
						return n, nil
					}
					p[n] = data
					n++
				default:
					return n, nil
				}
			}
			return n, nil
		}
	}
}

func (b *SSHBus) Write(p []byte) (int, error) {
	return b.bridge.channel.Write(p)
}

func (b *SSHBus) Close() error {
	b.SignalExit()
	return nil
}

func (b *SSHBus) SignalExit() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.doneChan:
	default:
		close(b.doneChan)
	}
}

func (b *SSHBus) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.doneChan:
		b.doneChan = make(chan struct{})
	default:
	}
}

func (b *SSHBus) ForceClose() error {
	b.SignalExit()
	return b.bridge.channel.Close()
}

func (b *SSHBus) Start() error { return nil }
func (b *SSHBus) Stop() error  { return nil }
func (b *SSHBus) Drain() error { return nil }

func (b *SSHBus) WindowSize() (tcell.WindowSize, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return tcell.WindowSize{Width: b.width, Height: b.height}, nil
}

func (b *SSHBus) NotifyResize(cb func()) {
	b.mu.Lock()
	b.cb = cb
	b.mu.Unlock()
}

func (b *SSHBus) Resize(w, h int) {
	b.mu.Lock()
	b.width = w
	b.height = h
	cb := b.cb
	b.mu.Unlock()
	if cb != nil {
		cb()
	}
}
