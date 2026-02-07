package entrypoint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/nat"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"github.com/mevdschee/underground-node-network/internal/ui/bridge"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"github.com/quic-go/quic-go"
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
	PersonID   string
	RoomName   string
	PersonChan chan *protocol.Message // Send punch_start to person
}

type Person struct {
	SessionID      string
	Username       string
	TeleportData   *protocol.PunchStartPayload
	UI             *ui.EntryUI
	DisplayName    string
	Bus            *bridge.SSHBus
	Bridge         *bridge.InputBridge
	UNNAware       bool
	PubKey         ssh.PublicKey
	PubKeyHash     string
	Conn           *ssh.ServerConn
	InitialCommand string
}

// Server is the entry point SSH server
type Server struct {
	address         string
	usersDir        string
	config          *ssh.ServerConfig
	rooms           map[string]*Room
	people          map[string]*Person
	punchSessions   map[string]*PunchSession // keyed by person ID
	banner          []string
	mu              sync.RWMutex
	quicListener    *nat.QUICListener
	headless        bool
	httpClient      *http.Client
	identities      map[string]string       // keyHash -> "unnUsername platform_username@platform"
	usernames       map[string]string       // unnUsername -> platformOwner (e.g. user@github)
	registeredRooms map[string]string       // roomName -> "hostKeyHash ownerUsername lastSeenDate"
	histories       map[string][]ui.Message // keyed by pubkey hash (hex)
	cmdHistories    map[string][]string     // keyed by pubkey hash (hex)
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
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		identities:      make(map[string]string),
		usernames:       make(map[string]string),
		registeredRooms: make(map[string]string),
		histories:       make(map[string][]ui.Message),
		cmdHistories:    make(map[string][]string),
	}

	// Load data from files
	s.loadUsers()
	s.loadRooms()

	config.PublicKeyCallback = func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
		pubKeyHash := s.calculatePubKeyHash(pubKey)
		requestedUser := c.User()

		s.mu.RLock()
		identity, verified := s.identities[pubKeyHash]
		ownerPlatform, taken := s.usernames[requestedUser]
		s.mu.RUnlock()

		perms := &ssh.Permissions{
			Extensions: map[string]string{
				"pubkey":     string(ssh.MarshalAuthorizedKey(pubKey)),
				"pubkeyhash": pubKeyHash,
			},
		}

		if verified {
			// identity is "unnUsername platform_username@platform [lastSeenDate]"
			fields := strings.Fields(identity)
			verifiedUsername := fields[0]
			platformInfo := fields[1]

			perms.Extensions["verified"] = "true"
			perms.Extensions["username"] = verifiedUsername

			pParts := strings.Split(platformInfo, "@")
			perms.Extensions["platform"] = pParts[1]

			// Update last seen
			currentDate := time.Now().Format("2006-01-02")
			s.mu.Lock()
			s.identities[pubKeyHash] = fmt.Sprintf("%s %s %s", verifiedUsername, platformInfo, currentDate)
			s.saveUsers()
			s.mu.Unlock()
		} else {
			perms.Extensions["verified"] = "false"
		}

		// Check if requested username is taken by a different platform account
		if taken {
			isOwner := false
			if verified {
				fields := strings.Fields(identity)
				platformInfo := fields[1]
				if ownerPlatform == platformInfo {
					isOwner = true
				}
			}
			if !isOwner {
				perms.Extensions["taken"] = "true"
			}
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

	if len(s.banner) == 0 {
		s.banner = []string{
			"Welcome to the UndergrouNd Network Entry Point!",
			"",
			"Chat is disabled here. Please join a room to interact with others.",
			"Use /rooms to see available rooms.",
			"Use /join <room_name> to enter a room.",
		}
	}

	return s, nil
}

func (s *Server) SetHeadless(headless bool) {
	s.headless = headless
}

