package ui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
)

type RoomInfo struct {
	Name  string
	Owner string
	Doors []string
}

type EntryUI struct {
	screen    tcell.Screen
	rooms     []RoomInfo
	logs      []string
	banner    []string
	input     string
	username  string
	addr      string
	mu        sync.Mutex
	onCmd     func(string)
	onExit    func()
	onClose   func()
	closeChan chan struct{}
	success   bool
}

func NewEntryUI(screen tcell.Screen, username, addr string) *EntryUI {
	return &EntryUI{
		screen:    screen,
		username:  username,
		addr:      addr,
		rooms:     make([]RoomInfo, 0),
		logs:      make([]string, 0),
		banner:    make([]string, 0),
		closeChan: make(chan struct{}, 1),
	}
}

func (ui *EntryUI) OnCmd(cb func(string)) {
	ui.mu.Lock()
	ui.onCmd = cb
	ui.mu.Unlock()
}

func (ui *EntryUI) OnExit(cb func()) {
	ui.mu.Lock()
	ui.onExit = cb
	ui.mu.Unlock()
}

func (ui *EntryUI) OnClose(cb func()) {
	ui.mu.Lock()
	ui.onClose = cb
	ui.mu.Unlock()
}

func (ui *EntryUI) Lock() {
	ui.mu.Lock()
}

func (ui *EntryUI) Unlock() {
	ui.mu.Unlock()
}

func (ui *EntryUI) SetRooms(rooms []RoomInfo) {
	ui.mu.Lock()
	ui.rooms = rooms
	ui.mu.Unlock()
	ui.screen.PostEvent(&tcell.EventInterrupt{})
}

func (ui *EntryUI) SetBanner(banner []string) {
	ui.mu.Lock()
	ui.banner = banner
	ui.mu.Unlock()
	ui.screen.PostEvent(&tcell.EventInterrupt{})
}

func (ui *EntryUI) ShowMessage(msg string) {
	ui.mu.Lock()
	ui.logs = append(ui.logs, msg)
	if len(ui.logs) > 100 {
		ui.logs = ui.logs[1:]
	}
	ui.mu.Unlock()
	ui.screen.PostEvent(&tcell.EventInterrupt{})
}

func (ui *EntryUI) Close(success bool) {
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

func (ui *EntryUI) Run() bool {
	exitChan := make(chan bool, 1)
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
				done, success := ui.handleKeyResult(ev)
				if done {
					exitChan <- success
					return
				}
			case *tcell.EventResize:
				ui.screen.Sync()
				ui.screen.Fill(' ', blackStyle)
			}
			// Trigger redraw after event
			select {
			case drawChan <- struct{}{}:
			default:
			}
		}
	}()

	// Redraw loop
	for {
		select {
		case success := <-exitChan:
			return success
		case <-ui.closeChan:
			ui.mu.Lock()
			defer ui.mu.Unlock()
			return ui.success
		case <-drawChan:
			ui.Draw()
		}
	}
}

