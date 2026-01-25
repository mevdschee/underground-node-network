package entrypoint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
)

// Room represents a registered room
type Room struct {
	Info       protocol.RoomInfo
	Connection *ssh.ServerConn
	Channel    ssh.Channel // For sending messages to operator
	Encoder    *json.Encoder
}

// PunchSession tracks an active hole-punch negotiation
type PunchSession struct {
	VisitorID   string
	RoomName    string
	VisitorChan chan *protocol.Message // Send punch_start to visitor
}

// Server is the entry point SSH server
type Server struct {
	address       string
	usersDir      string
	config        *ssh.ServerConfig
	rooms         map[string]*Room
	punchSessions map[string]*PunchSession // keyed by visitor ID
	mu            sync.RWMutex
	listener      net.Listener
}

// NewServer creates a new entry point server
func NewServer(address, hostKeyPath, usersDir string) (*Server, error) {
	config := &ssh.ServerConfig{
		NoClientAuth: false,
	}

	config.PublicKeyCallback = func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
		username := c.User()
		// Sanitize
		if !isValidUsername(username) {
			return nil, fmt.Errorf("invalid username: only alphanumeric characters allowed")
		}

		keyPath := filepath.Join(usersDir, username)

		// Check if user exists
		if _, err := os.Stat(keyPath); os.IsNotExist(err) {
			// Manual registration required
			return &ssh.Permissions{
				Extensions: map[string]string{"registered": "false"},
			}, nil
		}

		// Verify key
		valid, err := verifyUserKey(keyPath, pubKey)
		if err != nil {
			log.Printf("Failed to verify user %s: %v", username, err)
			return nil, fmt.Errorf("internal verification error")
		}
		if !valid {
			return nil, fmt.Errorf("public key mismatch for user %s", username)
		}

		return &ssh.Permissions{
			Extensions: map[string]string{"registered": "true"},
		}, nil
	}

	// Load or generate host key
	hostKey, err := loadOrGenerateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load host key: %w", err)
	}
	config.AddHostKey(hostKey)

	return &Server{
		address:       address,
		usersDir:      usersDir,
		config:        config,
		rooms:         make(map[string]*Room),
		punchSessions: make(map[string]*PunchSession),
	}, nil
}

// Start begins listening for SSH connections
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.address, err)
	}
	s.listener = listener
	log.Printf("Entry point listening on %s", s.address)

	go s.acceptLoop()
	return nil
}

// Stop stops the server
func (s *Server) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// GetRooms returns a list of active rooms
func (s *Server) GetRooms() []protocol.RoomInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rooms := make([]protocol.RoomInfo, 0, len(s.rooms))
	for _, room := range s.rooms {
		rooms = append(rooms, room.Info)
	}
	return rooms
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("Failed to accept connection: %v", err)
			}
			return
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		if err != io.EOF {
			log.Printf("Failed SSH handshake: %v", err)
		}
		return
	}
	defer sshConn.Close()

	username := sshConn.User()
	log.Printf("Connection from: %s", username)

	// Discard global requests
	go ssh.DiscardRequests(reqs)

	// Handle channels
	for newChannel := range chans {
		go s.handleChannel(newChannel, sshConn, username)
	}
}

func (s *Server) handleChannel(newChannel ssh.NewChannel, conn *ssh.ServerConn, username string) {
	channelType := newChannel.ChannelType()

	switch channelType {
	case "session":
		s.handleSession(newChannel, conn, username)
	default:
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", channelType))
	}
}

