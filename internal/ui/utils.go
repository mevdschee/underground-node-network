package ui

import (
	"encoding/binary"
	"strings"
)

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

func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	runes := []rune(s)
	if len(runes) <= width {
		return []string{s}
	}

	var lines []string
	first := true
	for len(runes) > 0 {
		effWidth := width
		indent := ""
		if !first && width > 2 {
			effWidth = width - 2
			indent = "  "
		}
		if effWidth < 1 {
			effWidth = 1
		}

		if len(runes) <= effWidth {
			lines = append(lines, indent+string(runes))
			break
		}

		// Look for last space within effWidth
		breakIdx := -1
		for i := 0; i < effWidth; i++ {
			if runes[i] == ' ' {
				breakIdx = i
			}
		}

		if breakIdx == -1 {
			// No space found, hard break at effWidth
			breakIdx = effWidth
		}

		lines = append(lines, indent+strings.TrimSpace(string(runes[:breakIdx])))
		runes = runes[breakIdx:]
		// Skip leading spaces on next line
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
		first = false
	}
	return lines
}
