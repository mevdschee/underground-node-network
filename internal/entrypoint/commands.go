package entrypoint

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"golang.org/x/crypto/ssh"
)

func (s *Server) updateAllPeople() {
	s.mu.RLock()
	people := make([]*Person, 0, len(s.people))
	for _, p := range s.people {
		people = append(people, p)
	}
	s.mu.RUnlock()

	rooms := s.GetRooms()
	for _, p := range people {
		s.updatePersonRoomsWithData(p, rooms)
	}
}

func (s *Server) updatePersonRooms(p *Person) {
	s.updatePersonRoomsWithData(p, s.GetRooms())
}

func (s *Server) updatePersonRoomsWithData(p *Person, rooms []protocol.RoomInfo) {
	if p.UI == nil {
		return
	}
	uiRooms := make([]ui.RoomInfo, 0, len(rooms))
	for _, r := range rooms {
		uiRooms = append(uiRooms, ui.RoomInfo{
			Name:        r.Name,
			Owner:       r.Owner,
			Doors:       r.Doors,
			PeopleCount: r.PeopleCount,
		})
	}
	p.UI.SetRooms(uiRooms)
}

func (s *Server) showMessage(p *Person, text string, msgType ui.MessageType) {
	if p.UI != nil {
		p.UI.ShowMessage(text, msgType)
	}
	if p.PubKeyHash != "" {
		s.addMessageToHistory(p.PubKeyHash, ui.Message{Text: text, Type: msgType})
	}
}

func (s *Server) handlePersonCommand(p *Person, conn *ssh.ServerConn, input string) {
	log.Printf("Person %s command: %s", p.Username, input)
	input = strings.TrimSpace(input)

	if p.PubKeyHash != "" {
		s.addCommandToHistory(p.PubKeyHash, input)
	}

	s.showMessage(p, fmt.Sprintf("> %s", input), ui.MsgCommand)

	if strings.HasPrefix(input, "/") {
		cmdLine := strings.TrimPrefix(input, "/")
		parts := strings.Fields(cmdLine)
		if len(parts) == 0 {
			return
		}
		command := parts[0]

		switch command {
		case "help":
			s.showMessage(p, "/help                     - Show this help message", ui.MsgServer)
			s.showMessage(p, "/rooms                    - List all active rooms", ui.MsgServer)
			s.showMessage(p, "/join <room_name>         - Join a room by name", ui.MsgServer)
			s.showMessage(p, "/quit                     - Exit", ui.MsgServer)
			s.showMessage(p, "Ctrl+C                    - Exit", ui.MsgServer)
		case "join":
			if len(parts) < 2 {
				s.showMessage(p, "Usage: /join <room_name>", ui.MsgServer)
				return
			}
			s.handleRoomJoin(p, conn, parts[1])
		case "rooms":
			s.mu.RLock()
			var rooms []protocol.RoomInfo
			for _, room := range s.rooms {
				rooms = append(rooms, room.Info)
			}
			s.mu.RUnlock()

			if len(rooms) == 0 {
				s.showMessage(p, "No rooms found.", ui.MsgServer)
			} else {
				s.showMessage(p, "Rooms:", ui.MsgServer)
				for _, room := range rooms {
					s.showMessage(p, fmt.Sprintf("• %s (%d) @%s", room.Name, room.PeopleCount, room.Owner), ui.MsgServer)
				}
			}
		case "quit", "exit":
			p.UI.Close(false)
		default:
			s.showMessage(p, fmt.Sprintf("Unknown command: %s", command), ui.MsgServer)
		}
		return
	}

	// Not a command - this is a chat message attempt
	s.showMessage(p, "Chat is disabled in the entry point.", ui.MsgServer)
	s.showMessage(p, "Use /rooms to list rooms and /join <room> to join.", ui.MsgServer)
}

func (s *Server) handleRoomJoin(p *Person, conn *ssh.ServerConn, roomName string) {
	// Try to connect to room via hole-punching
	s.mu.RLock()
	room, ok := s.rooms[roomName]
	s.mu.RUnlock()

	if !ok {
		s.showMessage(p, fmt.Sprintf("Room not found: %s", roomName), ui.MsgServer)
		return
	}

	// Generate person ID
	personID := fmt.Sprintf("%s-%d", p.Username, time.Now().UnixNano())

	// Create punch session
	personChan := make(chan *protocol.Message, 1)
	s.mu.Lock()
	s.punchSessions[personID] = &PunchSession{
		PersonID:   personID,
		RoomName:   roomName,
		PersonChan: personChan,
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.punchSessions, personID)
		s.mu.Unlock()
	}()

	personKey := ""
	if conn.Permissions != nil {
		personKey = conn.Permissions.Extensions["pubkey"]
	}

	// For P2P auth, room operator needs the user's "global" identity if verified
	displayName := p.Username
	if conn.Permissions != nil && conn.Permissions.Extensions["verified"] == "true" {
		displayName = fmt.Sprintf("%s (%s)", conn.Permissions.Extensions["username"], conn.Permissions.Extensions["platform"])
	}

	unnUsername := p.Username
	if conn.Permissions != nil && conn.Permissions.Extensions["username"] != "" {
		unnUsername = conn.Permissions.Extensions["username"]
	}

	offerPayload := protocol.PunchOfferPayload{
		PersonID:    personID,
		Candidates:  []string{},
		PersonKey:   personKey,
		DisplayName: displayName,
		Username:    unnUsername,
	}
	offerMsg, _ := protocol.NewMessage(protocol.MsgTypePunchOffer, offerPayload)

	s.mu.RLock()
	if room.Encoder != nil {
		room.Encoder.Encode(offerMsg)
	}
	s.mu.RUnlock()

	select {
	case startMsg := <-personChan:
		var startPayload protocol.PunchStartPayload
		if err := startMsg.ParsePayload(&startPayload); err != nil {
			s.showMessage(p, fmt.Sprintf("Error: %v", err), ui.MsgServer)
			return
		}

		// Store data for teleportation
		p.TeleportData = &startPayload

		// Send OSC teleport data to UNN-aware clients
		common.SendOSC(p.Bus, "teleport", map[string]interface{}{
			"room_name":   startPayload.RoomName,
			"candidates":  startPayload.Candidates,
			"ssh_port":    startPayload.SSHPort,
			"public_keys": startPayload.PublicKeys,
		})

		// Final TUI message
		s.showMessage(p, "Room joined! Teleporting...", ui.MsgSystem)

		// Close the TUI loop immediately
		p.UI.Close(true)
	case <-time.After(10 * time.Second):
		s.showMessage(p, "Timeout waiting for room operator.", ui.MsgServer)
	}
}

