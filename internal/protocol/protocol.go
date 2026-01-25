package protocol

import (
	"encoding/json"
)

// Message types for entry point protocol
const (
	MsgTypeRegister    = "register"
	MsgTypeUnregister  = "unregister"
	MsgTypeRoomList    = "room_list"
	MsgTypeConnect     = "connect"
	MsgTypeConnectInfo = "connect_info"
	MsgTypeError       = "error"

	// Hole-punching signaling
	MsgTypePunchRequest = "punch_request" // Visitor requests to punch to room
	MsgTypePunchOffer   = "punch_offer"   // Entry point forwards to room
	MsgTypePunchAnswer  = "punch_answer"  // Room sends candidates back
	MsgTypePunchStart   = "punch_start"   // Both sides start punching
)

// Message is the base message structure for entry point communication
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// RegisterPayload is sent when a node registers with the entry point
type RegisterPayload struct {
	RoomName   string   `json:"room_name"`
	Doors      []string `json:"doors"`
	Candidates []string `json:"candidates"`  // NAT traversal candidates (IP only)
	SSHPort    int      `json:"ssh_port"`    // Local SSH server port
	PublicKeys []string `json:"public_keys"` // SSH public keys (authorized_keys format)
}

// RoomInfo represents an active room in the network
type RoomInfo struct {
	Name       string   `json:"name"`
	Owner      string   `json:"owner"`
	Doors      []string `json:"doors"`
	Candidates []string `json:"candidates"`
	SSHPort    int      `json:"ssh_port"`
	PublicKeys []string `json:"public_keys"`
}

// RoomListPayload contains the list of active rooms
type RoomListPayload struct {
	Rooms []RoomInfo `json:"rooms"`
}

// ConnectPayload is sent when a visitor wants to connect to a room
type ConnectPayload struct {
	RoomName string `json:"room_name"`
}

// ConnectInfoPayload is sent back with connection details
type ConnectInfoPayload struct {
	RoomName   string   `json:"room_name"`
	Candidates []string `json:"candidates"`
	SSHPort    int      `json:"ssh_port"`
}

// ErrorPayload is sent when an error occurs
type ErrorPayload struct {
	Message string `json:"message"`
}

// PunchRequestPayload is sent by visitor to initiate hole-punching
type PunchRequestPayload struct {
	RoomName   string   `json:"room_name"`
	Candidates []string `json:"candidates"` // Visitor's candidates
	VisitorID  string   `json:"visitor_id"` // Unique ID for this punch session
}

// PunchOfferPayload is forwarded from entry point to room operator
type PunchOfferPayload struct {
	VisitorID  string   `json:"visitor_id"`
	Candidates []string `json:"candidates"`
}

// PunchAnswerPayload is sent by room operator back to entry point
type PunchAnswerPayload struct {
	VisitorID  string   `json:"visitor_id"`
	Candidates []string `json:"candidates"`
	SSHPort    int      `json:"ssh_port"`
}

// PunchStartPayload tells both sides to start hole-punching
type PunchStartPayload struct {
	Candidates []string `json:"candidates"`  // Remote peer's candidates
	SSHPort    int      `json:"ssh_port"`    // Remote SSH port (for room)
	PublicKeys []string `json:"public_keys"` // Remote peer's public keys
	StartTime  int64    `json:"start_time"`  // Unix timestamp to sync start
}

// NewMessage creates a new message with the given type and payload
func NewMessage(msgType string, payload interface{}) (*Message, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{
		Type:    msgType,
		Payload: payloadBytes,
	}, nil
}

// ParsePayload unmarshals the payload into the given struct
func (m *Message) ParsePayload(v interface{}) error {
	return json.Unmarshal(m.Payload, v)
}