// Start begins listening for QUIC connections
func (s *Server) Start() error {
	// Parse address to get port
	_, portStr, err := net.SplitHostPort(s.address)
	if err != nil {
		return fmt.Errorf("invalid address %s: %w", s.address, err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return fmt.Errorf("invalid port in address %s: %w", s.address, err)
	}

	// Create QUIC listener
	quicListener, err := nat.NewQUICListener(port)
	if err != nil {
		return fmt.Errorf("failed to create QUIC listener on %s: %w", s.address, err)
	}
	s.quicListener = quicListener
	log.Printf("Entry point listening on %s (QUIC/UDP)", s.address)

	go s.acceptLoop()
	return nil
}

// Stop stops the server
func (s *Server) Stop() error {
	if s.quicListener != nil {
		return s.quicListener.Close()
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
		// Accept QUIC connection
		quicConn, err := s.quicListener.Accept(context.Background())
		if err != nil {
			if !strings.Contains(err.Error(), "server closed") {
				log.Printf("Failed to accept QUIC connection: %v", err)
			}
			return
		}

		// Accept a stream for SSH
		go func(qc *quic.Conn) {
			stream, err := qc.AcceptStream(context.Background())
			if err != nil {
				log.Printf("Failed to accept stream: %v", err)
				return
			}

			// Wrap stream as net.Conn for SSH
			conn := nat.NewQUICStreamConn(stream, qc)
			s.handleConnection(conn)
		}(quicConn)
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

	var roomName string
	var isOperator bool

	// Process first request to determine if operator or person
	pubKey := conn.Permissions.Extensions["pubkey"]
	pubKeyHash := ""
	var parsedPubKey ssh.PublicKey
	if pubKey != "" {
		parsedPubKey, _, _, _, _ = ssh.ParseAuthorizedKey([]byte(pubKey))
		if parsedPubKey != nil {
			pubKeyHash = s.calculatePubKeyHash(parsedPubKey)
		}
	}

	for req := range requests {
		switch req.Type {
		case "subsystem":
			subsystem := string(req.Payload[4:])
			if subsystem == "unn-control" {
				req.Reply(true, nil)
				// Disconnect old operator with same key
				s.mu.Lock()
				for name, room := range s.rooms {
					if room.Connection != nil && room.Connection.Permissions.Extensions["pubkey"] == pubKey {
						log.Printf("Disconnecting old operator for room %s (new connection with same key)", name)
						room.Connection.Close()
					}
				}
				s.mu.Unlock()

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
		case "pty-req":
			// Captured initial size if any
			var initialW, initialH uint32 = 80, 24
			if req.Type == "pty-req" {
				if w, h, ok := common.ParsePtyRequest(req.Payload); ok {
					initialW, initialH = w, h
				}
			}
			req.Reply(true, nil)

			// This is a person
			if !isOperator {
				log.Printf("Person connected: %s", username)

				sessionID := fmt.Sprintf("%s-%d", username, time.Now().UnixNano())
				inputBridge := bridge.NewInputBridge(channel)
				p := &Person{
					SessionID: sessionID,
					Username:  username,
					TeleportData: &protocol.PunchStartPayload{
						RoomName: "lobby", // Default
					},
					Bridge:     inputBridge,
					Bus:        bridge.NewSSHBus(inputBridge, int(initialW), int(initialH)),
					UNNAware:   strings.Contains(string(conn.ClientVersion()), "UNN"),
					PubKey:     parsedPubKey,
					PubKeyHash: pubKeyHash,
					Conn:       conn,
				}
				p.UI = ui.NewEntryUI(nil, p.Username, s.address)
				p.UI.Headless = s.headless
				p.UI.Input = p.Bus

				s.mu.Lock()
				// Disconnect old person with same key
				if pubKeyHash != "" {
					for xid, old := range s.people {
						if old.PubKeyHash == pubKeyHash {
							log.Printf("Disconnecting old person session %s for user %s (new connection)", xid, username)
							s.SendOSC(old, "popup", map[string]interface{}{
								"title":   "Duplicate Session",
								"message": "You have been disconnected because you connected from another session.",
								"type":    "warning",
							})
							// Give it a moment to send
							go func(c *ssh.ServerConn) {
								time.Sleep(200 * time.Millisecond)
								c.Close()
							}(old.Conn)
						}
					}
				}
				s.people[sessionID] = p
				s.mu.Unlock()

				// Handle remaining requests in background to capture resize and ack shell
				go func() {
					for r := range requests {
						switch r.Type {
						case "pty-req":
							if w, h, ok := common.ParsePtyRequest(r.Payload); ok {
								p.Bus.Resize(int(w), int(h))
							}
							r.Reply(true, nil)
						case "window-change":
							if w, h, ok := common.ParseWindowChange(r.Payload); ok {
								p.Bus.Resize(int(w), int(h))
							}
							r.Reply(true, nil)
						case "shell":
							// Ack traditional interactive request for client compatibility
							r.Reply(true, nil)
						default:
							r.Reply(false, nil)
						}
					}
				}()

				// Main interaction session
				go func() {
					defer func() {
						s.mu.Lock()
						if current, ok := s.people[sessionID]; ok && current == p {
							delete(s.people, sessionID)
						}
						s.mu.Unlock()
						p.Bus.ForceClose()
					}()
					s.handlePerson(p, conn)
				}()
				return
			}
		default:
			req.Reply(false, nil)
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
