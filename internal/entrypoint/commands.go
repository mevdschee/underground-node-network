package entrypoint

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"golang.org/x/crypto/ssh"
)

func (s *Server) updateAllPeople() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.people {
		s.updatePersonRooms(p)
	}
}

func (s *Server) updatePersonRooms(p *Person) {
	if p.UI == nil {
		return
	}
	rooms := s.GetRooms()
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

func (s *Server) handlePersonCommand(p *Person, conn *ssh.ServerConn, input string) {
	log.Printf("Person %s command: %s", p.Username, input)
	input = strings.TrimSpace(input)
	p.UI.ShowMessage(fmt.Sprintf("> %s", input), ui.MsgCommand)

	if strings.HasPrefix(input, "/") {
		cmdLine := strings.TrimPrefix(input, "/")
		parts := strings.Fields(cmdLine)
		if len(parts) == 0 {
			return
		}
		command := parts[0]

		switch command {
		case "help":
			p.UI.ShowMessage("/help               - Show this help message", ui.MsgServer)
			p.UI.ShowMessage("/rooms              - List all active rooms", ui.MsgServer)
			p.UI.ShowMessage("/join <room_name>   - Join a room by name", ui.MsgServer)
			p.UI.ShowMessage("Ctrl+C              - Exit", ui.MsgServer)
		case "join":
			if len(parts) < 2 {
				p.UI.ShowMessage("Usage: /join <room_name>", ui.MsgServer)
				return
			}
			s.handleRoomJoin(p, conn, parts[1])
		case "rooms":
			s.mu.RLock()
			if len(s.rooms) == 0 {
				p.UI.ShowMessage("No rooms found.", ui.MsgServer)
			} else {
				p.UI.ShowMessage("Rooms:", ui.MsgServer)
				for _, room := range s.rooms {
					p.UI.ShowMessage(fmt.Sprintf("• %s (%d) @%s", room.Info.Name, room.Info.PeopleCount, room.Info.Owner), ui.MsgServer)
				}
			}
			s.mu.RUnlock()
		default:
			p.UI.ShowMessage(fmt.Sprintf("Unknown command: %s", command), ui.MsgServer)
		}
		return
	}

	// Not a command - this is a chat message attempt
	p.UI.ShowMessage("Chat is disabled in the entry point.", ui.MsgServer)
	p.UI.ShowMessage("Use /rooms to list rooms and /join <room> to join.", ui.MsgServer)
}

func (s *Server) handleRoomJoin(p *Person, conn *ssh.ServerConn, roomName string) {
	// Try to connect to room via hole-punching
	s.mu.RLock()
	room, ok := s.rooms[roomName]
	s.mu.RUnlock()

	if !ok {
		p.UI.ShowMessage(fmt.Sprintf("Room not found: %s", roomName), ui.MsgServer)
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
			p.UI.ShowMessage(fmt.Sprintf("\033[1;31mError: %v\033[0m", err), ui.MsgServer)
			return
		}

		// Store data for capture after TUI exit
		p.TeleportData = &startPayload

		// Final TUI message
		p.UI.ShowMessage("", ui.MsgSystem)
		p.UI.ShowMessage(" \033[1;32m✔ Room joined! Teleporting...\033[0m", ui.MsgSystem)

		// Close the TUI loop immediately
		p.UI.Close(true)
	case <-time.After(10 * time.Second):
		p.UI.ShowMessage("Timeout waiting for room operator.", ui.MsgServer)
	}
}
