package entrypoint

import (
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

	"github.com/mevdschee/p2pquic-go/pkg/signaling"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"github.com/mevdschee/underground-node-network/internal/ui/bridge"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
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

// Person represents a connected user with a TUI session
type Person struct {
	SessionID      string
	Username       string
	TeleportData   *protocol.PunchStartPayload
	UI             *ui.EntryUI
	DisplayName    string
	Bus            *bridge.SSHBus
	Bridge         *bridge.InputBridge
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
	tcpListener     net.Listener      // TCP listener for SSH connections
	signalingServer *signaling.Server // signaling server for p2pquic peers
	httpClient      *http.Client

	mu              sync.RWMutex
	rooms           map[string]*Room         // room name -> *Room
	people          map[string]*Person       // session ID -> *Person
	punchSessions   map[string]*PunchSession // keyed by person ID
	identities      map[string]string        // keyHash -> "unnUsername platform_username@platform"
	usernames       map[string]string        // unnUsername -> platformOwner (e.g. user@github)
	registeredRooms map[string]string        // roomName -> "hostKeyHash ownerUsername lastSeenDate"
	histories       map[string][]ui.Message  // keyed by pubkey hash (hex)
	cmdHistories    map[string][]string      // keyed by pubkey hash (hex)
	banner          []string
	headless        bool
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

	// Parse address to get port for p2pquic peer
	_, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", address, err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return nil, fmt.Errorf("invalid port in address %s: %w", address, err)
	}

	// Initialize signaling server for p2pquic
	signalingServer := signaling.NewServer()

	s := &Server{
		address:         address,
		usersDir:        usersDir,
		config:          config,
		rooms:           make(map[string]*Room),
		people:          make(map[string]*Person),
		punchSessions:   make(map[string]*PunchSession),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		signalingServer: signalingServer,
		identities:      make(map[string]string),
		usernames:       make(map[string]string),
		registeredRooms: make(map[string]string),
		histories:       make(map[string][]ui.Message),
		cmdHistories:    make(map[string][]string),
	}

	// Load data from files
	s.loadUsers()
	s.loadRooms()
	s.loadBanner()

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

	return s, nil
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

	// Create TCP listener for SSH
	tcpListener, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("failed to create TCP listener on %s: %w", s.address, err)
	}
	s.tcpListener = tcpListener
	log.Printf("Entry point listening on %s (SSH/TCP)", s.address)
	log.Printf("P2PQUIC signaling server ready (entrypoint is signaling-only, not a peer)")

	go s.acceptLoop()
	return nil
}

// Stop stops the server
func (s *Server) Stop() error {
	// Stop the signaling server cleanup goroutine
	if s.signalingServer != nil {
		s.signalingServer.Close()
	}
	if s.tcpListener != nil {
		return s.tcpListener.Close()
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
		// Accept TCP connection
		tcpConn, err := s.tcpListener.Accept()
		if err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("Failed to accept TCP connection: %v", err)
			}
			return
		}

		// Handle SSH connection directly over TCP
		go s.handleConnection(tcpConn)
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

	// Parse public key for session tracking
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
			req.Reply(true, nil)

			switch subsystem {
			case "unn-control":
				// Room operator registration
				log.Printf("Operator %s connected via unn-control subsystem", username)

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
				var roomName string
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

			case "unn-api":
				// Client API queries (room list, user status, registration)
				log.Printf("Client %s connected via unn-api subsystem", username)
				s.handleAPI(channel, conn)
				return

			case "unn-signaling":
				// p2pquic signaling (peer registration, candidate exchange)
				log.Printf("Peer connected via unn-signaling subsystem")
				s.handleSignaling(channel, conn)
				return

			default:
				log.Printf("Unknown subsystem requested: %s", subsystem)
				channel.Close()
				return
			}

		case "pty-req":
			// Interactive person session with TUI
			var initialW, initialH uint32 = 80, 24
			if w, h, ok := common.ParsePtyRequest(req.Payload); ok {
				initialW, initialH = w, h
			}
			req.Reply(true, nil)

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
						common.SendOSC(old.Bus, "popup", map[string]interface{}{
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

		default:
			req.Reply(false, nil)
		}
	}
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

func (s *Server) loadBanner() {
	data, err := os.ReadFile("banner.asc")
	if err != nil {
		log.Printf("No banner.asc file found")
		return
	}
	s.banner = strings.Split(string(data), "\n")
}