func (s *Server) handlePerson(p *Person, conn *ssh.ServerConn) {
	entryUI := p.UI
	if !s.headless {
		screen, err := tcell.NewTerminfoScreenFromTty(p.Bus)
		if err != nil {
			log.Printf("Failed to create screen for %s: %v", p.Username, err)
			return
		}
		if err := screen.Init(); err != nil {
			log.Printf("Failed to init screen for %s: %v", p.Username, err)
			return
		}
		entryUI.SetScreen(screen)
	}

	// Handle verification and command setup in background so entryUI.Run() can start
	go func() {
		verified := conn.Permissions != nil && conn.Permissions.Extensions["verified"] == "true"

		if !verified {
			if !s.handleOnboardingForm(p, conn) {
				entryUI.Close(false)
				// Give the UI a moment to show "exiting" or similar, then force close connection
				go func() {
					time.Sleep(500 * time.Millisecond)
					p.Conn.Close()
				}()
				return
			}
			verified = true
		} else if conn.Permissions != nil && conn.Permissions.Extensions["username"] != "" {
			p.Username = conn.Permissions.Extensions["username"]
			p.UI.SetUsername(p.Username)
		}

		entryUI.OnCmd(func(cmd string) {
			s.handlePersonCommand(p, conn, cmd)
		})

		// Initial room list
		s.updatePersonRooms(p)

		if p.PubKeyHash != "" {
			s.mu.RLock()
			chatHistory := s.histories[p.PubKeyHash]
			cmdHistory := s.cmdHistories[p.PubKeyHash]
			s.mu.RUnlock()

			if len(chatHistory) == 0 && len(s.banner) > 0 {
				for _, line := range s.banner {
					text := strings.TrimRight(line, "\r\n")
					s.addMessageToHistory(p.PubKeyHash, ui.Message{Text: text, Type: ui.MsgServer})
				}
				// Re-fetch history after adding banner
				s.mu.RLock()
				chatHistory = s.histories[p.PubKeyHash]
				s.mu.RUnlock()
			}

			if len(chatHistory) > 0 {
				entryUI.SetChatHistory(chatHistory)
			}
			if len(cmdHistory) > 0 {
				entryUI.SetCommandHistory(cmdHistory)
			}
		}

		// Process initial command after onboarding is done
		if p.InitialCommand != "" {
			s.handlePersonCommand(p, conn, p.InitialCommand)
		}
	}()

	// Add OnClose callback to break terminal deadlock
	entryUI.OnClose(func() {
		p.Bus.SignalExit()
	})

	success := entryUI.Run()

	// Explicitly finalize screen immediately after Run() to restore terminal state
	s.mu.RLock()
	if !s.headless && entryUI.GetScreen() != nil {
		entryUI.GetScreen().Fini()
		// Send ANSI reset to ensure the terminal background is restored
		fmt.Fprint(p.Bus, "\033[m")
	}
	s.mu.RUnlock()

	// Clear screen on exit to clean up the TUI artifacts
	// First reset colors to avoid black background spill
	fmt.Fprint(p.Bus, "\033[m\033[2J\033[H")

	// If success (joined a room), keep connection open for client to do signaling
	// Client will close when done. If not success, close immediately.
	if !success {
		conn.Close()
	}
}

func (s *Server) addMessageToHistory(pubHash string, msg ui.Message) {
	if pubHash == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.histories[pubHash]
	history = append(history, msg)
	if len(history) > 200 {
		history = history[1:]
	}
	s.histories[pubHash] = history
}

func (s *Server) addCommandToHistory(pubHash string, cmd string) {
	if pubHash == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.cmdHistories[pubHash]
	// Avoid duplicate consecutive commands
	if len(history) > 0 && history[len(history)-1] == cmd {
		return
	}
	history = append(history, cmd)
	if len(history) > 100 {
		history = history[1:]
	}
	s.cmdHistories[pubHash] = history
}
