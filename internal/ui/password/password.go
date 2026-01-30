package password

import (
	"fmt"
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
	visualPos := uniseg.StringWidth(prefix)
	s.ShowCursor(startX+2+visualPos, startY+2)

	// Hints
	hint := "ENTER to submit • ESC to cancel"
	common.DrawText(s, startX+(boxW-len(hint))/2, startY+boxH-1, fmt.Sprintf(" %s ", hint), len(hint)+2, borderStyle)
}

func (p *PasswordEntry) HandleKey(ev *tcell.EventKey) (bool, string, bool) {
	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlC:
		return true, "", true
	case tcell.KeyEnter:
		return true, p.Value, false
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
	case tcell.KeyHome:
		p.Cursor = 0
	case tcell.KeyEnd:
		p.Cursor = len([]rune(p.Value))
	case tcell.KeyRune:
		runes := []rune(p.Value)
		p.Value = string(append(runes[:p.Cursor], append([]rune{ev.Rune()}, runes[p.Cursor:]...)...))
		p.Cursor++
	}
	return false, "", false
}

// PasswordUI is a full-screen prompt for sensitive input (legacy/standalone wrapper)
type PasswordUI struct {
	screen    tcell.Screen
	input     string
	Cursor    int
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
				switch ev.Key() {
				case tcell.KeyBackspace, tcell.KeyBackspace2:
					ui.mu.Lock()
					runes := []rune(ui.input)
					if ui.Cursor > 0 {
						ui.input = string(append(runes[:ui.Cursor-1], runes[ui.Cursor:]...))
						ui.Cursor--
					}
					ui.mu.Unlock()
				case tcell.KeyDelete:
					ui.mu.Lock()
					runes := []rune(ui.input)
					if ui.Cursor < len(runes) {
						ui.input = string(append(runes[:ui.Cursor], runes[ui.Cursor+1:]...))
					}
					ui.mu.Unlock()
				case tcell.KeyLeft:
					ui.mu.Lock()
					if ui.Cursor > 0 {
						ui.Cursor--
					}
					ui.mu.Unlock()
				case tcell.KeyRight:
					ui.mu.Lock()
					if ui.Cursor < len([]rune(ui.input)) {
						ui.Cursor++
					}
					ui.mu.Unlock()
				case tcell.KeyHome:
					ui.mu.Lock()
					ui.Cursor = 0
					ui.mu.Unlock()
				case tcell.KeyEnd:
					ui.mu.Lock()
					ui.Cursor = len([]rune(ui.input))
					ui.mu.Unlock()
				case tcell.KeyRune:
					ui.mu.Lock()
					runes := []rune(ui.input)
					ui.input = string(append(runes[:ui.Cursor], append([]rune{ev.Rune()}, runes[ui.Cursor:]...)...))
					ui.Cursor++
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

	// Cursor
	prefix := stars[:ui.Cursor]
	visualPos := uniseg.StringWidth(prefix)
	ui.screen.ShowCursor(px+len(prompt)+visualPos, ty+2)

	hint := "ENTER to submit • ESC to cancel"
	hx := (w - len(hint)) / 2
	common.DrawText(ui.screen, hx, ty+4, hint, len(hint), style.Foreground(tcell.ColorLightCyan))

	ui.screen.Show()
}

func (ui *PasswordUI) Close() {
	close(ui.closeChan)
}
