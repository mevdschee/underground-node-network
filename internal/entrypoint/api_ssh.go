package entrypoint

import (
	"encoding/json"
	"io"
	"log"
	"strings"
	"time"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
)

// API Message Types for unn-api subsystem
const (
	APITypeRoomList     = "room_list"
	APITypeUserStatus   = "user_status"
	APITypeUserRegister = "user_register"
	APITypePreparePunch = "prepare_punch" // Request coordinated hole-punching
	APITypeResponse     = "response"
	APITypeError        = "error"
)

// APIMessage is the envelope for all API subsystem messages
type APIMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// APIUserStatusRequest queries user verification status
type APIUserStatusRequest struct {
	Username string `json:"username,omitempty"`
}

// APIUserStatusResponse contains user verification status
type APIUserStatusResponse struct {
	Verified        bool   `json:"verified"`
	Username        string `json:"username,omitempty"`
	Platform        string `json:"platform,omitempty"`
	IsTaken         bool   `json:"is_taken"`
	TakenByPlatform string `json:"taken_by_platform,omitempty"`
}

// APIUserRegisterRequest registers a new user identity
type APIUserRegisterRequest struct {
	UNNUsername  string `json:"unn_username"`
	PlatformInfo string `json:"platform_info"` // e.g. "user@github"
}

// APIUserRegisterResponse confirms registration
type APIUserRegisterResponse struct {
	Status   string `json:"status"`
	Username string `json:"username"`
}

// handleAPI processes the unn-api SSH subsystem
// This handles room queries and user registration
func (s *Server) handleAPI(channel ssh.Channel, conn *ssh.ServerConn) {
	defer channel.Close()

	decoder := json.NewDecoder(channel)
	encoder := json.NewEncoder(channel)

	log.Printf("API subsystem connection from %s", conn.User())

	for {
		var msg APIMessage
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				log.Printf("API decode error: %v", err)
			}
			return
		}

		switch msg.Type {
		case APITypeRoomList:
			s.handleAPIRoomList(encoder)

		case APITypeUserStatus:
			var req APIUserStatusRequest
			if err := json.Unmarshal(msg.Payload, &req); err != nil {
				s.sendAPIError(encoder, "invalid user_status payload")
				continue
			}
			s.handleAPIUserStatus(encoder, conn, req)

		case APITypeUserRegister:
			var req APIUserRegisterRequest
			if err := json.Unmarshal(msg.Payload, &req); err != nil {
				s.sendAPIError(encoder, "invalid user_register payload")
				continue
			}
			s.handleAPIUserRegister(encoder, conn, req)

		case APITypePreparePunch:
			var req struct {
				RoomName         string   `json:"room_name"`
				ClientPeerID     string   `json:"client_peer_id"`
				ClientCandidates []string `json:"client_candidates"`
			}
			if err := json.Unmarshal(msg.Payload, &req); err != nil {
				s.sendAPIError(encoder, "invalid prepare_punch payload")
				continue
			}

			// Trigger coordinated hole-punching (pass conn for server-reflexive IP)
			if err := s.SendPunchPrepare(req.RoomName, req.ClientPeerID, req.ClientCandidates, conn); err != nil {
				s.sendAPIError(encoder, err.Error())
			} else {
				encoder.Encode(APIMessage{
					Type: APITypeResponse,
					Payload: mustMarshal(map[string]string{
						"status": "punch_coordinated",
					}),
				})
			}

		default:
			s.sendAPIError(encoder, "unknown message type: "+msg.Type)
		}
	}
}

// handleAPIRoomList returns the list of active rooms
func (s *Server) handleAPIRoomList(encoder *json.Encoder) {
	s.mu.RLock()
	rooms := make([]protocol.RoomInfo, 0, len(s.rooms))
	for _, room := range s.rooms {
		rooms = append(rooms, room.Info)
	}
	s.mu.RUnlock()

	payload, _ := json.Marshal(rooms)
	encoder.Encode(APIMessage{
		Type:    APITypeResponse,
		Payload: payload,
	})
}

// handleAPIUserStatus checks user verification status and username availability
func (s *Server) handleAPIUserStatus(encoder *json.Encoder, conn *ssh.ServerConn, req APIUserStatusRequest) {
	pubKeyHash := conn.Permissions.Extensions["pubkeyhash"]

	s.mu.RLock()
	identity, verified := s.identities[pubKeyHash]
	ownerPlatform, taken := s.usernames[req.Username]
	s.mu.RUnlock()

	response := APIUserStatusResponse{
		Verified: verified,
		IsTaken:  taken,
	}

	if verified {
		// identity format: "unnUsername platform_username@platform lastSeenDate"
		fields := strings.Fields(identity)
		if len(fields) >= 2 {
			response.Username = fields[0]
			platformParts := strings.Split(fields[1], "@")
			if len(platformParts) == 2 {
				response.Platform = platformParts[1]
			}
		}
	}

	if taken {
		response.TakenByPlatform = ownerPlatform
	}

	payload, _ := json.Marshal(response)
	encoder.Encode(APIMessage{
		Type:    APITypeResponse,
		Payload: payload,
	})
}

// handleAPIUserRegister registers a new user identity
func (s *Server) handleAPIUserRegister(encoder *json.Encoder, conn *ssh.ServerConn, req APIUserRegisterRequest) {
	pubKeyHash := conn.Permissions.Extensions["pubkeyhash"]

	// Validate username format
	if !isValidUsername(req.UNNUsername) {
		s.sendAPIError(encoder, "invalid username format")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if username is already taken by a different platform account
	if ownerPlatform, taken := s.usernames[req.UNNUsername]; taken {
		if ownerPlatform != req.PlatformInfo {
			s.sendAPIError(encoder, "username already taken by "+ownerPlatform)
			return
		}
	}

	// TODO: Verify the platform info against GitHub/GitLab OAuth
	// For now, we'll accept any registration (development mode)
	log.Printf("WARNING: Accepting user registration without verification: %s -> %s", pubKeyHash, req.UNNUsername)

	// Store the identity
	currentDate := time.Now().Format("2006-01-02")
	s.identities[pubKeyHash] = req.UNNUsername + " " + req.PlatformInfo + " " + currentDate
	s.usernames[req.UNNUsername] = req.PlatformInfo
	s.saveUsers()

	log.Printf("User registered via API: %s as %s (%s)", pubKeyHash, req.UNNUsername, req.PlatformInfo)

	response := APIUserRegisterResponse{
		Status:   "registered",
		Username: req.UNNUsername,
	}

	payload, _ := json.Marshal(response)
	encoder.Encode(APIMessage{
		Type:    APITypeResponse,
		Payload: payload,
	})
}

func (s *Server) sendAPIError(encoder *json.Encoder, message string) {
	payload, _ := json.Marshal(map[string]string{"message": message})
	encoder.Encode(APIMessage{
		Type:    APITypeError,
		Payload: payload,
	})
}

func isValidUsername(username string) bool {
	if len(username) < 3 || len(username) > 20 {
		return false
	}
	for _, char := range username {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_') {
			return false
		}
	}
	return true
}
