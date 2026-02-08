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

				// Get the host key hash from the payload
				var payloadHostHash string
				if len(payload.PublicKeys) > 0 {
					hPubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(payload.PublicKeys[0]))
					if err == nil {
						payloadHostHash = protocol.CalculatePubKeyHash(hPubKey)
					}
				}

				// Check if the host key from the payload matches the registered one
				if payloadHostHash != "" && payloadHostHash != registeredHostHash {
					// Host key is different - check if this is the owner rotating the key
					isOwner := conn.Permissions.Extensions["verified"] == "true" && conn.Permissions.Extensions["username"] == registeredOwner
					if isOwner {
						log.Printf("Room %s host key rotated by owner %s (new hash: %s)", payload.RoomName, registeredOwner, payloadHostHash)
						registeredHostHash = payloadHostHash
					} else {
						s.mu.Unlock()
						log.Printf("Rejected room registration: %s host key mismatch (payload: %s, registered: %s)", payload.RoomName, payloadHostHash, registeredHostHash)
						s.sendError(encoder, fmt.Sprintf("Room name '%s' is already taken by another user.", payload.RoomName))
						continue
					}
				}

				username = registeredOwner

				// Update registry (handles both rotation and last-seen updates)
				s.registeredRooms[payload.RoomName] = fmt.Sprintf("%s %s %s", registeredHostHash, registeredOwner, currentDate)
				s.saveRooms()
			} else {
				// Silent auto-registration
				if !isValidRoomName(payload.RoomName) {
					s.mu.Unlock()
					s.sendError(encoder, "Invalid room name. Must be 3-20 characters, alphanumeric.")
					continue
				}

				hostKeyHash := connPubKeyHash
				if len(payload.PublicKeys) > 0 {
					hPubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(payload.PublicKeys[0]))
					if err == nil {
						hostKeyHash = protocol.CalculatePubKeyHash(hPubKey)
					}
				}

				s.registeredRooms[payload.RoomName] = fmt.Sprintf("%s %s %s", hostKeyHash, username, currentDate)
				s.saveRooms()
				log.Printf("New room auto-registered: %s by %s", payload.RoomName, username)
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
				// Broadcast updated room list to all connected people
				s.updateAllPeople()
			}

			// Send back room list
			s.sendRoomList(encoder)

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
				log.Printf("Invalid punch_answer payload: %v", err)
				continue
			}

			// Route to waiting person session
			s.mu.RLock()
			session, ok := s.punchSessions[payload.PersonID]
			s.mu.RUnlock()

			if ok && session.PersonChan != nil {
				// Convert PunchAnswer to PunchStart for the client
				startPayload := protocol.PunchStartPayload{
					RoomName:   session.RoomName,
					Candidates: payload.Candidates,
					SSHPort:    payload.SSHPort,
					PublicKeys: []string{}, // Room will provide via direct connection
				}
				startMsg, _ := protocol.NewMessage(protocol.MsgTypePunchStart, startPayload)
				select {
				case session.PersonChan <- startMsg:
					log.Printf("Routed punch_answer to person %s", payload.PersonID)
				default:
					log.Printf("Person channel full for %s", payload.PersonID)
				}
			} else {
				// client-* IDs are handled via p2pquic signaling, not punchSessions
				if !strings.HasPrefix(payload.PersonID, "client-") {
					log.Printf("No punch session found for person %s", payload.PersonID)
				}
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

func isValidRoomName(name string) bool {
	if len(name) < 3 || len(name) > 20 {
		return false
	}
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_') {
			return false
		}
	}
	return true
}
