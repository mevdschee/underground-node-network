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

		// Store data for capture after TUI exit
		p.TeleportData = &startPayload

		// Final TUI message
		s.showMessage(p, "Room joined! Teleporting...", ui.MsgSystem)

		// Close the TUI loop immediately
		p.UI.Close(true)
	case <-time.After(10 * time.Second):
		s.showMessage(p, "Timeout waiting for room operator.", ui.MsgServer)
	}
}
