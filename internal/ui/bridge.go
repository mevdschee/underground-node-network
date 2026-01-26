package ui

import (
	"io"
	"log"
	"sync"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/crypto/ssh"
)

// InputBridge manages a single background pump from an ssh.Channel
// and provides bytes to multiple consecutive consumers.
type InputBridge struct {
	channel  ssh.Channel
	dataChan chan byte
	err      error
	mu       sync.Mutex
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
			for i := 0; i < n; i++ {
				b.dataChan <- buf[i]
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
			log.Printf("[DEBUG] InputBridge.Read: Closed (err: %v)", b.err)
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
				log.Printf("[DEBUG] InputBridge.Read: Returning %d bytes", n)
				return n, nil
			}
		}
		log.Printf("[DEBUG] InputBridge.Read: Returning %d bytes (full buffer)", n)
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
		log.Printf("[DEBUG] SSHBus.Read: Early exit via doneChan")
		return 0, io.EOF
	default:
		// Check for data but always respect doneChan
		select {
		case <-b.doneChan:
			log.Printf("[DEBUG] SSHBus.Read: Exit via doneChan (priority)")
			return 0, io.EOF
		case data, ok := <-b.bridge.dataChan:
			if !ok {
				b.bridge.mu.Lock()
				defer b.bridge.mu.Unlock()
				log.Printf("[DEBUG] SSHBus.Read: Exit via bridge.dataChan closed (err: %v)", b.bridge.err)
				return 0, b.bridge.err
			}
			p[0] = data
			n := 1
			// Fill as much as possible without blocking
			for n < len(p) {
				select {
				case <-b.doneChan:
					log.Printf("[DEBUG] SSHBus.Read: Partial return (%d bytes) via doneChan", n)
					return n, nil
				case data, ok := <-b.bridge.dataChan:
					if !ok {
						log.Printf("[DEBUG] SSHBus.Read: Partial return (%d bytes) via bridge closed", n)
						return n, nil
					}
					p[n] = data
					n++
				default:
					log.Printf("[DEBUG] SSHBus.Read: Returning %d bytes", n)
					return n, nil
				}
			}
			log.Printf("[DEBUG] SSHBus.Read: Returning %d bytes (full buffer)", n)
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
	log.Printf("[DEBUG] SSHBus.SignalExit() called")
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.doneChan:
		log.Printf("[DEBUG] SSHBus.SignalExit(): Already closed")
	default:
		close(b.doneChan)
		log.Printf("[DEBUG] SSHBus.SignalExit(): Closed doneChan")
	}
}

func (b *SSHBus) Reset() {
	log.Printf("[DEBUG] SSHBus.Reset() called")
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.doneChan:
		b.doneChan = make(chan struct{})
		log.Printf("[DEBUG] SSHBus.Reset(): Recreated doneChan")
	default:
		log.Printf("[DEBUG] SSHBus.Reset(): Was not closed")
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
