package ui

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"github.com/mevdschee/underground-node-network/internal/ui/input"
	"github.com/mevdschee/underground-node-network/internal/ui/log"
	"github.com/mevdschee/underground-node-network/internal/ui/sidebar"
)

type ChatUI struct {
	screen        tcell.Screen
	logs          *log.LogView
	peopleSidebar *sidebar.Sidebar
	doorsSidebar  *sidebar.Sidebar
	cmdInput      *input.CommandInput

	mu        sync.Mutex
	username  string
	title     string
	onSend    func(string)
	onExit    func()
	onClose   func()
	onCmd     func(string) bool
	drawChan  chan struct{}
	closeChan chan struct{}

	success   bool
	firstDraw bool
	Headless  bool
	Input     io.ReadWriter
}

func NewChatUI(screen tcell.Screen) *ChatUI {
	return &ChatUI{
		screen:        screen,
		logs:          log.NewLogView(),
		peopleSidebar: sidebar.NewSidebar("People:", 18),
		doorsSidebar:  sidebar.NewSidebar("Doors:", 18),
		cmdInput:      input.NewCommandInput(">"),
		drawChan:      make(chan struct{}, 1),
		closeChan:     make(chan struct{}, 1),
	}
}

func (ui *ChatUI) SetUsername(name string) {
	ui.mu.Lock()
	ui.username = name
	screen := ui.screen
	ui.mu.Unlock()
	if screen != nil {
		screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *ChatUI) SetTitle(title string) {
	ui.mu.Lock()
	ui.title = title
	screen := ui.screen
	ui.mu.Unlock()
	if screen != nil {
		screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *ChatUI) SetScreen(screen tcell.Screen) {
	ui.mu.Lock()
	ui.screen = screen
	ui.mu.Unlock()
}

func (ui *ChatUI) GetScreen() tcell.Screen {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.screen
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

func (ui *ChatUI) OnCmd(cb func(string) bool) {
	ui.mu.Lock()
	ui.onCmd = cb
	ui.mu.Unlock()
}

func (ui *ChatUI) SetCommandHistory(history []string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.cmdInput.History = history
	ui.cmdInput.HIndex = len(history)
}

func (ui *ChatUI) GetCommandHistory() []string {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.cmdInput.History
}

func (ui *ChatUI) SetPeople(people []string) {
	ui.mu.Lock()
	ui.peopleSidebar.SetItems(people)
	screen := ui.screen
	ui.mu.Unlock()

	if screen != nil {
		screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *ChatUI) SetDoors(doors []string) {
	ui.mu.Lock()
	ui.doorsSidebar.SetItems(doors)
	screen := ui.screen
	ui.mu.Unlock()

	if screen != nil {
		screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *ChatUI) GetMessages() []log.Message {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.logs.Messages
}

func (ui *ChatUI) ClearMessages() {
	ui.mu.Lock()
	ui.logs = log.NewLogView()
	ui.mu.Unlock()
	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
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

	if onClose != nil {
		onClose()
	}

	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *ChatUI) Reset() {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.closeChan = make(chan struct{}, 1)
	ui.firstDraw = true
	ui.logs.ScrollOffset = 0
}

func (ui *ChatUI) Run() string {
	if ui.Headless {
		if ui.Input != nil {
			go func() {
				scanner := bufio.NewScanner(ui.Input)
				for scanner.Scan() {
					msg := scanner.Text()
					if strings.HasPrefix(msg, "/") {
						handled := false
						ui.mu.Lock()
						onCmd := ui.onCmd
						ui.mu.Unlock()
						if onCmd != nil {
							handled = onCmd(msg)
						}
						if !handled {
							ui.Close(true)
							return
						}
					} else if ui.onSend != nil {
						ui.onSend(msg)
					}
				}
				ui.Close(false)
			}()
		}
		<-ui.closeChan
		return "" // Headless command return not supported in this simplified refactor
	}

	// Initial setup
	blackStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)
	ui.screen.SetStyle(blackStyle)
	ui.screen.Sync()
	ui.screen.Fill(' ', blackStyle)
	ui.screen.Show()

	for {
		ui.Draw()
		ev := ui.screen.PollEvent()
		if ev == nil {
			ui.Close(false)
			return ""
		}

		switch ev := ev.(type) {
		case *tcell.EventResize:
			ui.screen.Sync()
		case *tcell.EventInterrupt:
			// Just redraw
		case *tcell.EventKey:
			if ev.Key() == tcell.KeyCtrlC || ev.Key() == tcell.KeyEscape {
				ui.Close(false)
				return ""
			}

			if ev.Key() == tcell.KeyPgUp {
				ui.mu.Lock()
				ui.logs.ScrollOffset += 10
				ui.mu.Unlock()
				continue
			}
			if ev.Key() == tcell.KeyPgDn {
				ui.mu.Lock()
				ui.logs.ScrollOffset -= 10
				if ui.logs.ScrollOffset < 0 {
					ui.logs.ScrollOffset = 0
				}
				ui.mu.Unlock()
				continue
			}

			submitted, val := ui.cmdInput.HandleKey(ev)
			if submitted {
				if strings.HasPrefix(val, "/") {
					handled := false
					ui.mu.Lock()
					onCmd := ui.onCmd
					ui.mu.Unlock()
					if onCmd != nil {
						handled = onCmd(val)
					}
					if !handled {
						ui.Close(true)
						return val
					}
				} else if ui.onSend != nil {
					ui.onSend(val)
				}
			}
		}

		select {
		case <-ui.closeChan:
			return ""
		default:
		}
	}
}

func (ui *ChatUI) AddMessage(msg string, msgType MessageType) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	var lt log.MessageType
	switch msgType {
	case MsgChat:
		lt = log.MsgChat
	case MsgSelf:
		lt = log.MsgSelf
	case MsgCommand:
		lt = log.MsgCommand
	case MsgServer:
		lt = log.MsgServer
	case MsgSystem:
		lt = log.MsgSystem
	case MsgAction:
		lt = log.MsgAction
	case MsgWhisper:
		lt = log.MsgWhisper
	default:
		lt = log.MsgChat
	}

	ui.logs.AddMessage(msg, lt)
	if ui.Headless && ui.Input != nil {
		fmt.Fprintf(ui.Input, "%s\n", msg)
	}
	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *ChatUI) Draw() {
	if ui.screen == nil {
		return
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()

	s := ui.screen
	w, h := s.Size()

	blackStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)
	headerStyle := blackStyle.Foreground(tcell.ColorLightCyan).Bold(true)
	sepStyle := blackStyle.Foreground(tcell.ColorDimGray)

	s.Clear()

	// Sidebar config
	sidebarW := 18
	if w < 40 {
		sidebarW = 0
	}
	mainW := w - sidebarW - 1
	if sidebarW == 0 {
		mainW = w
	}

	// Header
	common.DrawText(s, 2, 0, ui.title, mainW-4, headerStyle)

	userStr := fmt.Sprintf("Logged in as: %s", ui.username)
	userLen := len([]rune(userStr))
	common.DrawText(s, w-userLen-2, 0, userStr, userLen, blackStyle)

	// 1. Draw horizontal separators
	for x := 0; x < w; x++ {
		s.SetContent(x, 1, '─', nil, sepStyle)
		s.SetContent(x, h-2, '─', nil, sepStyle)
	}

	// 2. Draw Sidebar
	if sidebarW > 0 {
		ui.peopleSidebar.Width = sidebarW
		ui.doorsSidebar.Width = sidebarW

		// Calculate height for doors (up to half of available space)
		maxSidebarH := h - 4
		maxDoorsH := maxSidebarH / 2
		neededDoorsH := len(ui.doorsSidebar.Items) + 1 // +1 for title
		doorsH := neededDoorsH
		if doorsH > maxDoorsH {
			doorsH = maxDoorsH
		}
		if doorsH < 1 {
			doorsH = 1
		}

		// Draw doors at top of sidebar
		ui.doorsSidebar.Draw(s, mainW, 2, doorsH, blackStyle, sepStyle)

		// Draw people below with a one-line gap
		peopleY := 2 + doorsH + 1
		peopleH := maxSidebarH - (doorsH + 1)
		if peopleH > 0 {
			ui.peopleSidebar.Draw(s, mainW, peopleY, peopleH, blackStyle, sepStyle)
		}

		// Add a separator between doors and people
		s.SetContent(mainW, 2+doorsH, '│', nil, sepStyle)
	}

	// 3. Draw Logs
	logH := h - 4
	if logH > 0 {
		if ui.logs.ScrollOffset > len(ui.logs.PhysicalLines)-logH {
			ui.logs.ScrollOffset = len(ui.logs.PhysicalLines) - logH
		}
		if ui.logs.ScrollOffset < 0 {
			ui.logs.ScrollOffset = 0
		}
		logW := w - 2
		if sidebarW > 0 {
			logW = mainW - 1
		}
		ui.logs.Draw(s, 1, 2, logW, logH, blackStyle)
	}

	// 4. Draw Connectors (last to ensure they aren't overwritten)
	if sidebarW > 0 {
		s.SetContent(mainW, 1, '┬', nil, sepStyle)
		s.SetContent(mainW, h-2, '┴', nil, sepStyle)
	}

	// 5. Draw Input
	ui.cmdInput.Draw(s, 1, h-1, w-2, blackStyle, blackStyle.Foreground(tcell.ColorGreen))

	s.Show()
}
