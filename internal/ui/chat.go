package ui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
)

type ChatUI struct {
	screen    tcell.Screen
	messages  []string
	visitors  []string
	input     string
	history   []string
	hIndex    int
	mu        sync.Mutex
	username  string
	title     string
	onSend    func(string)
	onExit    func()
	onClose   func()
	closeChan chan struct{}
	success   bool
}

func NewChatUI(screen tcell.Screen) *ChatUI {
	return &ChatUI{
		screen:    screen,
		messages:  make([]string, 0),
		visitors:  make([]string, 0),
		closeChan: make(chan struct{}, 1),
	}
}

func (ui *ChatUI) SetUsername(name string) {
	ui.mu.Lock()
	ui.username = name
	ui.mu.Unlock()
}

func (ui *ChatUI) SetTitle(title string) {
	ui.mu.Lock()
	ui.title = title
	ui.mu.Unlock()
}

func (ui *ChatUI) OnSend(cb func(string)) {
	ui.onSend = cb
}

func (ui *ChatUI) OnExit(cb func()) {
	ui.mu.Lock()
	ui.onExit = cb
	ui.mu.Unlock()
}

func (ui *ChatUI) OnClose(cb func()) {
	ui.mu.Lock()
	ui.onClose = cb
	ui.mu.Unlock()
}

func (ui *ChatUI) SetVisitors(visitors []string) {
	ui.mu.Lock()
	ui.visitors = visitors
	ui.mu.Unlock()
	ui.screen.PostEvent(&tcell.EventInterrupt{})
}

func (ui *ChatUI) Close(success bool) {
	ui.mu.Lock()
	onClose := ui.onClose
	select {
	case <-ui.closeChan:
		ui.mu.Unlock()
		return
	default:
		ui.success = success
		close(ui.closeChan)
	}
	ui.mu.Unlock()

	// Trigger the close callback immediately to unblock any I/O
	if onClose != nil {
		onClose()
	}

	// Wake up the PollEvent goroutine
	ui.screen.PostEvent(&tcell.EventInterrupt{})
}

func (ui *ChatUI) Run() string {
	exitChan := make(chan string, 1)
	drawChan := make(chan struct{}, 1)

	// Initial setup
	blackStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)
	ui.screen.SetStyle(blackStyle)
	ui.screen.Sync()
	ui.screen.Fill(' ', blackStyle)
	ui.screen.Show()
	drawChan <- struct{}{}

	// Event loop
	go func() {
		defer ui.screen.PostEvent(&tcell.EventInterrupt{}) // Ensure draw loop can exit
		for {
			ev := ui.screen.PollEvent()

			// Check if we should exit after being unblocked (ev could be nil or *tcell.EventInterrupt)
			select {
			case <-ui.closeChan:
				return
			default:
			}

			if ev == nil {
				return
			}

			switch ev := ev.(type) {
			case *tcell.EventKey:
				if ev.Key() == tcell.KeyCtrlC || ev.Key() == tcell.KeyEscape {
					go ui.Close(false) // Trigger clean exit
					exitChan <- ""
					return
				}
				if ev.Key() == tcell.KeyEnter && len(ui.input) > 0 {
					msg := ui.input
					ui.input = ""
					if strings.HasPrefix(msg, "/") {
						go ui.Close(true) // Trigger clean exit for command handling
						exitChan <- msg
						return
					}
					if ui.onSend != nil {
						ui.onSend(msg)
					}
				}
				ui.handleKey(ev)
			case *tcell.EventResize:
				ui.screen.Sync()
				ui.screen.Fill(' ', blackStyle)
			}
			// Trigger redraw after any event
			select {
			case drawChan <- struct{}{}:
			default:
			}
		}
	}()

	// Redraw loop
	for {
		select {
		case res := <-exitChan:
			return res
		case <-ui.closeChan:
			return ""
		case <-drawChan:
			ui.Draw()
		}
	}
}

func (ui *ChatUI) AddMessage(msg string) {
	ui.mu.Lock()
	ui.messages = append(ui.messages, msg)
	ui.mu.Unlock()
	ui.screen.PostEvent(&tcell.EventInterrupt{})
}

