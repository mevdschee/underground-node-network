package entrypoint

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
)

func (s *Server) handleOperator(channel ssh.Channel, conn *ssh.ServerConn, username string, roomName *string) {
	decoder := json.NewDecoder(channel)
	encoder := json.NewEncoder(channel)

	for {
		var msg protocol.Message
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				log.Printf("Error reading from operator: %v", err)
			}
			return
		}

		switch msg.Type {
		case protocol.MsgTypeRegister:
			var payload protocol.RegisterPayload
			if err := msg.ParsePayload(&payload); err != nil {
				s.sendError(encoder, "invalid register payload")
				continue
			}

			s.mu.Lock()
			currentDate := time.Now().Format("2006-01-02")
			connPubKeyHash := conn.Permissions.Extensions["pubkeyhash"]

			if info, ok := s.registeredRooms[payload.RoomName]; ok {
				parts := strings.Split(info, " ")
				// format: hostKeyHash owner date
				registeredHostHash := parts[0]
				registeredOwner := parts[1]

				if registeredHostHash != connPubKeyHash {
					s.mu.Unlock()
					log.Printf("Rejected room registration: %s host key mismatch (connection: %s, registered: %s)", payload.RoomName, connPubKeyHash, registeredHostHash)
					s.sendError(encoder, "Unauthorized host key for this room name.")
					continue
				}

				// If we have no owner in registration yet (unlikely with manual registration),
				// or if we want to allow the current connection to be identifying as the room,
				// we use the registered owner for metadata.
				username = registeredOwner

				// Update last seen date
				s.registeredRooms[payload.RoomName] = fmt.Sprintf("%s %s %s", registeredHostHash, registeredOwner, currentDate)
				s.saveRooms()
			} else {
				// Rejected: Room must be pre-registered
				s.mu.Unlock()
				log.Printf("Rejected room registration: %s not pre-registered (attempted by key %s)", payload.RoomName, connPubKeyHash)
				s.sendError(encoder, "Room name not registered. Use /register in the entrypoint first.")
				continue
			}

			_, alreadyOnline := s.rooms[payload.RoomName]
			*roomName = payload.RoomName

			s.rooms[payload.RoomName] = &Room{
				Info: protocol.RoomInfo{
					Name:        payload.RoomName,
					Owner:       username,
					Doors:       payload.Doors,
					Candidates:  payload.Candidates,
					SSHPort:     payload.SSHPort,
					PublicKeys:  payload.PublicKeys,
					PeopleCount: payload.PeopleCount,
				},
				Connection: conn,
				Channel:    channel,
				Encoder:    encoder,
			}
			s.mu.Unlock()

			if !alreadyOnline {
				log.Printf("Room online: %s by %s", payload.RoomName, username)
			}

			// Send back room list
			s.sendRoomList(encoder)
			s.updateAllPeople()

		case protocol.MsgTypeUnregister:
			if *roomName != "" {
				s.mu.Lock()
				delete(s.rooms, *roomName)
				s.mu.Unlock()
				log.Printf("Room unregistered: %s", *roomName)
				*roomName = ""
			}

		case protocol.MsgTypePunchAnswer:
			// Room operator sent back candidates for hole-punching
			var payload protocol.PunchAnswerPayload
			if err := msg.ParsePayload(&payload); err != nil {
				continue
			}

			s.mu.RLock()
			session, ok := s.punchSessions[payload.PersonID]
			s.mu.RUnlock()

			if ok {
				// Look up room keys
				var publicKeys []string
				s.mu.RLock()
				if room, exists := s.rooms[session.RoomName]; exists {
					publicKeys = room.Info.PublicKeys
				}
				s.mu.RUnlock()

				// Send punch_start to person with room's candidates
				startPayload := protocol.PunchStartPayload{
					RoomName:   session.RoomName,
					Candidates: payload.Candidates,
					SSHPort:    payload.SSHPort,
					PublicKeys: publicKeys,
				}
				startMsg, _ := protocol.NewMessage(protocol.MsgTypePunchStart, startPayload)
				session.PersonChan <- startMsg
				log.Printf("Punch start sent to person %s", payload.PersonID)
			}
		}
	}
}

func (s *Server) sendRoomList(encoder *json.Encoder) {
	s.mu.RLock()
	rooms := make([]protocol.RoomInfo, 0, len(s.rooms))
	for _, room := range s.rooms {
		rooms = append(rooms, room.Info)
	}
	s.mu.RUnlock()

	msg, _ := protocol.NewMessage(protocol.MsgTypeRoomList, rooms)
	encoder.Encode(msg)
}

func (s *Server) sendError(encoder *json.Encoder, message string) {
	payload := protocol.ErrorPayload{Message: message}
	msg, _ := protocol.NewMessage(protocol.MsgTypeError, payload)
	encoder.Encode(msg)
}
