package ui

import "encoding/binary"

func truncateString(s string, limit int) string {
	runes := []rune(s)
	if len(runes) > limit {
		if limit > 1 {
			return string(runes[:limit-1]) + "…"
		}
		return "…"
	}
	return s
}

// ParsePtyRequest parses the width and height from an SSH pty-req payload
func ParsePtyRequest(payload []byte) (uint32, uint32, bool) {
	if len(payload) < 4 {
		return 0, 0, false
	}
	termLen := binary.BigEndian.Uint32(payload[:4])
	if uint32(len(payload)) < 4+termLen+8 {
		return 0, 0, false
	}
	w := binary.BigEndian.Uint32(payload[4+termLen : 4+termLen+4])
	h := binary.BigEndian.Uint32(payload[4+termLen+4 : 4+termLen+8])
	return w, h, true
}

// ParseWindowChange parses the width and height from an SSH window-change payload
func ParseWindowChange(payload []byte) (uint32, uint32, bool) {
	if len(payload) < 8 {
		return 0, 0, false
	}
	w := binary.BigEndian.Uint32(payload[0:4])
	h := binary.BigEndian.Uint32(payload[4:8])
	return w, h, true
}
