package bridge

import (
	"io"
	"sync"

	"github.com/gdamore/tcell/v2"
)

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

	select {
	case <-b.doneChan:
		return 0, io.EOF
	default:
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
