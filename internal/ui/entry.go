package ui

import (
	"bufio"
	"fmt"
	"io"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/banner"
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
	banner        *banner.Banner
	cmdInput      *input.CommandInput
	registration  *form.Form
	passwordInput *password.PasswordEntry // Corrected type

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
	scrollOffset int
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
		cmdInput:   input.NewCommandInput(""),
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
		items = append(items, fmt.Sprintf("%-12s @%s", r.Name, r.Owner))
	}
	ui.roomsDataSpec = sidebar.NewSidebar("Active rooms:", 25)
	ui.roomsDataSpec.SetItems(items)

	if ui.screen != nil {
		ui.screen.PostEvent(&tcell.EventInterrupt{})
	}
}

func (ui *EntryUI) SetBanner(lines []string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.banner = banner.NewBanner(lines)

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
}

func (ui *EntryUI) SetUsername(username string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.username = username
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
		switch ev := ev.(type) {
		case *tcell.EventResize:
			ui.screen.Sync()
		case *tcell.EventInterrupt:
			// Just redraw
		case *tcell.EventKey:
			done, success := ui.HandleKeyResult(ev)
			if done {
				return success
			}
		}

		select {
		case <-ui.closeChan:
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
	if ui.banner != nil {
		ui.banner.Draw(s, 0, 1, mainW, blackStyle)
		sepY = 1 + ui.banner.Height()
	}

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

	// Separator
	for x := 0; x < w; x++ {
		s.SetContent(x, sepY, '━', nil, sepStyle)
	}

	// Sidebar
	if sidebarW > 0 && ui.roomsDataSpec != nil {
		ui.roomsDataSpec.Width = sidebarW
		ui.roomsDataSpec.Draw(s, mainW, sepY+1, h-sepY-2, blackStyle, sepStyle)
		s.SetContent(mainW, sepY, '┳', nil, sepStyle)
	}

	// Logs
	logH := h - 2 - sepY
	if logH > 0 {
		ui.logs.Draw(s, 1, sepY+1, mainW-2, logH, blackStyle)
	}

	// Input
	ui.cmdInput.Draw(s, 1, h-1, w-2, blackStyle, blackStyle.Foreground(tcell.ColorGreen))

	s.Show()
}

func (ui *EntryUI) HandleKeyResult(ev *tcell.EventKey) (done bool, success bool) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	if ui.InFormMode && ui.registration != nil {
		submitted, vals := ui.registration.HandleKey(ev)
		if submitted {
			ui.FormResult <- vals
		}
		return false, false
	}

	if ui.InFormMode && ui.passwordInput != nil {
		submitted, val := ui.passwordInput.HandleKey(ev)
		if submitted {
			ui.FormResult <- []string{val}
		}
		return false, false
	}

	if ev.Key() == tcell.KeyCtrlC {
		if ui.onExit != nil {
			ui.onExit()
		}
		return true, false
	}

	if ev.Key() == tcell.KeyPgUp {
		ui.scrollOffset += 10
		return false, false
	}
	if ev.Key() == tcell.KeyPgDn {
		ui.scrollOffset -= 10
		if ui.scrollOffset < 0 {
			ui.scrollOffset = 0
		}
		return false, false
	}

	submitted, val := ui.cmdInput.HandleKey(ev)
	if submitted {
		if ui.prompt != "" {
			ui.prompt = ""
			ui.promptChan <- val
		} else if ui.onCmd != nil {
			ui.onCmd(val)
		}
	}

	return false, false
}
