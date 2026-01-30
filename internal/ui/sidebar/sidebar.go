package sidebar

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
)

// Sidebar represents a vertical information panel
type Sidebar struct {
	Title string
	Items []string
	Width int
}

// NewSidebar creates a new sidebar with a title and width
func NewSidebar(title string, width int) *Sidebar {
	return &Sidebar{
		Title: title,
		Width: width,
	}
}

// SetItems updates the items in the sidebar
func (b *Sidebar) SetItems(items []string) {
	b.Items = items
}

// Draw renders the sidebar at (x, y) with a specific height
func (b *Sidebar) Draw(s tcell.Screen, x, y, h int, style, sepStyle tcell.Style) {
	if s == nil || b.Width <= 0 {
		return
	}

	// Draw vertical separator
	for sy := y; sy < y+h; sy++ {
		s.SetContent(x, sy, '│', nil, sepStyle)
	}

	// Draw title
	common.DrawText(s, x+1, y, " "+b.Title, b.Width-1, style.Bold(true))

	// Draw items
	for i, item := range b.Items {
		if i+1 >= h {
			break
		}
		common.DrawText(s, x+2, y+1+i, item, b.Width-2, style)
	}
}
