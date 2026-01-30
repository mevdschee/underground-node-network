package sshserver

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/doors"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"github.com/mevdschee/underground-node-network/internal/ui/bridge"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"github.com/mevdschee/underground-node-network/internal/ui/password"
	"golang.org/x/crypto/ssh"
)

// Person represents a connected person
type Person struct {
	SessionID       string
	Username        string
	Conn            ssh.Conn
	ChatUI          *ui.ChatUI
	Bus             *bridge.SSHBus
	Bridge          *bridge.InputBridge
	PendingDownload string
	PubKey          ssh.PublicKey // The specific key used for auth
	UNNAware        bool
}

type Server struct {
	address         string
	config          *ssh.ServerConfig
	doorManager     *doors.Manager
	roomName        string
	people          map[string]*Person
	authorizedKeys  map[string]string // Marshaled pubkey -> verified username
	filesDir        string
	hostKey         ssh.Signer
	downloadTimeout time.Duration
	mu              sync.RWMutex
	listener        net.Listener
	headless        bool
	uploadLimit     int64                   // bytes per second
	histories       map[string][]ui.Message // keyed by pubkey hash (hex)
	cmdHistories    map[string][]string     // keyed by pubkey hash (hex)
	bannedHashes    map[string]string       // hash -> reason
	roomLockKey     string
	operatorPubKey  ssh.PublicKey
}

func NewServer(address, hostKeyPath, roomName, filesDir string, doorManager *doors.Manager) (*Server, error) {
	s := &Server{
		address:         address,
		doorManager:     doorManager,
		roomName:        roomName,
		filesDir:        filesDir,
		people:          make(map[string]*Person),
		authorizedKeys:  make(map[string]string),
		histories:       make(map[string][]ui.Message),
		cmdHistories:    make(map[string][]string),
		bannedHashes:    make(map[string]string),
		downloadTimeout: 60 * time.Second,
	}

	config := &ssh.ServerConfig{
		NoClientAuth: false,
	}

	config.PublicKeyCallback = func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()

		marshaled := pubKey.Marshal()
		if _, ok := s.authorizedKeys[string(marshaled)]; !ok {
			return nil, fmt.Errorf("public key not authorized for this room")
		}

		return &ssh.Permissions{
			Extensions: map[string]string{
				"pubkey": base64.StdEncoding.EncodeToString(marshaled),
			},
		}, nil
	}

	// Load or generate host key
	hostKey, err := loadOrGenerateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load host key: %w", err)
	}
	config.AddHostKey(hostKey)
	s.hostKey = hostKey

	s.config = config
	return s, nil
}

func (s *Server) SetHeadless(headless bool) {
	s.headless = headless
}

func (s *Server) SetUploadLimit(limit int64) {
	s.mu.Lock()
	s.uploadLimit = limit
	s.mu.Unlock()
}

func (s *Server) AuthorizeKey(pubKey ssh.PublicKey, username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorizedKeys[string(pubKey.Marshal())] = username
	log.Printf("Authorized key for person: %s", username)
}

// SetDownloadTimeout sets the timeout for one-shot SFTP download servers
func (s *Server) SetDownloadTimeout(timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.downloadTimeout = timeout
}

// Start begins listening for SSH connections
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.address, err)
	}
	s.listener = listener

	// Get actual address (important when port 0 is used for random port)
	actualAddr := listener.Addr().String()
	log.Printf("SSH server listening on %s", actualAddr)

	go s.acceptLoop()
	return nil
}

// GetPort returns the actual port the server is listening on
func (s *Server) GetPort() int {
	if s.listener == nil {
		return 0
	}
	addr := s.listener.Addr().(*net.TCPAddr)
	return addr.Port
}

// Stop stops the SSH server
func (s *Server) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// GetPeople returns a list of current people
func (s *Server) GetPeople() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.people))
	for _, p := range s.people {
		names = append(names, p.Username)
	}
	return names
}

func (s *Server) updateAllPeople() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.people {
		s.updatePeopleList(p)
	}
}

