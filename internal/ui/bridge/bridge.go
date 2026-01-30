package bridge

import (
	"encoding/json"
	"io"
	"strings"
	"sync"

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

// OSCDetector wraps an io.Writer to intercept OSC sequences from doors
type OSCDetector struct {
	w       io.Writer
	handler func(action string, params map[string]interface{})
	buf     strings.Builder
	inOSC   bool
}

func NewOSCDetector(w io.Writer, handler func(action string, params map[string]interface{})) *OSCDetector {
	return &OSCDetector{
		w:       w,
		handler: handler,
	}
}

func (d *OSCDetector) Write(p []byte) (n int, err error) {
	for i := 0; i < len(p); i++ {
		b := p[i]
		if !d.inOSC {
			if b == 0x1b {
				d.inOSC = true
				d.buf.Reset()
				d.buf.WriteByte(b)
				continue
			}
			if _, err := d.w.Write([]byte{b}); err != nil {
				return i, err
			}
		} else {
			d.buf.WriteByte(b)
			if d.buf.Len() == 2 && d.buf.String() != "\x1b]" {
				// Not an OSC sequence after all
				d.inOSC = false
				if _, err := d.w.Write([]byte(d.buf.String())); err != nil {
					return i, err
				}
				continue
			}
			if b == 0x07 { // BEL - terminator
				d.inOSC = false
				oscStr := d.buf.String()
				if strings.HasPrefix(oscStr, "\x1b]9;") {
					jsonStr := strings.TrimPrefix(oscStr, "\x1b]9;")
					jsonStr = strings.TrimSuffix(jsonStr, "\x07")
					var payload map[string]interface{}
					if err := json.Unmarshal([]byte(jsonStr), &payload); err == nil {
						if action, ok := payload["action"].(string); ok {
							delete(payload, "action")
							d.handler(action, payload)
						}
					}
				} else {
					if _, err := d.w.Write([]byte(oscStr)); err != nil {
						return i, err
					}
				}
			}
		}
	}
	return len(p), nil
}
