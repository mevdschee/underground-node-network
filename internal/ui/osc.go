package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SendOSC sends an OSC 9 sequence with a JSON payload
func SendOSC(w io.Writer, action string, params map[string]interface{}) error {
	payload := make(map[string]interface{})
	payload["action"] = action
	for k, v := range params {
		payload[k] = v
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "\x1b]9;%s\x07", string(jsonData))
	return err
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
