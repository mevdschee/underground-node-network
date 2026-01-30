package input

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"github.com/rivo/uniseg"
)

// CommandInput handles single-line text input with history
type CommandInput struct {
	Prompt    string
	Value     string
	CursorIdx int
	History   []string
	HIndex    int
}

func NewCommandInput(prompt string) *CommandInput {
	return &CommandInput{
		Prompt: prompt,
	}
}

func (i *CommandInput) Draw(s tcell.Screen, x, y, w int, style, promptStyle tcell.Style) {
	if s == nil {
		return
	}

	fullPrompt := i.Prompt + " "
	promptWidth := uniseg.StringWidth(fullPrompt)
	common.DrawText(s, x, y, fullPrompt, promptWidth, promptStyle)

	common.DrawText(s, x+promptWidth, y, i.Value, w-promptWidth, style)

	// Position cursor
	prefix := string([]rune(i.Value)[:i.CursorIdx])
	visualPos := uniseg.StringWidth(prefix)
	s.ShowCursor(x+promptWidth+visualPos, y)
}

func (i *CommandInput) HandleKey(ev *tcell.EventKey) (bool, string) {
	switch ev.Key() {
	case tcell.KeyEnter:
		val := i.Value
		if val != "" {
			i.History = append(i.History, val)
			i.HIndex = len(i.History)
		}
		i.Value = ""
		i.CursorIdx = 0
		return true, val
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if i.CursorIdx > 0 {
			runes := []rune(i.Value)
			i.Value = string(runes[:i.CursorIdx-1]) + string(runes[i.CursorIdx:])
			i.CursorIdx--
		}
	case tcell.KeyDelete:
		runes := []rune(i.Value)
		if i.CursorIdx < len(runes) {
			i.Value = string(runes[:i.CursorIdx]) + string(runes[i.CursorIdx+1:])
		}
	case tcell.KeyLeft:
		if i.CursorIdx > 0 {
			i.CursorIdx--
		}
	case tcell.KeyRight:
		if i.CursorIdx < len([]rune(i.Value)) {
			i.CursorIdx++
		}
	case tcell.KeyHome:
		i.CursorIdx = 0
	case tcell.KeyEnd:
		i.CursorIdx = len([]rune(i.Value))
	case tcell.KeyUp:
		if i.HIndex > 0 {
			i.HIndex--
			i.Value = i.History[i.HIndex]
			i.CursorIdx = len([]rune(i.Value))
		}
	case tcell.KeyDown:
		if i.HIndex < len(i.History)-1 {
			i.HIndex++
			i.Value = i.History[i.HIndex]
			i.CursorIdx = len([]rune(i.Value))
		} else {
			i.HIndex = len(i.History)
			i.Value = ""
			i.CursorIdx = 0
		}
	case tcell.KeyRune:
		runes := []rune(i.Value)
		i.Value = string(runes[:i.CursorIdx]) + string(ev.Rune()) + string(runes[i.CursorIdx:])
		i.CursorIdx++
	}
	return false, ""
}
