package form

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"github.com/rivo/uniseg"
)

type FormField struct {
	Label        string
	Value        string
	Error        string
	MaxLength    int
	Alphanumeric bool
}

// Form handles structured, multi-field input
type Form struct {
	Title     string
	Fields    []FormField
	ActiveIdx int
	CursorIdx int
}

func NewForm(title string, fields []FormField) *Form {
	return &Form{
		Title:  title,
		Fields: fields,
	}
}

func (f *Form) Draw(s tcell.Screen, w, h int, style tcell.Style) {
	if s == nil || len(f.Fields) == 0 {
		return
	}

	boxW := 55
	boxH := 4 + (len(f.Fields) * 3)
	if w < boxW+4 {
		boxW = w - 4
	}
	startX := (w - boxW) / 2
	startY := (h - boxH) / 2

	// Shadow
	common.FillRegion(s, startX+2, startY+1, boxW, boxH, ' ', tcell.StyleDefault.Background(tcell.ColorBlack))

	// Box background
	boxStyle := style.Background(tcell.ColorDarkBlue).Foreground(tcell.ColorWhite)
	common.FillRegion(s, startX, startY, boxW, boxH, ' ', boxStyle)

	// Border
	borderStyle := boxStyle.Foreground(tcell.ColorLightCyan)
	common.DrawBorder(s, startX, startY, boxW, boxH, borderStyle)

	// Title
	title := " " + f.Title + " "
	common.DrawText(s, startX+(boxW-len(title))/2, startY, title, len(title), borderStyle.Bold(true))

	for i, field := range f.Fields {
		fieldY := startY + 2 + (i * 3)
		label := field.Label
		if i == f.ActiveIdx {
			label = fmt.Sprintf("▶ %s", label)
		} else {
			label = fmt.Sprintf("  %s", label)
		}
		common.DrawText(s, startX+2, fieldY, label, boxW-4, boxStyle)

		// Value field
		valueStyle := boxStyle.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)
		if i == f.ActiveIdx {
			valueStyle = valueStyle.Underline(true).Foreground(tcell.ColorYellow)
		}
		valW := boxW - 6
		common.FillRegion(s, startX+4, fieldY+1, valW, 1, ' ', valueStyle)
		common.DrawText(s, startX+4, fieldY+1, field.Value, valW, valueStyle)

		// Error message
		if field.Error != "" {
			errorStyle := boxStyle.Foreground(tcell.ColorRed).Bold(true)
			common.DrawText(s, startX+4, fieldY+2, "  ! "+field.Error, valW, errorStyle)
		}

		if i == f.ActiveIdx {
			prefix := string([]rune(field.Value)[:f.CursorIdx])
			visualPos := uniseg.StringWidth(prefix)
			s.ShowCursor(startX+4+visualPos, fieldY+1)
		}
	}

	hint := "TAB to move • ENTER to submit • ESC to cancel"
	hintText := fmt.Sprintf(" %s ", hint)
	common.DrawText(s, startX+(boxW-len(hintText))/2, startY+boxH-1, hintText, len(hintText), borderStyle)
}

func (f *Form) HandleKey(ev *tcell.EventKey) (bool, []string) {
	switch ev.Key() {
	case tcell.KeyTab, tcell.KeyDown:
		f.ActiveIdx = (f.ActiveIdx + 1) % len(f.Fields)
		f.CursorIdx = len([]rune(f.Fields[f.ActiveIdx].Value))
	case tcell.KeyUp:
		f.ActiveIdx = (f.ActiveIdx - 1 + len(f.Fields)) % len(f.Fields)
		f.CursorIdx = len([]rune(f.Fields[f.ActiveIdx].Value))
	case tcell.KeyEscape, tcell.KeyCtrlC:
		return true, nil
	case tcell.KeyEnter:
		vals := make([]string, len(f.Fields))
		for i, fld := range f.Fields {
			vals[i] = fld.Value
		}
		return true, vals
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		fld := &f.Fields[f.ActiveIdx]
		if f.CursorIdx > 0 {
			runes := []rune(fld.Value)
			fld.Value = string(append(runes[:f.CursorIdx-1], runes[f.CursorIdx:]...))
			f.CursorIdx--
		}
	case tcell.KeyDelete:
		fld := &f.Fields[f.ActiveIdx]
		runes := []rune(fld.Value)
		if f.CursorIdx < len(runes) {
			fld.Value = string(append(runes[:f.CursorIdx], runes[f.CursorIdx+1:]...))
		}
	case tcell.KeyLeft:
		if f.CursorIdx > 0 {
			f.CursorIdx--
		}
	case tcell.KeyRight:
		fld := &f.Fields[f.ActiveIdx]
		if f.CursorIdx < len([]rune(fld.Value)) {
			f.CursorIdx++
		}
	case tcell.KeyHome:
		f.CursorIdx = 0
	case tcell.KeyEnd:
		fld := &f.Fields[f.ActiveIdx]
		f.CursorIdx = len([]rune(fld.Value))
	case tcell.KeyRune:
		fld := &f.Fields[f.ActiveIdx]
		if fld.Alphanumeric && !common.IsAlphanumeric(string(ev.Rune())) {
			return false, nil
		}
		if fld.MaxLength > 0 && len([]rune(fld.Value)) >= fld.MaxLength {
			return false, nil
		}
		runes := []rune(fld.Value)
		fld.Value = string(runes[:f.CursorIdx]) + string(ev.Rune()) + string(runes[f.CursorIdx:])
		f.CursorIdx++
	}
	return false, nil
}
