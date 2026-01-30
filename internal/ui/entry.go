package ui

import (
	"bufio"
	"fmt"
	"io"
	stdlog "log"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"github.com/mevdschee/underground-node-network/internal/ui/form"
	"github.com/mevdschee/underground-node-network/internal/ui/input"
	"github.com/mevdschee/underground-node-network/internal/ui/log"
	"github.com/mevdschee/underground-node-network/internal/ui/password"
	"github.com/mevdschee/underground-node-network/internal/ui/sidebar"
)

type EntryUI struct {
	screen        tcell.Screen
	roomsDataSpec *sidebar.Sidebar
	sidebars      map[string]*sidebar.Sidebar
	logs          *log.LogView
	cmdInput      *input.CommandInput
	registration  *form.Form
	passwordInput *password.PasswordEntry

	roomsData []RoomInfo

	prompt         string
	promptChan     chan string
	LogsOnly       bool // If true, only draw logs full-screen (for verification flow)
	CenteredPrompt bool // If true, draw the prompt in a centered box
	input          string
	username       string
	addr           string
	history        []string
	hIndex         int
	lastWidth      int
	lastLogCount   int
	mu             sync.Mutex
	onCmd          func(string)
	onExit         func()
	onClose        func()
	closeChan      chan struct{}
	success        bool
	Headless       bool
	Input          io.ReadWriter

	// SCROLLING
	physicalLogs []log.Message // Wrapped lines

	// FORM MODE
	InFormMode    bool
	FormFields    []form.FormField
	FormActiveIdx int
	FormResult    chan []string
	cursorIdx     int
	inputOffset   int
	draft         string
}

func NewEntryUI(screen tcell.Screen, username, addr string) *EntryUI {
	return &EntryUI{
		screen:     screen,
		username:   username,
		addr:       addr,
		promptChan: make(chan string),
		logs:       log.NewLogView(),
		cmdInput:   input.NewCommandInput(">"),
		sidebars:   make(map[string]*sidebar.Sidebar), // To be cleaned up
		closeChan:  make(chan struct{}, 1),
		FormResult: make(chan []string),
	}
}

func (ui *EntryUI) OnCmd(cb func(string)) {
	ui.mu.Lock()
	ui.onCmd = cb
	ui.mu.Unlock()
}

func (ui *EntryUI) SetCommandHistory(history []string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.cmdInput.History = history
	ui.cmdInput.HIndex = len(history)
}

func (ui *EntryUI) GetCommandHistory() []string {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.cmdInput.History
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
	defer ui.mu.Unlock()
	ui.roomsData = rooms

	var items []string
	for _, r := range rooms {
		items = append(items, fmt.Sprintf("%s (%d)", r.Name, r.PeopleCount))
	}
	ui.roomsDataSpec = sidebar.NewSidebar("Rooms:", 25)
	ui.roomsDataSpec.SetItems(items)

	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *EntryUI) SetBanner(lines []string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	for _, line := range lines {
		ui.logs.AddMessage(line, log.MsgServer)
	}

	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *EntryUI) ShowMessage(msg string, msgType MessageType) {
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

func (ui *EntryUI) SetUsername(username string) {
	ui.mu.Lock()
	ui.username = username
	screen := ui.screen
	ui.mu.Unlock()
	if screen != nil {
		screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *EntryUI) Prompt(q string) string {
	ui.mu.Lock()
	ui.prompt = q
	ui.cmdInput.Prompt = q
	ui.mu.Unlock()

	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}

	return <-ui.promptChan
}

func (ui *EntryUI) PromptForm(fields []form.FormField) []string {
	ui.mu.Lock()
	ui.InFormMode = true
	ui.registration = form.NewForm("IDENTITY VERIFICATION", fields)
	ui.mu.Unlock()

	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}

	res := <-ui.FormResult

	ui.mu.Lock()
	ui.InFormMode = false
	ui.registration = nil
	ui.mu.Unlock()

	return res
}

func (ui *EntryUI) PromptPassword(q string) string {
	ui.mu.Lock()
	ui.InFormMode = true
	ui.passwordInput = password.NewPasswordEntry(q)
	ui.mu.Unlock()

	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}

	res := <-ui.FormResult

	ui.mu.Lock()
	ui.InFormMode = false
	ui.passwordInput = nil
	ui.mu.Unlock()

	if len(res) > 0 {
		return res[0]
	}
	return ""
}

