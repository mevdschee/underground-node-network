package entrypoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
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

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
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

type Visitor struct {
	SessionID    string
	Username     string
	TeleportData *protocol.PunchStartPayload
	UI           *ui.EntryUI
	Bus          *ui.SSHBus
	Bridge       *ui.InputBridge
}

// Server is the entry point SSH server
type Server struct {
	address       string
	usersDir      string
	config        *ssh.ServerConfig
	rooms         map[string]*Room
	visitors      map[string]*Visitor
	punchSessions map[string]*PunchSession // keyed by visitor ID
	banner        []string
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

		pubKeyStr := string(ssh.MarshalAuthorizedKey(pubKey))
		return &ssh.Permissions{
			Extensions: map[string]string{
				"registered": "true",
				"pubkey":     pubKeyStr,
			},
		}, nil
	}

	// Load or generate host key
	hostKey, err := loadOrGenerateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load host key: %w", err)
	}
	config.AddHostKey(hostKey)

	s := &Server{
		address:       address,
		usersDir:      usersDir,
		config:        config,
		rooms:         make(map[string]*Room),
		visitors:      make(map[string]*Visitor),
		punchSessions: make(map[string]*PunchSession),
	}

	// Load banner if it exists
	bannerPaths := []string{
		"banner.asc",
	}

	for _, bp := range bannerPaths {
		if b, err := os.ReadFile(bp); err == nil {
			s.banner = strings.Split(string(b), "\n")
			break
		}
	}

	return s, nil
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
					s.updateAllVisitors()
				}
				return
			} else {
				req.Reply(false, nil)
			}
		case "shell", "pty-req":
			// Captured initial size if any
			var initialW, initialH uint32 = 80, 24
			if req.Type == "pty-req" {
				if w, h, ok := ui.ParsePtyRequest(req.Payload); ok {
					initialW, initialH = w, h
				}
			}
			req.Reply(true, nil)

			// This is a visitor
			if !isOperator {
				log.Printf("Visitor connected: %s", username)

				sessionID := fmt.Sprintf("%s-%d", username, time.Now().UnixNano())
				bridge := ui.NewInputBridge(channel)
				v := &Visitor{
					SessionID: sessionID,
					Username:  username,
					Bridge:    bridge,
					Bus:       ui.NewSSHBus(bridge, int(initialW), int(initialH)),
				}
				s.mu.Lock()
				s.visitors[sessionID] = v
				s.mu.Unlock()

				defer func() {
					s.mu.Lock()
					// Only delete if it's still our session
					if current, ok := s.visitors[sessionID]; ok && current == v {
						delete(s.visitors, sessionID)
					}
					s.mu.Unlock()
				}()

				// Handle remaining requests in background to capture resize
				go func() {
					for r := range requests {
						switch r.Type {
						case "pty-req":
							if w, h, ok := ui.ParsePtyRequest(r.Payload); ok {
								v.Bus.Resize(int(w), int(h))
							}
							r.Reply(true, nil)
						case "window-change":
							if w, h, ok := ui.ParseWindowChange(r.Payload); ok {
								v.Bus.Resize(int(w), int(h))
							}
							r.Reply(true, nil)
						case "shell":
							r.Reply(true, nil)
						default:
							r.Reply(false, nil)
						}
					}
				}()
				s.handleVisitor(v, conn)
				v.Bus.ForceClose() // Ensure channel close after visitor done
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
			s.updateAllVisitors()

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
					RoomName:   session.RoomName,
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

func (s *Server) handleVisitor(v *Visitor, conn *ssh.ServerConn) {
	screen, err := tcell.NewTerminfoScreenFromTty(v.Bus)
	if err != nil {
		log.Printf("Failed to create screen for %s: %v", v.Username, err)
		return
	}
	if err := screen.Init(); err != nil {
		log.Printf("Failed to init screen for %s: %v", v.Username, err)
		return
	}
	entryUI := ui.NewEntryUI(screen, v.Username, s.address)
	v.UI = entryUI

	if len(s.banner) > 0 {
		entryUI.SetBanner(s.banner)
	}

	entryUI.OnCmd(func(cmd string) {
		s.handleVisitorCommand(v, conn, cmd)
	})

	// Add OnClose callback to break terminal deadlock
	entryUI.OnClose(func() {
		v.Bus.SignalExit()
	})

	// Initial room list
	s.updateVisitorRooms(v)

	success := entryUI.Run()
	screen.Fini() // Important: close tcell before printing to raw bus

	// Now that we've exited the TUI and restored terminal state:
	// Only show teleport info if we actually joined a room (success=true)
	if success && v.TeleportData != nil {
		s.showTeleportInfo(v)
	} else {
		// Clear screen only on manual exit to clean up the TUI artifacts
		fmt.Fprint(v.Bus, "\033[2J\033[H")
	}

	conn.Close() // Ensure immediate disconnect
}

func (s *Server) updateAllVisitors() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.visitors {
		s.updateVisitorRooms(v)
	}
}

func (s *Server) updateVisitorRooms(v *Visitor) {
	if v.UI == nil {
		return
	}
	rooms := s.GetRooms()
	uiRooms := make([]ui.RoomInfo, 0, len(rooms))
	for _, r := range rooms {
		uiRooms = append(uiRooms, ui.RoomInfo{
			Name:  r.Name,
			Owner: r.Owner,
			Doors: r.Doors,
		})
	}
	v.UI.SetRooms(uiRooms)
}