func (ui *ChatUI) Draw() {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	s := ui.screen
	w, h := s.Size()

	blackStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)
	promptStyle := blackStyle.Foreground(tcell.ColorGreen)
	sepStyle := blackStyle.Foreground(tcell.ColorDimGray)
	sidebarStyle := blackStyle.Foreground(tcell.ColorYellow)

	sidebarW := 18
	if w < 40 {
		sidebarW = 0 // Hide sidebar on very narrow screens
	}

	mainW := w - sidebarW - 1
	if sidebarW == 0 {
		mainW = w
	}

	// Draw Header (Single Line)
	title := ui.title
	if title == "" {
		title = "UNN Room Interaction"
	}
	ui.drawText(2, 0, title, mainW-4, blackStyle.Foreground(tcell.ColorLightCyan).Bold(true))

	userStr := fmt.Sprintf("Logged in as: %s", ui.username)
	ui.drawText(w-len(userStr)-2, 0, userStr, len(userStr), blackStyle)

	// Header separator
	for x := 0; x < w; x++ {
		s.SetContent(x, 1, '━', nil, sepStyle)
	}

	// Content start
	contentY := 2
	mainH := h - 3 // Reclaim space for input and header

	// Draw messages (Main Pane)
	start := 0
	if len(ui.messages) > mainH {
		start = len(ui.messages) - mainH
	}
	for i, msg := range ui.messages[start:] {
		if i >= mainH {
			break
		}
		// Truncate and pad
		displayMsg := msg
		if len(displayMsg) > mainW {
			displayMsg = truncateString(displayMsg, mainW)
		}
		ui.drawText(1, contentY+i, displayMsg, mainW-1, blackStyle)
	}

	// Draw Sidebar (Visitors)
	if sidebarW > 0 {
		// Vertical separator
		for y := 2; y < h-2; y++ {
			s.SetContent(mainW, y, '│', nil, sepStyle)
		}
		// Intersection piece
		s.SetContent(mainW, 1, '┳', nil, sepStyle)

		sidebarStartY := 2
		ui.drawText(mainW+1, sidebarStartY, " Visitors:      ", sidebarW, blackStyle)
		for i, visitor := range ui.visitors {
			if sidebarStartY+1+i >= h-2 {
				break
			}
			displayName := truncateString(visitor, sidebarW-2)
			ui.drawText(mainW+2, sidebarStartY+1+i, "• "+displayName, sidebarW-2, sidebarStyle)
		}
		// Clear rest of sidebar
		remaining := h - 2 - (sidebarStartY + 1 + len(ui.visitors))
		if remaining > 0 {
			ui.fillRegion(mainW+1, sidebarStartY+1+len(ui.visitors), sidebarW, remaining, ' ', blackStyle)
		}
	}

	// Draw input separator
	for x := 0; x < w; x++ {
		s.SetContent(x, h-2, '─', nil, sepStyle)
	}
	if sidebarW > 0 {
		s.SetContent(mainW, h-2, '┴', nil, sepStyle)
	}

	// Draw input
	prompt := "> "
	fullInput := prompt + ui.input
	ui.drawText(1, h-1, fullInput, w-2, promptStyle)
	s.ShowCursor(len(prompt)+len(ui.input)+1, h-1)

	s.Show()
}

func (ui *ChatUI) handleKey(ev *tcell.EventKey) bool {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	switch ev.Key() {
	case tcell.KeyEnter:
		// Handled in Run
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(ui.input) > 0 {
			ui.input = ui.input[:len(ui.input)-1]
		}
	case tcell.KeyUp:
		if ui.hIndex > 0 {
			ui.hIndex--
			ui.input = ui.history[ui.hIndex]
		}
	case tcell.KeyDown:
		if ui.hIndex < len(ui.history)-1 {
			ui.hIndex++
			ui.input = ui.history[ui.hIndex]
		} else {
			ui.hIndex = len(ui.history)
			ui.input = ""
		}
	case tcell.KeyRune:
		ui.input += string(ev.Rune())
	}
	return false
}

func (ui *ChatUI) drawText(x, y int, text string, width int, style tcell.Style) {
	runes := []rune(text)
	for i := 0; i < width; i++ {
		r := ' '
		if i < len(runes) {
			r = runes[i]
		}
		ui.screen.SetContent(x+i, y, r, nil, style)
	}
}

// FillRegion clears a rectangular area with a specific character and style
func (ui *ChatUI) fillRegion(x, y, w, h int, r rune, style tcell.Style) {
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			ui.screen.SetContent(x+col, y+row, r, nil, style)
		}
	}
}
