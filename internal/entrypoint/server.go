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
	"net/http"
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
	PersonID   string
	RoomName   string
	PersonChan chan *protocol.Message // Send punch_start to person
}

type Person struct {
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
	people        map[string]*Person
	punchSessions map[string]*PunchSession // keyed by person ID
	banner        []string
	mu            sync.RWMutex
	listener      net.Listener
	headless      bool
	httpClient    *http.Client
	identities    map[string]string // keyHash -> "unnUsername platform_username@platform"
	usernames     map[string]string // unnUsername -> keyHash
}

// NewServer creates a new entry point server
func NewServer(address, hostKeyPath, usersDir string) (*Server, error) {
	config := &ssh.ServerConfig{
		NoClientAuth: false,
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
		people:        make(map[string]*Person),
		punchSessions: make(map[string]*PunchSession),
		httpClient:    http.DefaultClient,
		identities:    make(map[string]string),
		usernames:     make(map[string]string),
	}

	// Load users from single file
	s.loadUsers()

	config.PublicKeyCallback = func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
		pubKeyHash := s.calculatePubKeyHash(pubKey)
		requestedUser := c.User()

		s.mu.RLock()
		identity, verified := s.identities[pubKeyHash]
		ownerHash, taken := s.usernames[requestedUser]
		s.mu.RUnlock()

		perms := &ssh.Permissions{
			Extensions: map[string]string{
				"pubkey":     string(ssh.MarshalAuthorizedKey(pubKey)),
				"pubkeyhash": pubKeyHash,
			},
		}

		if verified {
			// identity is "unnUsername platform_username@platform"
			parts := strings.SplitN(identity, " ", 2)
			verifiedUsername := parts[0]
			platformInfo := parts[1]

			perms.Extensions["verified"] = "true"
			perms.Extensions["username"] = verifiedUsername

			pParts := strings.Split(platformInfo, "@")
			perms.Extensions["platform"] = pParts[1]
		} else {
			perms.Extensions["verified"] = "false"
		}

		// Check if requested username is taken by someone else
		if taken && ownerHash != pubKeyHash {
			perms.Extensions["taken"] = "true"
		}

		return perms, nil
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

func (s *Server) SetHeadless(headless bool) {
	s.headless = headless
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
	if sshConn.Permissions != nil && sshConn.Permissions.Extensions["verified"] == "true" {
		username = sshConn.Permissions.Extensions["username"]
	}
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

	// Process first request to determine if operator or person
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
					s.updateAllPeople()
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

			// This is a person
			if !isOperator {
				log.Printf("Person connected: %s", username)

				sessionID := fmt.Sprintf("%s-%d", username, time.Now().UnixNano())
				bridge := ui.NewInputBridge(channel)
				p := &Person{
					SessionID: sessionID,
					Username:  username,
					Bridge:    bridge,
					Bus:       ui.NewSSHBus(bridge, int(initialW), int(initialH)),
				}
				p.UI = ui.NewEntryUI(nil, p.Username, s.address)
				p.UI.Headless = s.headless
				p.UI.Input = p.Bus
				s.mu.Lock()
				s.people[sessionID] = p
				s.mu.Unlock()

				defer func() {
					s.mu.Lock()
					// Only delete if it's still our session
					if current, ok := s.people[sessionID]; ok && current == p {
						delete(s.people, sessionID)
					}
					s.mu.Unlock()
				}()

				// Handle remaining requests in background to capture resize
				go func() {
					for r := range requests {
						switch r.Type {
						case "pty-req":
							if w, h, ok := ui.ParsePtyRequest(r.Payload); ok {
								p.Bus.Resize(int(w), int(h))
							}
							r.Reply(true, nil)
						case "window-change":
							if w, h, ok := ui.ParseWindowChange(r.Payload); ok {
								p.Bus.Resize(int(w), int(h))
							}
							r.Reply(true, nil)
						case "shell":
							r.Reply(true, nil)
						default:
							r.Reply(false, nil)
						}
					}
				}()
				s.handlePerson(p, conn)
				p.Bus.ForceClose() // Ensure channel close after person done
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
			if conn.Permissions == nil || conn.Permissions.Extensions["verified"] != "true" {
				log.Printf("Rejected room registration for unverified user: %s", username)
				s.sendError(encoder, "User not verified. Please connect manually and verify your identity first.")
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
				s.mu.RLock()
				if !s.headless && entryUI.GetScreen() != nil {
					entryUI.GetScreen().Fini()
				}
				s.mu.RUnlock()
				entryUI.Close(false)
				return
			}
			verified = true
		} else if conn.Permissions != nil && conn.Permissions.Extensions["username"] != "" {
			p.Username = conn.Permissions.Extensions["username"]
			p.UI.SetUsername(p.Username)
		}

		if len(s.banner) > 0 {
			entryUI.SetBanner(s.banner)
		}

		entryUI.OnCmd(func(cmd string) {
			s.handlePersonCommand(p, conn, cmd)
		})

		// Initial room list
		s.updatePersonRooms(p)
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

	// Now that we've exited the TUI and restored terminal state:
	// Only show teleport info if we actually joined a room (success=true)
	if success && p.TeleportData != nil {
		s.showTeleportInfo(p)
	} else {
		// Clear screen only on manual exit to clean up the TUI artifacts
		// First reset colors to avoid black background spill
		fmt.Fprint(p.Bus, "\033[m\033[2J\033[H")
	}

	conn.Close() // Ensure immediate disconnect
}

func (s *Server) VerifyIdentity(platform, username string, offeredKey ssh.PublicKey) (bool, error) {
	url := ""
	switch platform {
	case "github":
		url = fmt.Sprintf("https://github.com/%s.keys", username)
	case "gitlab":
		url = fmt.Sprintf("https://gitlab.com/%s.keys", username)
	case "sourcehut":
		url = fmt.Sprintf("https://meta.sr.ht/~%s.keys", username)
	case "codeberg":
		url = fmt.Sprintf("https://codeberg.org/%s.keys", username)
	default:
		return false, fmt.Errorf("unsupported platform: %s", platform)
	}

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("platform returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}
		if bytes.Equal(pubKey.Marshal(), offeredKey.Marshal()) {
			return true, nil
		}
	}

	return false, nil
}

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
			Name:  r.Name,
			Owner: r.Owner,
			Doors: r.Doors,
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
			p.UI.ShowMessage("<room_name>         - Join a room by name", ui.MsgServer)
			p.UI.ShowMessage("Ctrl+C              - Exit", ui.MsgServer)
		case "rooms":
			s.mu.RLock()
			if len(s.rooms) == 0 {
				p.UI.ShowMessage("No active rooms.", ui.MsgServer)
			} else {
				p.UI.ShowMessage("Active Rooms:", ui.MsgServer)
				for _, room := range s.rooms {
					hash := "anonymous"
					if len(room.Info.PublicKeys) > 0 {
						hash = s.getPubKeyHash(room.Info.PublicKeys[0])
						if len(hash) > 8 {
							hash = hash[:8]
						}
					}
					p.UI.ShowMessage(fmt.Sprintf(" - %s (%s) [owned by %s]", room.Info.Name, hash, room.Info.Owner), ui.MsgServer)
				}
			}
			s.mu.RUnlock()
		default:
			p.UI.ShowMessage(fmt.Sprintf("Unknown command: %s", command), ui.MsgServer)
		}
		return
	}

	// Try to connect to room via hole-punching
	s.mu.RLock()
	room, ok := s.rooms[input]
	s.mu.RUnlock()

	if !ok {
		p.UI.ShowMessage(fmt.Sprintf("Room not found: %s", input), ui.MsgServer)
		return
	}

	// Generate person ID
	personID := fmt.Sprintf("%s-%d", p.Username, time.Now().UnixNano())

	// Create punch session
	personChan := make(chan *protocol.Message, 1)
	s.mu.Lock()
	s.punchSessions[personID] = &PunchSession{
		PersonID:   personID,
		RoomName:   input,
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

func (s *Server) calculatePubKeyHash(key ssh.PublicKey) string {
	hash := sha256.Sum256(key.Marshal())
	return fmt.Sprintf("%x", hash)
}

func (s *Server) loadUsers() {
	path := filepath.Join(s.usersDir, "users")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// format: hash unn_username platform_username@platform
		parts := strings.SplitN(line, " ", 3)
		if len(parts) == 3 {
			hash := parts[0]
			unnName := parts[1]
			platformId := parts[2]
			s.identities[hash] = fmt.Sprintf("%s %s", unnName, platformId)
			s.usernames[unnName] = hash
		}
	}
}