func (s *Server) handleSession(newChannel ssh.NewChannel, conn *ssh.ServerConn, username string) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("Could not accept session: %v", err)
		return
	}
	defer channel.Close()

	var roomName string
	var isOperator bool

	// Process first request to determine if operator or visitor
	for req := range requests {
		switch req.Type {
		case "subsystem":
			subsystem := string(req.Payload[4:])
			if subsystem == "unn-control" {
				req.Reply(true, nil)
				isOperator = true
				log.Printf("Operator connected: %s", username)
				// Handle operator - this will block until disconnect
				s.handleOperator(channel, conn, username, &roomName)
				// Clean up room when operator disconnects
				if roomName != "" {
					s.mu.Lock()
					delete(s.rooms, roomName)
					s.mu.Unlock()
					log.Printf("Room unregistered: %s", roomName)
				}
				return
			} else {
				req.Reply(false, nil)
			}
		case "shell", "pty-req":
			req.Reply(true, nil)
			// This is a visitor
			if !isOperator {
				log.Printf("Visitor connected: %s", username)
				// Handle remaining requests in background
				go func() {
					for r := range requests {
						if r.Type == "shell" || r.Type == "pty-req" {
							r.Reply(true, nil)
						} else {
							r.Reply(false, nil)
						}
					}
				}()
				s.handleVisitor(channel, username)
				return
			}
		default:
			req.Reply(false, nil)
		}
	}
}

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

			// Enforce registration
			if conn.Permissions == nil || conn.Permissions.Extensions["registered"] != "true" {
				log.Printf("Rejected room registration for unregistered user: %s", username)
				s.sendError(encoder, "User not registered. Please connect manually and /register first.")
				continue
			}

			s.mu.Lock()
			*roomName = payload.RoomName
			s.rooms[payload.RoomName] = &Room{
				Info: protocol.RoomInfo{
					Name:       payload.RoomName,
					Owner:      username,
					Doors:      payload.Doors,
					Candidates: payload.Candidates,
					SSHPort:    payload.SSHPort,
					PublicKeys: payload.PublicKeys,
				},
				Connection: conn,
				Channel:    channel,
				Encoder:    encoder,
			}
			s.mu.Unlock()

			log.Printf("Room registered: %s by %s", payload.RoomName, username)

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
				continue
			}

			s.mu.RLock()
			session, ok := s.punchSessions[payload.VisitorID]
			s.mu.RUnlock()

			if ok {
				// Look up room keys
				var publicKeys []string
				s.mu.RLock()
				if room, exists := s.rooms[session.RoomName]; exists {
					publicKeys = room.Info.PublicKeys
				}
				s.mu.RUnlock()

				// Send punch_start to visitor with room's candidates
				startPayload := protocol.PunchStartPayload{
					Candidates: payload.Candidates,
					SSHPort:    payload.SSHPort,
					PublicKeys: publicKeys,
				}
				startMsg, _ := protocol.NewMessage(protocol.MsgTypePunchStart, startPayload)
				session.VisitorChan <- startMsg
				log.Printf("Punch start sent to visitor %s", payload.VisitorID)
			}
		}
	}
}

func (s *Server) handleVisitor(channel ssh.Channel, username string) {
	// Welcome message
	fmt.Fprintf(channel, "\r\n")
	fmt.Fprintf(channel, "╔═══════════════════════════════════════════════════════════════╗\r\n")
	fmt.Fprintf(channel, "║  Underground Node Network - Entry Point                       ║\r\n")
	fmt.Fprintf(channel, "╚═══════════════════════════════════════════════════════════════╝\r\n")
	fmt.Fprintf(channel, "\r\n")
	fmt.Fprintf(channel, "Welcome, %s!\r\n\r\n", username)

	s.showRooms(channel)
	fmt.Fprintf(channel, "\r\nType a room name to connect, or /help for commands.\r\n\r\n")

	// Interaction loop
	buf := make([]byte, 1024)
	var line []byte
	var history []string
	historyIndex := -1
	currentLineBackup := ""
	escState := 0

	for {
		n, err := channel.Read(buf)
		if err != nil {
			return
		}

		for i := 0; i < n; i++ {
			b := buf[i]

			// Handle ANSI escape sequences for arrow keys
			if b == 27 {
				escState = 1
				continue
			}
			if escState == 1 && b == '[' {
				escState = 2
				continue
			}
			if escState == 2 {
				escState = 0
				if b == 'A' { // Arrow Up
					if len(history) > 0 && historyIndex != 0 {
						if historyIndex == -1 || historyIndex == len(history) {
							currentLineBackup = string(line)
							historyIndex = len(history)
						}
						historyIndex--
						// Clear line: \r (carriage return) + \x1b[K (clear to end of line)
						fmt.Fprintf(channel, "\r\x1b[K")
						line = []byte(history[historyIndex])
						channel.Write(line)
					}
					continue
				} else if b == 'B' { // Arrow Down
					if historyIndex != -1 && historyIndex < len(history) {
						historyIndex++
						fmt.Fprintf(channel, "\r\x1b[K")
						if historyIndex == len(history) {
							line = []byte(currentLineBackup)
						} else {
							line = []byte(history[historyIndex])
						}
						channel.Write(line)
					}
					continue
				}
				// Other escape sequences ignored for now
				continue
			}
			escState = 0

			switch b {
			case '\r', '\n':
				fmt.Fprintf(channel, "\r\n")
				if len(line) > 0 {
					cmd := string(line)
					s.handleVisitorCommand(channel, username, cmd)
					// Add to history if it's different from the last one
					if len(history) == 0 || history[len(history)-1] != cmd {
						history = append(history, cmd)
					}
					historyIndex = len(history)
					line = nil
				}
			case 127, 8: // Backspace
				if len(line) > 0 {
					line = line[:len(line)-1]
					fmt.Fprintf(channel, "\b \b")
				}
			case 3: // Ctrl+C
				return
			default:
				line = append(line, b)
				channel.Write([]byte{b})
			}
		}
	}
}