func (s *Server) updatePeopleList(p *Person) {
	if p.ChatUI == nil {
		return
	}
	s.mu.RLock()
	names := make([]string, 0, len(s.people))
	for _, person := range s.people {
		displayName := person.Username
		if s.isOperator(person.PubKey) {
			displayName = "@" + person.Username
		}
		names = append(names, displayName)
	}
	s.mu.RUnlock()
	p.ChatUI.SetPeople(names)
	p.ChatUI.SetDoors(s.doorManager.List())
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
	// handeConnection

	sshConn, chans, _, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		if err != io.EOF {
			log.Printf("Failed SSH handshake: %v", err)
		}
		return
	}

	var pubKey ssh.PublicKey
	if b64, ok := sshConn.Permissions.Extensions["pubkey"]; ok {
		marshaled, _ := base64.StdEncoding.DecodeString(b64)
		pubKey, _ = ssh.ParsePublicKey(marshaled)
	}

	// Check for bans
	pubHash := s.getPubKeyHash(pubKey)
	s.mu.RLock()
	reason, banned := s.bannedHashes[pubHash]
	if !banned {
		for h, r := range s.bannedHashes {
			if strings.HasPrefix(pubHash, h) {
				banned = true
				reason = r
				break
			}
		}
	}
	s.mu.RUnlock()

	if banned {
		fmt.Fprintf(conn, "\r\n*** YOU ARE BANNED FROM THIS ROOM ***\r\n*** Reason: %s ***\r\n\r\n", reason)
		sshConn.Close()
		return
	}

	// First connection becomes operator if none exists
	s.mu.Lock()
	if s.operatorPubKey == nil && pubKey != nil {
		s.operatorPubKey = pubKey
		log.Printf("First person %s identified as operator", sshConn.User())
	}
	s.mu.Unlock()

	defer sshConn.Close()

	username := sshConn.User()
	s.mu.RLock()
	if mappedName, ok := s.authorizedKeys[string(pubKey.Marshal())]; ok && mappedName != "" {
		username = mappedName
	}
	s.mu.RUnlock()
	log.Printf("Person connected: %s", username)

	sessionID := fmt.Sprintf("%s-%d", username, time.Now().UnixNano())

	s.mu.Lock()
	// Disconnect old session with same key
	if pubKey != nil {
		pubKeyBytes := pubKey.Marshal()
		for oldID, old := range s.people {
			if old.PubKey != nil && bytes.Equal(old.PubKey.Marshal(), pubKeyBytes) {
				log.Printf("Disconnecting old session %s for user %s (new connection with same key)", oldID, old.Username)
				s.SendOSC(old, "popup", map[string]interface{}{
					"title":   "Duplicate Session",
					"message": "You have been disconnected because you connected from another session.",
					"type":    "warning",
				})
				// Give it a moment to send
				oldConn := old.Conn
				go func() {
					time.Sleep(200 * time.Millisecond)
					oldConn.Close()
				}()
			}
		}
	}

	p := &Person{
		SessionID: sessionID,
		Username:  username,
		Conn:      sshConn,
		PubKey:    pubKey,
		UNNAware:  strings.Contains(string(sshConn.ClientVersion()), "UNN"),
	}
	s.people[sessionID] = p
	s.mu.Unlock()
	s.updateAllPeople()

	defer func() {
		s.mu.Lock()
		if current, ok := s.people[sessionID]; ok && current == p {
			delete(s.people, sessionID)
		}
		s.mu.Unlock()
		log.Printf("Person disconnected: %s", username)
		s.updateAllPeople()
	}()

	// Accept channels and requests - these normally go into handleChannel
	for newChannel := range chans {
		go s.handleChannel(newChannel, sessionID)
	}
}

func (s *Server) handleChannel(newChannel ssh.NewChannel, sessionID string) {
	s.mu.RLock()
	p := s.people[sessionID]
	s.mu.RUnlock()
	if p == nil {
		newChannel.Reject(ssh.Prohibited, "session not found")
		return
	}

	switch newChannel.ChannelType() {
	case "session":
		go s.handleSession(newChannel, sessionID)
	case "direct-tcpip":
		go s.handleDirectTcpip(newChannel)
	default:
		newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
	}
}

