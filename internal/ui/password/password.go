package password

import (
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"github.com/rivo/uniseg"
)

// PasswordEntry is a centered prompt for sensitive input
type PasswordEntry struct {
	Prompt string
	Value  string
	Cursor int
}

func NewPasswordEntry(prompt string) *PasswordEntry {
	return &PasswordEntry{Prompt: prompt}
}

func (p *PasswordEntry) Draw(s tcell.Screen, w, h int, style tcell.Style) {
	if s == nil {
		return
	}

	boxW := 40
	boxH := 5
	startX := (w - boxW) / 2
	startY := (h - boxH) / 2

	// Shadow
	common.FillRegion(s, startX+1, startY+1, boxW, boxH, ' ', tcell.StyleDefault.Background(tcell.ColorBlack))

	// Box
	boxStyle := style.Background(tcell.ColorDarkBlue).Foreground(tcell.ColorWhite)
	common.FillRegion(s, startX, startY, boxW, boxH, ' ', boxStyle)

	// Border
	borderStyle := boxStyle.Foreground(tcell.ColorLightCyan)
	common.DrawBorder(s, startX, startY, boxW, boxH, borderStyle)

	// Prompt
	common.DrawText(s, startX+2, startY+1, p.Prompt, boxW-4, boxStyle.Bold(true))

	// Input Area
	inputStyle := boxStyle.Background(tcell.ColorBlack).Foreground(tcell.ColorYellow)
	common.FillRegion(s, startX+2, startY+2, boxW-4, 1, ' ', inputStyle)

	masked := ""
	for i := 0; i < len([]rune(p.Value)); i++ {
		masked += "*"
	}
	common.DrawText(s, startX+2, startY+2, masked, boxW-4, inputStyle)

	// Cursor
	prefix := masked[:p.Cursor]
	cursorX := startX + 2 + uniseg.StringWidth(prefix)
	s.ShowCursor(cursorX, startY+2)
}

func (p *PasswordEntry) HandleKey(ev *tcell.EventKey) (bool, string) {
	switch ev.Key() {
	case tcell.KeyEnter:
		return true, p.Value
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if p.Cursor > 0 {
			runes := []rune(p.Value)
			p.Value = string(append(runes[:p.Cursor-1], runes[p.Cursor:]...))
			p.Cursor--
		}
	case tcell.KeyDelete:
		runes := []rune(p.Value)
		if p.Cursor < len(runes) {
			p.Value = string(append(runes[:p.Cursor], runes[p.Cursor+1:]...))
		}
	case tcell.KeyLeft:
		if p.Cursor > 0 {
			p.Cursor--
		}
	case tcell.KeyRight:
		if p.Cursor < len([]rune(p.Value)) {
			p.Cursor++
		}
	case tcell.KeyRune:
		runes := []rune(p.Value)
		p.Value = string(append(runes[:p.Cursor], append([]rune{ev.Rune()}, runes[p.Cursor:]...)...))
		p.Cursor++
	}
	return false, ""
}

// PasswordUI is a full-screen prompt for sensitive input (legacy/standalone wrapper)
type PasswordUI struct {
	screen    tcell.Screen
	input     string
	mu        sync.Mutex
	done      chan string
	closeChan chan struct{}
}

func NewPasswordUI(screen tcell.Screen) *PasswordUI {
	return &PasswordUI{
		screen:    screen,
		done:      make(chan string, 1),
		closeChan: make(chan struct{}),
	}
}

func (ui *PasswordUI) Run() string {
	if ui.screen == nil {
		return ""
	}

	ui.screen.Clear()
	ui.DrawStandalone()

	go func() {
		for {
			select {
			case <-ui.closeChan:
				return
			default:
			}
			ev := ui.screen.PollEvent()
			if ev == nil {
				ui.done <- ""
				return
			}
			switch ev := ev.(type) {
			case *tcell.EventKey:
				if ev.Key() == tcell.KeyCtrlC || ev.Key() == tcell.KeyEscape {
					ui.done <- ""
					return
				}
				if ev.Key() == tcell.KeyEnter {
					ui.done <- ui.input
					return
				}
				if ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2 {
					ui.mu.Lock()
					runes := []rune(ui.input)
					if len(runes) > 0 {
						ui.input = string(runes[:len(runes)-1])
					}
					ui.mu.Unlock()
				} else if ev.Key() == tcell.KeyRune {
					ui.mu.Lock()
					ui.input += string(ev.Rune())
					ui.mu.Unlock()
				}
				ui.DrawStandalone()
			case *tcell.EventResize:
				ui.screen.Sync()
				ui.DrawStandalone()
			}
		}
	}()

	res := <-ui.done
	return res
}

func (ui *PasswordUI) DrawStandalone() {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	w, h := ui.screen.Size()
	style := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)

	ui.screen.Fill(' ', style)

	title := "--- ROOM SECURED ---"
	prompt := "Enter Key: "
	stars := strings.Repeat("*", len(ui.input))

	tx := (w - len(title)) / 2
	px := (w - (len(prompt) + 20)) / 2
	ty := h/2 - 2

	common.DrawText(ui.screen, tx, ty, title, len(title), style.Bold(true))
	common.DrawText(ui.screen, px, ty+2, prompt+stars, 30, style)

	ui.screen.Show()
}

func (ui *PasswordUI) Close() {
	close(ui.closeChan)
}