func (s *Server) showRooms(w io.Writer) {
	rooms := s.GetRooms()

	if len(rooms) == 0 {
		fmt.Fprintf(w, "No active rooms.\r\n")
		return
	}

	fmt.Fprintf(w, "Active rooms:\r\n")
	for _, room := range rooms {
		doorStr := ""
		if len(room.Doors) > 0 {
			doorStr = fmt.Sprintf(" [%s]", strings.Join(room.Doors, ", "))
		}
		fmt.Fprintf(w, "  • %s (by %s)%s\r\n", room.Name, room.Owner, doorStr)
	}
}

func (s *Server) handleVisitorCommand(channel ssh.Channel, username string, input string) {
	input = strings.TrimSpace(input)

	if strings.HasPrefix(input, "/") {
		cmdLine := strings.TrimPrefix(input, "/")
		parts := strings.Fields(cmdLine)
		if len(parts) == 0 {
			return
		}
		command := parts[0]

		switch command {
		case "help":
			fmt.Fprintf(channel, "\rCommands:\r\n")
			fmt.Fprintf(channel, "  /rooms           - List active rooms\r\n")
			fmt.Fprintf(channel, "  /register <key>  - Register your public key\r\n")
			fmt.Fprintf(channel, "  /help            - Show this help\r\n")
			fmt.Fprintf(channel, "  <room>           - Connect to a room\r\n")
			fmt.Fprintf(channel, "  Ctrl+C           - Exit\r\n")
		case "rooms":
			s.showRooms(channel)
		case "register":
			if len(parts) < 2 {
				fmt.Fprintf(channel, "\rUsage: /register <public_key>\r\n")
				return
			}
			// Re-join the key parts (type blob [comment])
			keyStr := strings.Join(parts[1:], " ")
			// ParseAuthorizedKey handles "type blob comment"
			pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
			if err != nil {
				fmt.Fprintf(channel, "\rInvalid public key format: %v\r\n", err)
				return
			}

			keyPath := filepath.Join(s.usersDir, username)
			// Check if already registered?
			// If file exists, we imply authentication passed (so we are the owner).
			// So we allow overwrite/update.
			// If file doesn't exist, we are claiming it.

			if err := registerUserKey(keyPath, pubKey); err != nil {
				fmt.Fprintf(channel, "\rRegistration failed: %v\r\n", err)
			} else {
				fmt.Fprintf(channel, "\rSuccessfully registered user '%s'.\r\n", username)
				log.Printf("User registered manually: %s", username)
			}

		default:
			fmt.Fprintf(channel, "\rUnknown command: %s\r\n", command)
		}
		return
	}

	// Try to connect to room via hole-punching
	s.mu.RLock()
	room, ok := s.rooms[input]
	s.mu.RUnlock()

	if !ok {
		fmt.Fprintf(channel, "\rRoom not found: %s\r\n", input)
		return
	}

	// Initiate hole-punch

	// Generate visitor ID for this punch session
	visitorID := fmt.Sprintf("%s-%d", username, time.Now().UnixNano())

	// Create punch session
	visitorChan := make(chan *protocol.Message, 1)
	s.mu.Lock()
	s.punchSessions[visitorID] = &PunchSession{
		VisitorID:   visitorID,
		RoomName:    input,
		VisitorChan: visitorChan,
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.punchSessions, visitorID)
		s.mu.Unlock()
	}()

	// Send punch_offer to room operator
	offerPayload := protocol.PunchOfferPayload{
		VisitorID:  visitorID,
		Candidates: []string{}, // Visitor doesn't have STUN in CLI mode
	}
	offerMsg, _ := protocol.NewMessage(protocol.MsgTypePunchOffer, offerPayload)

	s.mu.RLock()
	if room.Encoder != nil {
		room.Encoder.Encode(offerMsg)
	}
	s.mu.RUnlock()

	// Wait for punch_start with timeout
	select {
	case startMsg := <-visitorChan:
		var startPayload protocol.PunchStartPayload
		if err := startMsg.ParsePayload(&startPayload); err != nil {
			fmt.Fprintf(channel, "Error: %v\r\n", err)
			return
		}

		fmt.Fprintf(channel, "\r\nConnect using unn-ssh wrapper:\r\n")

		entryAddr := s.address
		if strings.HasPrefix(entryAddr, "0.0.0.0") {
			entryAddr = "localhost" + entryAddr[7:]
		}

		fmt.Fprintf(channel, "  ssh://%s/%s\r\n", entryAddr, input)

		fmt.Fprintf(channel, "\r\n[CONNECTION_DATA]\r\n")
		fmt.Fprintf(channel, "Candidates: %s\r\n", strings.Join(startPayload.Candidates, ","))
		fmt.Fprintf(channel, "SSHPort: %d\r\n", startPayload.SSHPort)
		for _, k := range startPayload.PublicKeys {
			// Trim to remove any trailing newlines from key files
			fmt.Fprintf(channel, "HostKey: %s\r\n", strings.TrimSpace(k))
		}
		fmt.Fprintf(channel, "[/CONNECTION_DATA]\r\n")

		log.Printf("Sent connection data to visitor %s for room %s", username, input)

		fmt.Fprintf(channel, "\r\nOr connect directly:\r\n")
		var hostKey string
		if len(startPayload.PublicKeys) > 0 {
			hostKey = strings.TrimSpace(startPayload.PublicKeys[0])
		}
		for _, candidate := range startPayload.Candidates {
			// Extract IP if it's in the old format (type:ip:port)
			ip := candidate
			if strings.Count(candidate, ":") >= 2 {
				parts := strings.Split(candidate, ":")
				if len(parts) == 3 {
					ip = parts[1]
				}
			}

			if hostKey != "" {
				hostSpec := "unn-room"
				if startPayload.SSHPort != 22 {
					hostSpec = fmt.Sprintf("[unn-room]:%d", startPayload.SSHPort)
				}
				fmt.Fprintf(channel, "  ssh -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/dev/fd/3 -o GlobalKnownHostsFile=/dev/null -o CheckHostIP=no -o HostKeyAlias=unn-room -p %d %s@%s 3<<<\"%s %s\"\r\n",
					startPayload.SSHPort, username, ip, hostSpec, hostKey)
			} else {
				fmt.Fprintf(channel, "  ssh -p %d %s@%s\r\n", startPayload.SSHPort, username, ip)
			}
		}

	case <-time.After(30 * time.Second):
		fmt.Fprintf(channel, "Timeout waiting for room operator\r\n")
	}
}

