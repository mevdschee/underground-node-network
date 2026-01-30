package ui

import (
	"encoding/binary"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

// DrawText renders a string at (x, y) on the screen, respecting visual width and grapheme clusters.
func DrawText(s tcell.Screen, x, y int, text string, width int, style tcell.Style) {
	if s == nil {
		return
	}
	gr := uniseg.NewGraphemes(text)
	posX := 0
	for gr.Next() {
		str := gr.Str()
		w := uniseg.StringWidth(str)
		if posX+w > width {
			break
		}
		// SetContent handles multi-rune clusters if the first rune and subsequent ones are provided.
		runes := []rune(str)
		s.SetContent(x+posX, y, runes[0], runes[1:], style)
		posX += w
	}
	// Clear remaining width
	for i := posX; i < width; i++ {
		s.SetContent(x+i, y, ' ', nil, style)
	}
}

func truncateString(s string, limit int) string {
	if uniseg.StringWidth(s) <= limit {
		return s
	}

	if limit <= 0 {
		return ""
	}
	if limit == 1 {
		return "…"
	}

	res := ""
	width := 0
	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		w := uniseg.StringWidth(gr.Str())
		if width+w > limit-1 {
			return res + "…"
		}
		res += gr.Str()
		width += w
	}
	return res + "…"
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
	if s == "" {
		return []string{""}
	}
	if width <= 0 {
		return []string{s}
	}

	var lines []string
	remaining := s
	first := true

	for remaining != "" {
		effWidth := width
		indent := ""
		if !first && width > 2 {
			effWidth = width - 2
			indent = "  "
		}

		if uniseg.StringWidth(remaining) <= effWidth {
			lines = append(lines, indent+remaining)
			break
		}

		// Look for last space within effWidth
		breakIdx := -1
		widthSoFar := 0
		lastSpaceIdx := -1
		byteIdx := 0

		gr := uniseg.NewGraphemes(remaining)
		for gr.Next() {
			str := gr.Str()
			w := uniseg.StringWidth(str)
			if widthSoFar+w > effWidth {
				break
			}
			if str == " " {
				lastSpaceIdx = byteIdx
			}
			widthSoFar += w
			byteIdx += len(str)
		}

		if lastSpaceIdx != -1 {
			breakIdx = lastSpaceIdx
		} else {
			// No space found, hard break
			breakIdx = byteIdx
		}

		if breakIdx == 0 && len(remaining) > 0 {
			// Force at least one grapheme to avoid infinite loop
			gr := uniseg.NewGraphemes(remaining)
			if gr.Next() {
				breakIdx = len(gr.Str())
			}
		}

		line := remaining[:breakIdx]
		trimmed := strings.TrimSpace(line)
		if trimmed != "" || !first { // Don't add empty lines from leading spaces
			lines = append(lines, indent+trimmed)
		}

		remaining = remaining[breakIdx:]
		// Skip leading spaces on next line
		remaining = strings.TrimLeft(remaining, " ")
		first = false
	}

	// Filter out empty lines at the end if the original wasn't empty
	if len(lines) > 1 {
		last := len(lines) - 1
		for last >= 0 && lines[last] == "  " {
			lines = lines[:last]
			last--
		}
	}

	if len(lines) == 0 {
		return []string{""}
	}

	return lines
}
func IsAlphanumeric(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