func (ui *EntryUI) GetLogs() []log.Message {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.logs.Messages
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

	defer func() {
		if ui.onClose != nil {
			ui.onClose()
		}
	}()

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
			stdlog.Printf("EntryUI: PollEvent returned nil, exiting loop")
			return ui.success
		}
		switch ev := ev.(type) {
		case *tcell.EventResize:
			ui.screen.Sync()
		case *tcell.EventInterrupt:
			// Just redraw
		case *tcell.EventKey:
			done, success := ui.HandleKeyResult(ev)
			if done {
				stdlog.Printf("EntryUI: HandleKeyResult requested exit (success=%v)", success)
				return success
			}
		}

		select {
		case <-ui.closeChan:
			stdlog.Printf("EntryUI: closeChan triggered exit (success=%v)", ui.success)
			return ui.success
		default:
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
	sepStyle := blackStyle.Foreground(tcell.ColorDimGray)

	s.Clear()

	if ui.InFormMode && ui.registration != nil {
		ui.registration.Draw(s, w, h, blackStyle)
		s.Show()
		return
	}

	if ui.InFormMode && ui.passwordInput != nil {
		ui.passwordInput.Draw(s, w, h, blackStyle)
		s.Show()
		return
	}

	// Sidebar config
	sidebarW := 25
	if w < 60 {
		sidebarW = 0
	}
	mainW := w - sidebarW - 1
	if sidebarW == 0 {
		mainW = w
	}

	// Draw Header
	title := "Underground Node Network - Entry Point"
	common.DrawText(s, 2, 0, title, mainW-4, headerStyle)

	userStr := fmt.Sprintf("Logged in as: %s", ui.username)
	userLen := len([]rune(userStr))
	common.DrawText(s, w-userLen-2, 0, userStr, userLen, blackStyle)

	sepY := 1

	if ui.LogsOnly {
		logH := h - 2 - sepY
		if logH > 0 {
			ui.logs.Draw(s, 1, sepY+1, w-2, logH, blackStyle)
		}
		if ui.prompt != "" {
			ui.cmdInput.Draw(s, 1, h-1, w-2, blackStyle.Foreground(tcell.ColorGreen), blackStyle.Foreground(tcell.ColorGreen))
		}
		s.Show()
		return
	}

	// 1. Draw horizontal separators
	for x := 0; x < w; x++ {
		s.SetContent(x, sepY, '─', nil, sepStyle)
		s.SetContent(x, h-2, '─', nil, sepStyle)
	}

	// 2. Draw Sidebar
	if sidebarW > 0 && ui.roomsDataSpec != nil {
		ui.roomsDataSpec.Width = sidebarW
		ui.roomsDataSpec.Draw(s, mainW, sepY+1, h-sepY-3, blackStyle, sepStyle)
	}

	// 3. Draw Logs
	logH := h - 3 - sepY
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
		ui.logs.Draw(s, 1, sepY+1, logW, logH, blackStyle)
	}

	// 4. Draw Connectors (last to ensure they aren't overwritten)
	if sidebarW > 0 {
		s.SetContent(mainW, sepY, '┬', nil, sepStyle)
		s.SetContent(mainW, h-2, '┴', nil, sepStyle)
	}

	// 5. Draw Input
	ui.cmdInput.Draw(s, 1, h-1, w-2, blackStyle, blackStyle.Foreground(tcell.ColorGreen))

	s.Show()
}

func (ui *EntryUI) HandleKeyResult(ev *tcell.EventKey) (done bool, success bool) {
	ui.mu.Lock()

	if ui.InFormMode && ui.registration != nil {
		submitted, vals := ui.registration.HandleKey(ev)
		if submitted {
			ch := ui.FormResult
			ui.mu.Unlock()
			ch <- vals
			return false, false
		}
		ui.mu.Unlock()
		return false, false
	}

	if ui.InFormMode && ui.passwordInput != nil {
		done, val, canceled := ui.passwordInput.HandleKey(ev)
		if done {
			ch := ui.FormResult
			ui.mu.Unlock()
			if canceled {
				ch <- nil
			} else {
				ch <- []string{val}
			}
			return false, false
		}
		ui.mu.Unlock()
		return false, false
	}

	onExit := ui.onExit
	onCmd := ui.onCmd
	prompt := ui.prompt
	promptChan := ui.promptChan

	if ev.Key() == tcell.KeyCtrlC {
		ui.mu.Unlock()
		if onExit != nil {
			onExit()
		}
		return true, false
	}

	if ev.Key() == tcell.KeyPgUp {
		ui.logs.ScrollOffset += 10
		ui.mu.Unlock()
		return false, false
	}
	if ev.Key() == tcell.KeyPgDn {
		ui.logs.ScrollOffset -= 10
		if ui.logs.ScrollOffset < 0 {
			ui.logs.ScrollOffset = 0
		}
		ui.mu.Unlock()
		return false, false
	}

	submitted, val := ui.cmdInput.HandleKey(ev)
	if submitted {
		if prompt != "" {
			ui.prompt = ""
			ui.cmdInput.Prompt = ">"
			ui.mu.Unlock()
			promptChan <- val
			return false, false
		} else if onCmd != nil {
			ui.mu.Unlock()
			onCmd(val)
			return false, false
		}
	}

	ui.mu.Unlock()
	return false, false
}