func (s *Server) handleVisitorCommand(v *Visitor, conn *ssh.ServerConn, input string) {
	log.Printf("Visitor %s command: %s", v.Username, input)
	input = strings.TrimSpace(input)
	v.UI.ShowMessage(fmt.Sprintf("> %s", input))

	if strings.HasPrefix(input, "/") {
		cmdLine := strings.TrimPrefix(input, "/")
		parts := strings.Fields(cmdLine)
		if len(parts) == 0 {
			return
		}
		command := parts[0]

		switch command {
		case "help":
			v.UI.ShowMessage("Commands: /register <key>, /help")
		case "register":
			if len(parts) < 2 {
				v.UI.ShowMessage("Usage: /register <public_key>")
				return
			}
			keyStr := strings.Join(parts[1:], " ")
			pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
			if err != nil {
				v.UI.ShowMessage(fmt.Sprintf("Invalid key: %v", err))
				return
			}
			keyPath := filepath.Join(s.usersDir, v.Username)
			if err := registerUserKey(keyPath, pubKey); err != nil {
				v.UI.ShowMessage(fmt.Sprintf("Failed: %v", err))
			} else {
				v.UI.ShowMessage("Successfully registered.")
			}
		default:
			v.UI.ShowMessage(fmt.Sprintf("Unknown command: %s", command))
		}
		return
	}

	// Try to connect to room via hole-punching
	s.mu.RLock()
	room, ok := s.rooms[input]
	s.mu.RUnlock()

	if !ok {
		v.UI.ShowMessage(fmt.Sprintf("Room not found: %s", input))
		return
	}

	// Generate visitor ID
	visitorID := fmt.Sprintf("%s-%d", v.Username, time.Now().UnixNano())

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

	visitorKey := ""
	if conn.Permissions != nil {
		visitorKey = conn.Permissions.Extensions["pubkey"]
	}

	offerPayload := protocol.PunchOfferPayload{
		VisitorID:  visitorID,
		Candidates: []string{},
		VisitorKey: visitorKey,
	}
	offerMsg, _ := protocol.NewMessage(protocol.MsgTypePunchOffer, offerPayload)

	s.mu.RLock()
	if room.Encoder != nil {
		room.Encoder.Encode(offerMsg)
	}
	s.mu.RUnlock()

	select {
	case startMsg := <-visitorChan:
		var startPayload protocol.PunchStartPayload
		if err := startMsg.ParsePayload(&startPayload); err != nil {
			v.UI.ShowMessage(fmt.Sprintf("\033[1;31mError: %v\033[0m", err))
			return
		}

		// Store data for capture after TUI exit
		v.TeleportData = &startPayload

		// Final TUI message
		v.UI.ShowMessage("")
		v.UI.ShowMessage(" \033[1;32m✔ Room joined! Teleporting...\033[0m")

		// Close the TUI loop immediately
		v.UI.Close(true)
	case <-time.After(10 * time.Second):
		v.UI.ShowMessage("Timeout waiting for room operator.")
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

func (s *Server) showTeleportInfo(v *Visitor) {
	data := v.TeleportData
	if data == nil {
		return
	}

	// Always clear screen before showing teleport info
	fmt.Fprint(v.Bus, "\033[2J\033[H")

	// Use yaml library for robust formatting
	yamlData, _ := yaml.Marshal(data)
	yamlStr := strings.ReplaceAll(string(yamlData), "\n", "\r\n")

	// The wrapper looks for these markers to capture connection info
	fmt.Fprintf(v.Bus, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\r\n")
	fmt.Fprintf(v.Bus, "[CONNECTION DATA]\r\n")
	fmt.Fprintf(v.Bus, "%s", yamlStr)
	fmt.Fprintf(v.Bus, "[/CONNECTION DATA]\r\n")
	fmt.Fprintf(v.Bus, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\r\n\r\n")

	fmt.Fprintf(v.Bus, "\033[1;32mUNN TELEPORTATION READY\033[0m\r\n\r\n")
	fmt.Fprintf(v.Bus, "The wrapper is automatically reconnecting you to the room.\r\n")
	fmt.Fprintf(v.Bus, "If the wrapper fails, you can connect manually using:\r\n\r\n")

	for _, candidate := range data.Candidates {
		fmt.Fprintf(v.Bus, "\033[1;36mssh -p %d %s\033[0m\r\n", data.SSHPort, candidate)
	}

	fmt.Fprintf(v.Bus, "\r\n\033[1mHost Verification Fingerprints:\033[0m\r\n")
	for _, key := range data.PublicKeys {
		fingerprint := s.calculateSHA256Fingerprint(key)
		fmt.Fprintf(v.Bus, "- %s\r\n", fingerprint)
	}
	fmt.Fprintf(v.Bus, "\r\n")

	// Consume the data so it's not shown again if we disconnect for other reasons
	v.TeleportData = nil
}

func (s *Server) calculateSHA256Fingerprint(keyStr string) string {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
	if err != nil {
		return "invalid key"
	}
	hash := sha256.Sum256(pubKey.Marshal())
	return "SHA256:" + base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
}