func (ui *EntryUI) Draw() {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	s := ui.screen
	w, h := s.Size()

	blackStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)
	headerStyle := blackStyle.Foreground(tcell.ColorLightCyan).Bold(true)
	roomStyle := blackStyle.Foreground(tcell.ColorYellow)
	logStyle := blackStyle.Foreground(tcell.ColorLightGray)
	promptStyle := blackStyle.Foreground(tcell.ColorGreen)
	sepStyle := blackStyle.Foreground(tcell.ColorDimGray)

	// Sidebar config
	sidebarW := 25
	if w < 60 {
		sidebarW = 0
	}
	mainW := w - sidebarW - 1
	if sidebarW == 0 {
		mainW = w
	}

	// Draw Header (Single Line)
	title := "Underground Node Network - Entry Point"
	ui.drawText(2, 0, title, mainW-4, headerStyle)

	userStr := fmt.Sprintf("Logged in as: %s", ui.username)
	userLen := len([]rune(userStr))
	ui.drawText(w-userLen-2, 0, userStr, userLen, blackStyle)

	// Pane separator (horizontal, below header)
	sepY := 1
	for x := 0; x < w; x++ {
		s.SetContent(x, sepY, '━', nil, sepStyle)
	}

	// Main area (below sepY, above input)
	contentStartY := sepY + 1

	// Draw Sidebar (Rooms - Right)
	if sidebarW > 0 {
		// Vertical separator
		for y := contentStartY; y < h-1; y++ {
			s.SetContent(mainW, y, '│', nil, sepStyle)
		}
		// Intersection piece
		s.SetContent(mainW, sepY, '┳', nil, sepStyle)

		ui.drawText(mainW+1, contentStartY, " Active rooms:", sidebarW-1, blackStyle)
		if len(ui.rooms) == 0 {
			ui.drawText(mainW+4, contentStartY+1, "(none)", sidebarW-4, sepStyle)
		} else {
			for i, room := range ui.rooms {
				if contentStartY+1+i >= h-2 {
					break
				}
				doorStr := ""
				if len(room.Doors) > 0 {
					doorStr = fmt.Sprintf(" [%s]", strings.Join(room.Doors, ", "))
				}
				displayLine := fmt.Sprintf("• %s%s", room.Name, doorStr)
				ui.drawText(mainW+2, contentStartY+1+i, truncateString(displayLine, sidebarW-3), sidebarW-3, roomStyle)
			}
		}
		// Clear rest of sidebar
		sidebarRemaining := h - 2 - (contentStartY + 1 + len(ui.rooms))
		if sidebarRemaining > 0 {
			ui.fillRegion(mainW+1, contentStartY+1+len(ui.rooms), sidebarW, sidebarRemaining, ' ', blackStyle)
		}
	}

	// Draw Main Pane (Left) - Banner + Messages
	currentY := contentStartY

	// Banner in Main Pane
	if len(ui.banner) > 0 {
		for _, line := range ui.banner {
			if currentY >= h-2 {
				break
			}
			ui.drawText(2, currentY, truncateString(line, mainW-2), mainW-2, headerStyle)
			currentY++
		}
		currentY++ // Space after banner
	}

	// Messages follow banner in Main Pane
	if currentY < h-2 {
		logH := h - 2 - currentY
		logStart := 0
		if len(ui.logs) > logH {
			logStart = len(ui.logs) - logH
		}
		logCount := 0
		for i, logMsg := range ui.logs[logStart:] {
			if currentY+i >= h-2 {
				break
			}
			ui.drawText(2, currentY+i, truncateString(logMsg, mainW-4), mainW-4, logStyle)
			logCount++
		}
		// Clear rest of main pane
		mainRemaining := h - 2 - (currentY + logCount)
		if mainRemaining > 0 {
			ui.fillRegion(1, currentY+logCount, mainW-1, mainRemaining, ' ', blackStyle)
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
	ui.drawText(1, h-1, prompt+ui.input, w-2, promptStyle)
	s.ShowCursor(len([]rune(prompt))+len([]rune(ui.input))+1, h-1)

	s.Show()
}

func (ui *EntryUI) handleKeyResult(ev *tcell.EventKey) (done bool, success bool) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	switch ev.Key() {
	case tcell.KeyCtrlC, tcell.KeyEscape:
		go ui.Close(false) // Trigger clean exit via closeChan
		return true, false
	case tcell.KeyEnter:
		if len(ui.input) > 0 {
			cmd := ui.input
			ui.input = ""
			if ui.onCmd != nil {
				go ui.onCmd(cmd)
			}
		}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		runes := []rune(ui.input)
		if len(runes) > 0 {
			ui.input = string(runes[:len(runes)-1])
		}
	case tcell.KeyRune:
		ui.input += string(ev.Rune())
	}
	return false, false
}

func (ui *EntryUI) drawText(x, y int, text string, width int, style tcell.Style) {
	runes := []rune(text)
	for i := 0; i < width; i++ {
		r := ' '
		if i < len(runes) {
			r = runes[i]
		}
		ui.screen.SetContent(x+i, y, r, nil, style)
	}
}

func (ui *EntryUI) fillRegion(x, y, w, h int, r rune, style tcell.Style) {
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			ui.screen.SetContent(x+col, y+row, r, nil, style)
		}
	}
}