func (s *Server) sendRoomList(encoder *json.Encoder) {
	payload := protocol.RoomListPayload{
		Rooms: s.GetRooms(),
	}
	msg, _ := protocol.NewMessage(protocol.MsgTypeRoomList, payload)
	encoder.Encode(msg)
}

func (s *Server) sendError(encoder *json.Encoder, message string) {
	payload := protocol.ErrorPayload{Message: message}
	msg, _ := protocol.NewMessage(protocol.MsgTypeError, payload)
	encoder.Encode(msg)
}

func loadOrGenerateHostKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err == nil {
		return ssh.ParsePrivateKey(keyBytes)
	}

	log.Printf("Generating new host key at %s", path)
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", path, "-N", "", "-q")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ssh-keygen failed: %w", err)
	}

	keyBytes, err = os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return ssh.ParsePrivateKey(keyBytes)
}

func isValidUsername(name string) bool {
	if len(name) == 0 || len(name) > 32 {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func registerUserKey(path string, key ssh.PublicKey) error {
	// Create directory if not exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create users directory: %w", err)
	}

	// Marshal key to authorized_keys format
	keyBytes := ssh.MarshalAuthorizedKey(key)
	if err := os.WriteFile(path, keyBytes, 0600); err != nil {
		return fmt.Errorf("failed to write user key: %w", err)
	}
	return nil
}

func verifyUserKey(path string, offeredKey ssh.PublicKey) (bool, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("failed to read user key: %w", err)
	}

	storedKey, _, _, _, err := ssh.ParseAuthorizedKey(keyBytes)
	if err != nil {
		return false, fmt.Errorf("failed to parse stored key: %w", err)
	}

	return bytes.Equal(storedKey.Marshal(), offeredKey.Marshal()), nil
}