func (s *Server) saveUsers() error {
	if err := os.MkdirAll(s.usersDir, 0700); err != nil {
		log.Printf("Error creating users directory: %v", err)
		return err
	}
	var buf bytes.Buffer
	// We want to save both maps into the single file.
	// Since identities map contains info for all hashes, we iterate it.
	for hash, info := range s.identities {
		// info is "unnUsername platform_username@platform"
		buf.WriteString(fmt.Sprintf("%s %s\n", hash, info))
	}
	err := os.WriteFile(filepath.Join(s.usersDir, "users"), buf.Bytes(), 0600)
	if err != nil {
		log.Printf("Error saving users file: %v", err)
	}
	return err
}

func (s *Server) showTeleportInfo(p *Person) {
	data := p.TeleportData
	if data == nil {
		return
	}

	// Always clear screen before showing teleport info
	// First reset colors to avoid the black background from sticking around
	fmt.Fprint(p.Bus, "\033[m\033[2J\033[H")

	// Use yaml library for robust formatting
	yamlData, _ := yaml.Marshal(data)
	yamlStr := strings.ReplaceAll(string(yamlData), "\n", "\r\n")

	// The wrapper looks for these markers to capture connection info
	fmt.Fprintf(p.Bus, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\r\n")
	fmt.Fprintf(p.Bus, "[CONNECTION DATA]\r\n")
	fmt.Fprintf(p.Bus, "%s", yamlStr)
	fmt.Fprintf(p.Bus, "[/CONNECTION DATA]\r\n")
	fmt.Fprintf(p.Bus, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\r\n\r\n")

	fmt.Fprintf(p.Bus, "\033[1;32mUNN TELEPORTATION READY\033[0m\r\n\r\n")
	fmt.Fprintf(p.Bus, "The wrapper is automatically reconnecting you to the room.\r\n")
	fmt.Fprintf(p.Bus, "If the wrapper fails, you can connect manually using:\r\n\r\n")

	for _, candidate := range data.Candidates {
		fmt.Fprintf(p.Bus, "\033[1;36mssh -p %d %s\033[0m\r\n", data.SSHPort, candidate)
	}

	fmt.Fprintf(p.Bus, "\r\n\033[1mHost Verification Fingerprints:\033[0m\r\n\r\n")
	for _, key := range data.PublicKeys {
		fingerprint := s.calculateSHA256Fingerprint(key)
		fmt.Fprintf(p.Bus, "\033[1;36m%s\033[0m\r\n", fingerprint)
	}
	fmt.Fprintf(p.Bus, "\r\n")

	// Consume the data so it's not shown again if we disconnect for other reasons
	p.TeleportData = nil
}

func (s *Server) calculateSHA256Fingerprint(keyStr string) string {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
	if err != nil {
		return "invalid key"
	}
	algo := strings.ToUpper(strings.TrimPrefix(pubKey.Type(), "ssh-"))
	hash := sha256.Sum256(pubKey.Marshal())
	fingerprint := "SHA256:" + base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
	return fmt.Sprintf("%s key fingerprint is %s.", algo, fingerprint)
}
func (s *Server) getPubKeyHash(keyStr string) string {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
	if err != nil {
		return "invalid"
	}
	hash := sha256.Sum256(pubKey.Marshal())
	return fmt.Sprintf("%x", hash)
}
func (s *Server) handleOnboardingForm(p *Person, conn *ssh.ServerConn) bool {
	eui := p.UI
	sshUser := conn.User()

	fields := []ui.FormField{
		{Label: "Platform (github 🇺🇸, gitlab 🇺🇸, sourcehut 🇪🇺, codeberg 🇪🇺)", Value: "github"},
		{Label: "Platform Username", Value: ""},
		{Label: "UNN Username", Value: sshUser, MaxLength: 20},
	}

	for {
		results := eui.PromptForm(fields)
		if len(results) < 3 {
			return false
		}
		platform := strings.ToLower(strings.TrimSpace(results[0]))
		platformUser := strings.TrimSpace(results[1])
		unnUsername := strings.TrimSpace(results[2])

		fields[0].Value = platform
		fields[1].Value = platformUser
		fields[2].Value = unnUsername

		// Clear errors
		for i := range fields {
			fields[i].Error = ""
		}

		platforms := []string{"github", "gitlab", "sourcehut", "codeberg"}
		validPlatform := false
		for _, v := range platforms {
			if platform == v {
				validPlatform = true
				break
			}
		}
		if !validPlatform {
			fields[0].Error = "unsupported platform"
			continue
		}

		if platformUser == "" {
			fields[1].Error = "cannot be empty"
			continue
		}

		// Length check
		if len(unnUsername) < 4 {
			fields[2].Error = "too short"
			continue
		}

		pubKeyStr := conn.Permissions.Extensions["pubkey"]
		offeredKey, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(pubKeyStr))

		matched, err := s.VerifyIdentity(platform, platformUser, offeredKey)
		if err != nil {
			if strings.Contains(err.Error(), "status 404") {
				fields[1].Error = "username not found"
			} else {
				eui.ShowMessage(fmt.Sprintf("\033[1;31mError verifying identity: %v\033[0m", err), ui.MsgServer)
			}
			continue
		}

		if matched {
			pubKeyHash := s.calculatePubKeyHash(offeredKey)
			s.mu.RLock()
			ownerHash, taken := s.usernames[unnUsername]
			s.mu.RUnlock()

			if taken && ownerHash != pubKeyHash {
				fields[2].Error = "not available"
				continue
			}

			s.mu.Lock()
			s.usernames[unnUsername] = pubKeyHash
			s.identities[pubKeyHash] = fmt.Sprintf("%s %s@%s", unnUsername, platformUser, platform)
			s.saveUsers()
			s.mu.Unlock()

			p.Username = unnUsername
			p.UI.SetUsername(unnUsername)
			conn.Permissions.Extensions["verified"] = "true"
			conn.Permissions.Extensions["platform"] = platform
			conn.Permissions.Extensions["username"] = unnUsername
			return true
		} else {
			fields[1].Error = "key not found"
		}
	}
}
