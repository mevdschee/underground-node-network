package banner

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
)

// Banner represents a multi-line ANSI art banner
type Banner struct {
	Lines []string
}

// NewBanner creates a new banner from a slice of strings
func NewBanner(lines []string) *Banner {
	return &Banner{Lines: lines}
}

// Draw renders the banner at the specified location
func (b *Banner) Draw(s tcell.Screen, x, y int, width int, style tcell.Style) {
	if s == nil || b == nil {
		return
	}
	for i, line := range b.Lines {
		common.DrawText(s, x, y+i, line, width, style)
	}
}

// Height returns the number of lines in the banner
func (b *Banner) Height() int {
	if b == nil {
		return 0
	}
	return len(b.Lines)
}
