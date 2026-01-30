package popup

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
)

// Popup represents a stylized terminal notification
type Popup struct {
	Title   string
	Message string
}

func NewPopup(title, message string) *Popup {
	return &Popup{Title: title, Message: message}
}

// Draw renders the popup centered on the screen.
// This is used for server-side preview or local rendering.
func (p *Popup) Draw(s tcell.Screen, w, h int) {
	if s == nil {
		return
	}

	lines := common.WrapText(p.Message, 46)
	boxW := 50
	boxH := len(lines) + 4

	startX := (w - boxW) / 2
	startY := (h - boxH) / 2

	// Shadow
	common.FillRegion(s, startX+2, startY+1, boxW, boxH, ' ', tcell.StyleDefault.Background(tcell.ColorBlack))

	// Box
	style := tcell.StyleDefault.Background(tcell.ColorDarkRed).Foreground(tcell.ColorWhite)
	common.FillRegion(s, startX, startY, boxW, boxH, ' ', style)

	// Border
	borderStyle := style.Foreground(tcell.ColorYellow)
	common.DrawBorder(s, startX, startY, boxW, boxH, borderStyle)

	// Title
	title := " " + p.Title + " "
	common.DrawText(s, startX+(boxW-len(title))/2, startY, title, len(title), borderStyle.Bold(true))

	// Content
	for i, line := range lines {
		common.DrawText(s, startX+2, startY+2+i, line, boxW-4, style)
	}
}
