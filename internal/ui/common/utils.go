package common

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
		runes := []rune(str)
		s.SetContent(x+posX, y, runes[0], runes[1:], style)
		posX += w
	}
	// Clear remaining width
	for i := posX; i < width; i++ {
		s.SetContent(x+i, y, ' ', nil, style)
	}
}

func TruncateString(s string, limit int) string {
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

func WrapText(s string, width int) []string {
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
			breakIdx = byteIdx
		}

		if breakIdx == 0 && len(remaining) > 0 {
			gr := uniseg.NewGraphemes(remaining)
			if gr.Next() {
				breakIdx = len(gr.Str())
			}
		}

		line := remaining[:breakIdx]
		trimmed := strings.TrimSpace(line)
		if trimmed != "" || !first {
			lines = append(lines, indent+trimmed)
		}

		remaining = remaining[breakIdx:]
		remaining = strings.TrimLeft(remaining, " ")
		first = false
	}

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

func FillRegion(s tcell.Screen, x, y, w, h int, r rune, style tcell.Style) {
	if s == nil {
		return
	}
	for ly := 0; ly < h; ly++ {
		for lx := 0; lx < w; lx++ {
			s.SetContent(x+lx, y+ly, r, nil, style)
		}
	}
}

func DrawBorder(s tcell.Screen, x, y, w, h int, style tcell.Style) {
	s.SetContent(x, y, '┏', nil, style)
	s.SetContent(x+w-1, y, '┓', nil, style)
	s.SetContent(x, y+h-1, '┗', nil, style)
	s.SetContent(x+w-1, y+h-1, '┛', nil, style)
	for lx := x + 1; lx < x+w-1; lx++ {
		s.SetContent(lx, y, '━', nil, style)
		s.SetContent(lx, y+h-1, '━', nil, style)
	}
	for ly := y + 1; ly < y+h-1; ly++ {
		s.SetContent(x, ly, '┃', nil, style)
		s.SetContent(x+w-1, ly, '┃', nil, style)
	}
}
