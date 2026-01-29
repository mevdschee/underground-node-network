package ui

import (
	"bufio"
	"fmt"
	"io"
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
	screen       tcell.Screen
	rooms        []RoomInfo
	logs         []Message
	input        string
	username     string
	addr         string
	history      []string
	hIndex       int
	cursorIdx    int
	scrollOffset int
	physicalLogs []Message
	lastWidth    int
	lastLogCount int
	mu           sync.Mutex
	onCmd        func(string)
	onExit       func()
	onClose      func()
	closeChan    chan struct{}
	success      bool
	Headless     bool
	Input        io.ReadWriter
}

func NewEntryUI(screen tcell.Screen, username, addr string) *EntryUI {
	return &EntryUI{
		screen:    screen,
		username:  username,
		addr:      addr,
		rooms:     make([]RoomInfo, 0),
		logs:      make([]Message, 0),
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

func (ui *EntryUI) SetScreen(screen tcell.Screen) {
	ui.mu.Lock()
	ui.screen = screen
	ui.mu.Unlock()
}

func (ui *EntryUI) GetScreen() tcell.Screen {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.screen
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
	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *EntryUI) SetBanner(banner []string) {
	ui.mu.Lock()
	// Clear existing logs and set banner as the initial content
	ui.logs = make([]Message, len(banner))
	for i, b := range banner {
		ui.logs[i] = Message{Text: b, Type: MsgServer}
	}
	ui.mu.Unlock()
	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *EntryUI) ShowMessage(msg string, msgType MessageType) {
	ui.mu.Lock()
	ui.logs = append(ui.logs, Message{Text: msg, Type: msgType})
	if len(ui.logs) > 100 {
		ui.logs = ui.logs[1:]
	}
	ui.scrollOffset = 0 // Reset scroll on new message
	ui.mu.Unlock()
	if ui.Headless && ui.Input != nil {
		fmt.Fprintf(ui.Input, "%s\r\n", msg)
	}
	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *EntryUI) GetLogs() []Message {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	res := make([]Message, len(ui.logs))
	copy(res, ui.logs)
	return res
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
	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *EntryUI) Run() bool {
	if ui.Headless {
		if ui.Input != nil {
			go func() {
				scanner := bufio.NewScanner(ui.Input)
				for scanner.Scan() {
					cmd := scanner.Text()
					ui.mu.Lock()
					onCmd := ui.onCmd
					ui.mu.Unlock()
					if onCmd != nil {
						onCmd(cmd)
					}
					// Check if we should exit after processing a command (e.g. joined room)
					select {
					case <-ui.closeChan:
						return
					default:
					}
				}
				ui.Close(false)
			}()
		}
		<-ui.closeChan
		return ui.success
	}

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
	if ui.screen == nil {
		return
	}
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

	// Draw Main Pane (Left) - Messages (including Banner)
	logH := h - 2 - contentStartY
	if logH > 0 {
		ui.updatePhysicalLogs(mainW)

		logEnd := len(ui.physicalLogs) - ui.scrollOffset
		if logEnd < 0 {
			logEnd = 0
		}
		logStart := logEnd - logH
		if logStart < 0 {
			logStart = 0
		}
		for i, logMsg := range ui.physicalLogs[logStart:logEnd] {
			if contentStartY+i >= h-2 {
				break
			}

			style := logStyle
			switch logMsg.Type {
			case MsgCommand:
				style = promptStyle.Foreground(tcell.ColorDimGray)
			case MsgServer:
				style = blackStyle.Foreground(tcell.ColorLightGray)
			case MsgSystem:
				style = headerStyle.Foreground(tcell.ColorDimGray)
			}

			// Use headerStyle if it looks like ASCII art (e.g., contains box drawing or many symbols)
			if strings.ContainsAny(logMsg.Text, "╔╗╚╝║═█▀▄▌▐") {
				style = headerStyle
			}
			ui.drawText(2, contentStartY+i, truncateString(logMsg.Text, mainW-4), mainW-4, style)
		}
		// Clear rest of main pane
		logCount := len(ui.logs[logStart:])
		mainRemaining := h - 2 - (contentStartY + logCount)
		if mainRemaining > 0 {
			ui.fillRegion(1, contentStartY+logCount, mainW-1, mainRemaining, ' ', blackStyle)
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
	s.ShowCursor(len([]rune(prompt))+ui.cursorIdx+1, h-1)

	s.Show()
}

func (ui *EntryUI) handleKeyResult(ev *tcell.EventKey) (done bool, success bool) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	runes := []rune(ui.input)

	switch ev.Key() {
	case tcell.KeyCtrlC, tcell.KeyEscape:
		go ui.Close(false) // Trigger clean exit via closeChan
		return true, false
	case tcell.KeyEnter:
		if len(ui.input) > 0 {
			cmd := ui.input
			ui.input = ""
			ui.cursorIdx = 0
			ui.scrollOffset = 0

			// Save to history if not duplicate of last
			if len(ui.history) == 0 || ui.history[len(ui.history)-1] != cmd {
				ui.history = append(ui.history, cmd)
			}
			ui.hIndex = len(ui.history)

			if ui.onCmd != nil {
				go ui.onCmd(cmd)
			}
		}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if ui.cursorIdx > 0 {
			runes = append(runes[:ui.cursorIdx-1], runes[ui.cursorIdx:]...)
			ui.input = string(runes)
			ui.cursorIdx--
		}
	case tcell.KeyDelete:
		if ui.cursorIdx < len(runes) {
			runes = append(runes[:ui.cursorIdx], runes[ui.cursorIdx+1:]...)
			ui.input = string(runes)
		}
	case tcell.KeyLeft:
		if ui.cursorIdx > 0 {
			ui.cursorIdx--
		}
	case tcell.KeyRight:
		if ui.cursorIdx < len(runes) {
			ui.cursorIdx++
		}
	case tcell.KeyHome:
		ui.cursorIdx = 0
	case tcell.KeyEnd:
		ui.cursorIdx = len(runes)
	case tcell.KeyPgUp:
		_, h := ui.screen.Size()
		ui.scrollOffset += (h - 4)
		if ui.scrollOffset > len(ui.physicalLogs) {
			ui.scrollOffset = len(ui.physicalLogs)
		}
	case tcell.KeyPgDn:
		_, h := ui.screen.Size()
		ui.scrollOffset -= (h - 4)
		if ui.scrollOffset < 0 {
			ui.scrollOffset = 0
		}
	case tcell.KeyUp:
		if ui.hIndex > 0 {
			ui.hIndex--
			ui.input = ui.history[ui.hIndex]
			ui.cursorIdx = len([]rune(ui.input))
		}
	case tcell.KeyDown:
		if ui.hIndex < len(ui.history)-1 {
			ui.hIndex++
			ui.input = ui.history[ui.hIndex]
			ui.cursorIdx = len([]rune(ui.input))
		} else {
			ui.hIndex = len(ui.history)
			ui.input = ""
			ui.cursorIdx = 0
		}
	case tcell.KeyRune:
		runes = append(runes[:ui.cursorIdx], append([]rune{ev.Rune()}, runes[ui.cursorIdx:]...)...)
		ui.input = string(runes)
		ui.cursorIdx++
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

func (ui *EntryUI) updatePhysicalLogs(width int) {
	if width == ui.lastWidth && len(ui.logs) == ui.lastLogCount {
		return
	}

	ui.physicalLogs = nil
	for _, msg := range ui.logs {
		wrapped := wrapText(msg.Text, width-4) // Account for padding
		for _, line := range wrapped {
			ui.physicalLogs = append(ui.physicalLogs, Message{Text: line, Type: msg.Type})
		}
	}
	ui.lastWidth = width
	ui.lastLogCount = len(ui.logs)
}