func (s *Server) handleDirectTcpip(newChannel ssh.NewChannel) {
	type directTcpipData struct {
		DestAddr   string
		DestPort   uint32
		OriginAddr string
		OriginPort uint32
	}

	var data directTcpipData
	if err := ssh.Unmarshal(newChannel.ExtraData(), &data); err != nil {
		newChannel.Reject(ssh.ConnectionFailed, "error parsing direct-tcpip data")
		return
	}

	if data.DestAddr != "127.0.0.1" && data.DestAddr != "localhost" {
		newChannel.Reject(ssh.Prohibited, "only localhost forwarding allowed")
		return
	}

	dest := fmt.Sprintf("%s:%d", data.DestAddr, data.DestPort)
	conn, err := net.Dial("tcp", dest)
	if err != nil {
		newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	channel, requests, err := newChannel.Accept()
	if err != nil {
		conn.Close()
		return
	}
	go ssh.DiscardRequests(requests)

	go func() {
		defer channel.Close()
		defer conn.Close()
		io.Copy(channel, conn)
	}()
	go func() {
		defer channel.Close()
		defer conn.Close()
		io.Copy(conn, channel)
	}()
}

func (s *Server) handleSession(newChannel ssh.NewChannel, sessionID string) {
	rawChannel, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("Could not accept session: %v", err)
		return
	}
	defer rawChannel.Close()

	var p *Person
	s.mu.RLock()
	p = s.people[sessionID]
	s.mu.RUnlock()

	if p == nil {
		return
	}

	var initialW, initialH uint32 = 80, 24

	// Process requests to determine session type (shell vs exec scp)
	for req := range requests {
		switch req.Type {
		case "exec":
			req.Reply(false, nil)
			return
		case "pty-req":
			if w, h, ok := common.ParsePtyRequest(req.Payload); ok {
				initialW, initialH = w, h
			}
			req.Reply(true, nil)
		case "shell":
			req.Reply(true, nil)

			// Shell session - init TUI and start interaction
			p.Bridge = bridge.NewInputBridge(rawChannel)
			p.Bridge.SetOSCHandler(func(action string, params map[string]interface{}) {
				s.HandleOSC(p, action, params)
			})
			p.Bus = bridge.NewSSHBus(p.Bridge, int(initialW), int(initialH))

			// Handle remaining requests in background (e.g., resize)
			go func() {
				for r := range requests {
					switch r.Type {
					case "pty-req":
						if w, h, ok := common.ParsePtyRequest(r.Payload); ok {
							p.Bus.Resize(int(w), int(h))
						}
						r.Reply(true, nil)
					case "window-change":
						if w, h, ok := common.ParseWindowChange(
							r.Payload); ok {
							p.Bus.Resize(int(w), int(h))
						}
						r.Reply(true, nil)
					default:
						r.Reply(false, nil)
					}
				}
			}()

			// Main interaction loop
			s.handleInteraction(rawChannel, sessionID)

			// Ensure channel close after person done
			p.Bus.ForceClose()
			return
		case "subsystem":
			subsystem := string(req.Payload[4:])
			if subsystem == "sftp" {
				req.Reply(false, nil)
				return
			}
			if subsystem == "unn-room" {
				req.Reply(true, nil)
				s.handleRoomSubsystem(rawChannel, sessionID)
				return
			}
			req.Reply(false, nil)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (s *Server) handleInteraction(channel ssh.Channel, sessionID string) {
	s.mu.RLock()
	p := s.people[sessionID]
	s.mu.RUnlock()
	if p == nil {
		return
	}
	username := p.Username

	// Handle room lock
	s.mu.RLock()
	lockKey := s.roomLockKey
	isOp := s.isOperator(p.PubKey)
	s.mu.RUnlock()

	if lockKey != "" && !isOp && !s.headless {
		// Use a local screen variable to avoid conflict with long-running ChatUI screen
		scr, err := tcell.NewTerminfoScreenFromTty(p.Bus)
		if err == nil {
			if err := scr.Init(); err == nil {
				pwdUI := password.NewPasswordUI(
					scr)
				entered := pwdUI.Run()
				scr.Fini()
				if entered != lockKey {
					fmt.Fprintf(channel, "\r\n*** INCORRECT ROOM KEY ***\r\n\r\n")
					p.Conn.Close()
					return
				}
			}
		}
	}

	chatUI := ui.NewChatUI(nil) // Screen will be set in loop
	chatUI.SetUsername(username)
	chatUI.SetTitle(fmt.Sprintf("Underground Node Network - Room: %s", s.roomName))
	chatUI.Headless = s.headless
	chatUI.Input = p.Bus
	p.ChatUI = chatUI

	pubHash := s.getPubKeyHash(p.PubKey)

	chatUI.OnSend(func(msg string) {
		s.addCommandToHistory(pubHash, msg)
		s.Broadcast(username, msg)
	})

	chatUI.OnClose(func() {
		p.Bus.SignalExit()
	})

	chatUI.OnCmd(func(cmd string) bool {
		s.addCommandToHistory(pubHash, cmd)
		return s.handleInternalCommand(p, cmd)
	})

	// REPLAY HISTORY
	s.mu.Lock()
	history := s.histories[pubHash]
	cmdHistory := s.cmdHistories[pubHash]
	s.mu.Unlock()

	if len(cmdHistory) > 0 {
		chatUI.SetCommandHistory(cmdHistory)
	}

	if len(history) > 0 {
		for _, m := range history {
			chatUI.AddMessage(m.Text, m.Type)
		}
	} else {
		// New session welcome message
		bannerPath := "room.asc"
		if b, err := os.ReadFile(bannerPath); err == nil {
			lines := strings.Split(string(b), "\n")
			s.mu.Lock()
			for _, line := range lines {
				text := strings.TrimRight(line, "\r\n")
				chatUI.AddMessage(text, ui.MsgServer)
				s.addMessageToHistory(pubHash, ui.Message{Text: text, Type: ui.MsgServer})
			}
			s.mu.Unlock()
		} else {
			chatUI.AddMessage(fmt.Sprintf("*** You joined %s as %s ***", s.roomName, username), ui.MsgSystem)
			chatUI.AddMessage("*** Type /help for commands ***", ui.MsgSystem)
		}
	}

	for {
		// Reset bus and UI for each TUI run
		p.Bus.Reset()
		chatUI.Reset()

		// Create a fresh screen for each run to avoid "already engaged" errors
		if !s.headless {
			screen, err := tcell.NewTerminfoScreenFromTty(p.Bus)
			if err != nil {
				log.Printf("Failed to create screen: %v", err)
				return
			}

			if err := screen.Init(); err != nil {
				log.Printf("Failed to init screen: %v", err)
				return
			}
			chatUI.SetScreen(screen)
		}

		// Update visitors list
		s.updatePeopleList(p)

		cmd := chatUI.Run()

		// Explicitly finalize screen immediately after Run() to restore terminal state
		s.mu.RLock()
		if !s.headless && chatUI.GetScreen() != nil {
			chatUI.GetScreen().Fini()
		}
		s.mu.RUnlock()

		if cmd == "" && p.PendingDownload == "" {
			p.Conn.Close() // Force immediate disconnect
			return         // User exited
		}

		// Prepare bus for potential door command (since chatUI.Run signaled it to exit)
		p.Bus.Reset()

		// Handle external "door" command (anything that returned from Run)
		done := s.handleCommand(channel, username, cmd)
		if done != nil {
			// Wait for door to finish
			<-done
			// Force the stdin goroutine to exit
			p.Bus.SignalExit()
		}

		// If a download was requested, exit the loop to show info in plain terminal
		if p.PendingDownload != "" {
			break
		}
	}

	// Post-TUI exit download info (mirroring teleport flow)
	if p.PendingDownload != "" {
		s.showDownloadInfo(p, p.PendingDownload)
	} else {
		// Clear screen on manual exit - first reset colors to avoid black background spill
		fmt.Fprint(p.Bus, "\033[m\033[2J\033[H")
	}
}

func loadOrGenerateHostKey(path string) (ssh.Signer, error) {
	// Try to load existing key
	keyBytes, err := os.ReadFile(path)
	if err == nil {
		return ssh.ParsePrivateKey(keyBytes)
	}

	// Generate new key
	log.Printf("Generating new host key at %s", path)
	return generateHostKey(path)
}

func generateHostKey(path string) (ssh.Signer, error) {
	// Use ssh-keygen to generate a proper OpenSSH format key
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", path, "-N", "", "-q")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ssh-keygen failed: %w", err)
	}

	// Read the generated key
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return ssh.ParsePrivateKey(keyBytes)
}
