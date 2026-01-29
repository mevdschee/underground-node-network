package ui

import (
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
)

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
	ui.Draw()

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
				ui.Draw()
			case *tcell.EventResize:
				ui.screen.Sync()
				ui.Draw()
			}
		}
	}()

	res := <-ui.done
	return res
}

func (ui *PasswordUI) Draw() {
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

	ui.drawText(tx, ty, title, style.Bold(true))
	ui.drawText(px, ty+2, prompt+stars, style)

	ui.screen.Show()
}

func (ui *PasswordUI) drawText(x, y int, text string, style tcell.Style) {
	for i, r := range text {
		ui.screen.SetContent(x+i, y, r, nil, style)
	}
}

func (ui *PasswordUI) Close() {
	close(ui.closeChan)
}
