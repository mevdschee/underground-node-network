package sshserver

import (
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
	"github.com/google/uuid"
	"github.com/mevdschee/underground-node-network/internal/doors"
	"github.com/mevdschee/underground-node-network/internal/fileserver"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"golang.org/x/crypto/ssh"
)

// Person represents a connected person
type Person struct {
	Username        string
	Conn            ssh.Conn
	ChatUI          *ui.ChatUI
	Bus             *ui.SSHBus
	Bridge          *ui.InputBridge
	PendingDownload string
	PubKey          ssh.PublicKey // The specific key used for auth
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
	for name := range s.people {
		names = append(names, name)
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
	for name, person := range s.people {
		displayName := name
		if s.isOperator(person.PubKey) {
			displayName = "@" + name
		}
		names = append(names, displayName)
	}
	s.mu.RUnlock()
	p.ChatUI.SetPeople(names)
	p.ChatUI.SetDoors(s.doorManager.List())
}

// Broadcast sends a message to all connected people and stores it in their histories
func (s *Server) Broadcast(sender, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	chatMsg := fmt.Sprintf("<%s> %s", sender, message)

	for _, p := range s.people {
		msgType := ui.MsgChat
		if p.Username == sender {
			msgType = ui.MsgSelf
		}

		// Add to UI if available
		if p.ChatUI != nil {
			p.ChatUI.AddMessage(chatMsg, msgType)
		}

		// Add to history (Security: only because they are connected now)
		pubHash := s.getPubKeyHash(p.PubKey)
		s.addMessageToHistory(pubHash, ui.Message{Text: chatMsg, Type: msgType})
	}
}

func (s *Server) broadcastWithHistory(senderPubKey ssh.PublicKey, chatMsg string, msgType ui.MessageType) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.people {
		actualType := msgType
		if msgType == ui.MsgChat && p.PubKey != nil && senderPubKey != nil && string(p.PubKey.Marshal()) == string(senderPubKey.Marshal()) {
			actualType = ui.MsgSelf
		}

		if p.ChatUI != nil {
			p.ChatUI.AddMessage(chatMsg, actualType)
		}
		pubHash := s.getPubKeyHash(p.PubKey)
		s.addMessageToHistory(pubHash, ui.Message{Text: chatMsg, Type: actualType})
	}
}

func (s *Server) isOperator(pubKey ssh.PublicKey) bool {
	if pubKey == nil || s.operatorPubKey == nil {
		return false
	}
	return string(pubKey.Marshal()) == string(s.operatorPubKey.Marshal())
}

func (s *Server) getPubKeyHash(pubKey ssh.PublicKey) string {
	if pubKey == nil {
		return "anonymous"
	}
	hash := sha256.Sum256(pubKey.Marshal())
	return fmt.Sprintf("%x", hash)
}

func (s *Server) addMessageToHistory(pubHash string, msg ui.Message) {
	history := s.histories[pubHash]
	history = append(history, msg)
	if len(history) > 200 {
		history = history[1:]
	}
	s.histories[pubHash] = history
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

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, s.config)
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

	// Register person
	s.mu.Lock()
	p := &Person{
		Username: username,
		Conn:     sshConn,
		PubKey:   pubKey,
	}
	s.people[username] = p
	s.mu.Unlock()
	s.updateAllPeople()

	defer func() {
		s.mu.Lock()
		delete(s.people, username)
		s.mu.Unlock()
		log.Printf("Person disconnected: %s", username)
		s.updateAllPeople()
	}()

	// Discard global requests
	go ssh.DiscardRequests(reqs)

	// Handle channels
	for newChannel := range chans {
		go s.handleChannel(newChannel, username)
	}
}

func (s *Server) handleChannel(newChannel ssh.NewChannel, username string) {
	channelType := newChannel.ChannelType()

	switch channelType {
	case "session":
		s.handleSession(newChannel, username)
	case "unn-room":
		s.handleRoomSubsystem(newChannel, username)
	case "direct-tcpip":
		s.handleDirectTcpip(newChannel)
	default:
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", channelType))
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

func (s *Server) handleSession(newChannel ssh.NewChannel, username string) {
	rawChannel, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("Could not accept session: %v", err)
		return
	}
	defer rawChannel.Close()

	var p *Person
	s.mu.RLock()
	p = s.people[username]
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
			if w, h, ok := ui.ParsePtyRequest(req.Payload); ok {
				initialW, initialH = w, h
			}
			req.Reply(true, nil)
		case "shell":
			req.Reply(true, nil)

			// Shell session - init TUI and start interaction
			p.Bridge = ui.NewInputBridge(rawChannel)
			p.Bus = ui.NewSSHBus(p.Bridge, int(initialW), int(initialH))

			// Handle remaining requests in background (e.g., resize)
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
					default:
						r.Reply(false, nil)
					}
				}
			}()

			// Main interaction loop
			s.handleInteraction(rawChannel, username)

			// Ensure channel close after person done
			p.Bus.ForceClose()
			return
		case "subsystem":
			subsystem := string(req.Payload[4:])
			if subsystem == "sftp" {
				req.Reply(false, nil)
				return
			}
			req.Reply(false, nil)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (s *Server) handleRoomSubsystem(newChannel ssh.NewChannel, username string) {
	channel, _, err := newChannel.Accept()
	if err != nil {
		log.Printf("Could not accept room subsystem: %v", err)
		return
	}
	defer channel.Close()

	// Room subsystem for control channel
	fmt.Fprintf(channel, "UNN Room Control Channel\n")
}

func (s *Server) handleInteraction(channel ssh.Channel, username string) {
	var p *Person
	s.mu.RLock()
	p = s.people[username]
	s.mu.RUnlock()

	if p == nil {
		return
	}

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
				pwdUI := ui.NewPasswordUI(scr)
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

	chatUI.OnSend(func(msg string) {
		s.Broadcast(username, msg)
	})

	chatUI.OnClose(func() {
		p.Bus.SignalExit()
	})

	chatUI.OnCmd(func(cmd string) bool {
		return s.handleInternalCommand(p, cmd)
	})

	// REPLAY HISTORY
	s.mu.Lock()
	pubHash := s.getPubKeyHash(p.PubKey)
	history := s.histories[pubHash]
	s.mu.Unlock()

	if len(history) > 0 {
		for _, m := range history {
			chatUI.AddMessage(m.Text, m.Type)
		}
	} else {
		// New session welcome message
		bannerPath := s.roomName + ".asc"
		if b, err := os.ReadFile(bannerPath); err == nil {
			lines := strings.Split(string(b), "\n")
			for _, line := range lines {
				chatUI.AddMessage(strings.TrimRight(line, "\r\n"), ui.MsgServer)
			}
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

func (s *Server) handleCommand(channel ssh.Channel, username string, input string) chan struct{} {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	if !strings.HasPrefix(input, "/") {
		// Regular chat message
		s.Broadcast(username, input)
		return nil
	}

	cmd := strings.TrimPrefix(input, "/")
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}
	command := parts[0]

	if command == "get" || command == "download" {
		if len(parts) < 2 {
			fmt.Fprint(channel, "\rUsage: /get <filename>\r\n")
			return nil
		}
		fname := strings.TrimSpace(parts[1])
		s.mu.RLock()
		p := s.people[username]
		s.mu.RUnlock()
		if p != nil {
			p.PendingDownload = fname
		}
		return nil
	}

	if command == "open" {
		if len(parts) < 2 {
			fmt.Fprint(channel, "\rUsage: /open <door>\r\n")
			return nil
		}
		doorName := parts[1]
		// Try to execute as a door
		if _, ok := s.doorManager.Get(doorName); ok {
			fmt.Fprintf(channel, "\r[Opening door: %s]\r\n", doorName)
			done := make(chan struct{})
			go func() {
				// Get current person to access bridge
				s.mu.RLock()
				p := s.people[username]
				s.mu.RUnlock()

				var input io.Reader = channel
				if p != nil && p.Bus != nil {
					input = p.Bus
				}

				if err := s.doorManager.Execute(doorName, input, channel, channel); err != nil {
					fmt.Fprintf(channel, "\r[Door error: %v]\r\n", err)
				}
				fmt.Fprintf(channel, "\r[Closed door: %s]\r\n", doorName)
				close(done)
			}()
			return done
		} else {
			fmt.Fprintf(channel, "\rDoor not found: %s\r\n", doorName)
		}
		return nil
	}

	fmt.Fprintf(channel, "\rUnknown command: %s\r\n", command)
	return nil
}

func (s *Server) handleInternalCommand(p *Person, cmd string) bool {
	if strings.HasPrefix(cmd, "/") {
		log.Printf("Internal command from %s: %s", p.Username, cmd)
		// Echo the command in the chat history
		parts := strings.SplitN(strings.TrimPrefix(cmd, "/"), " ", 2)
		command := parts[0]
		pubHash := s.getPubKeyHash(p.PubKey)

		addMessage := func(text string, msgType ui.MessageType) {
			p.ChatUI.AddMessage(text, msgType)
			s.mu.Lock()
			s.addMessageToHistory(pubHash, ui.Message{Text: text, Type: msgType})
			s.mu.Unlock()
		}

		switch command {
		case "help":
			addMessage(cmd, ui.MsgCommand)
			addMessage("--- Available Commands ---", ui.MsgServer)
			addMessage("/help         - Show this help", ui.MsgServer)
			addMessage("/people       - List people in room", ui.MsgServer)
			addMessage("/doors        - List available doors", ui.MsgServer)
			addMessage("/files        - List available files", ui.MsgServer)
			addMessage("/get <file>   - Download a file", ui.MsgServer)
			addMessage("/clear        - Clear your chat history", ui.MsgServer)
			addMessage("/open <door>  - Open a door (launch program)", ui.MsgServer)
			addMessage("Ctrl+C        - Exit room", ui.MsgServer)

			if s.isOperator(p.PubKey) {
				addMessage("--- Operator Commands ---", ui.MsgServer)
				addMessage("/kick <person> [reason]    - Kick a person", ui.MsgServer)
				addMessage("/kickban <person> [reason] - Kick and ban a person", ui.MsgServer)
				addMessage("/unban <person>            - Unban a person", ui.MsgServer)
				addMessage("/banlist                   - List banned people", ui.MsgServer)
				addMessage("/lock <key>                - Lock the room", ui.MsgServer)
				addMessage("/unlock                    - Unlock the room", ui.MsgServer)
				addMessage("/kickall [reason]          - Kick everyone", ui.MsgServer)
			}
			return true
		case "people":
			addMessage(cmd, ui.MsgCommand)
			s.mu.RLock()
			people := make([]string, 0, len(s.people))
			for _, person := range s.people {
				prefix := ""
				if s.operatorPubKey != nil && string(person.PubKey.Marshal()) == string(s.operatorPubKey.Marshal()) {
					prefix = "@"
				}
				hash := s.getPubKeyHash(person.PubKey)
				if len(hash) > 8 {
					hash = hash[:8]
				}
				people = append(people, fmt.Sprintf("%s%s (%s)", prefix, person.Username, hash))
			}
			s.mu.RUnlock()
			addMessage("--- People in room ---", ui.MsgServer)
			for _, personStr := range people {
				addMessage("• "+personStr, ui.MsgServer)
			}
			return true
		case "me":
			if len(parts) < 2 {
				addMessage(cmd, ui.MsgCommand)
				addMessage("Usage: /me <action>", ui.MsgServer)
				return true
			}
			action := strings.TrimSpace(parts[1])
			chatMsg := fmt.Sprintf("* %s %s", p.Username, action)
			s.broadcastWithHistory(p.PubKey, chatMsg, ui.MsgAction)
			return true
		case "whisper":
			if len(parts) < 2 {
				addMessage(cmd, ui.MsgCommand)
				addMessage("Usage: /whisper <user> <message>", ui.MsgServer)
				return true
			}
			msgParts := strings.SplitN(parts[1], " ", 2)
			if len(msgParts) < 2 {
				addMessage(cmd, ui.MsgCommand)
				addMessage("Usage: /whisper <user> <message>", ui.MsgServer)
				return true
			}
			targetName := strings.TrimSpace(msgParts[0])
			whisperMsg := strings.TrimSpace(msgParts[1])

			s.mu.Lock()
			var target *Person
			for _, person := range s.people {
				if person.Username == targetName {
					target = person
					break
				}
			}
			s.mu.Unlock()

			if target == nil {
				addMessage(cmd, ui.MsgCommand)
				addMessage(fmt.Sprintf("User '%s' not found.", targetName), ui.MsgServer)
				return true
			}

			// Add to sender
			addMessage(fmt.Sprintf("-> [%s] %s", targetName, whisperMsg), ui.MsgWhisper)
			// Add to target
			target.ChatUI.AddMessage(fmt.Sprintf("[%s] -> %s", p.Username, whisperMsg), ui.MsgWhisper)
			// Save to histories
			s.mu.Lock()
			senderHash := s.getPubKeyHash(p.PubKey)
			targetHash := s.getPubKeyHash(target.PubKey)
			s.addMessageToHistory(senderHash, ui.Message{Text: fmt.Sprintf("-> [%s] %s", targetName, whisperMsg), Type: ui.MsgWhisper})
			s.addMessageToHistory(targetHash, ui.Message{Text: fmt.Sprintf("[%s] -> %s", p.Username, whisperMsg), Type: ui.MsgWhisper})
			s.mu.Unlock()

			// Broadcast whisper event (the fact, not the content)
			bystanderMsg := fmt.Sprintf("* %s is secretly whispering with %s", p.Username, targetName)
			s.mu.Lock()
			for _, person := range s.people {
				if person.Username != p.Username && person.Username != targetName {
					person.ChatUI.AddMessage(bystanderMsg, ui.MsgSystem)
					h := s.getPubKeyHash(person.PubKey)
					s.addMessageToHistory(h, ui.Message{Text: bystanderMsg, Type: ui.MsgSystem})
				}
			}
			s.mu.Unlock()
			return true
		case "kick":
			if !s.isOperator(p.PubKey) {
				addMessage(cmd, ui.MsgCommand)
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			if len(parts) < 2 {
				addMessage(cmd, ui.MsgCommand)
				addMessage("Usage: /kick <user/hash> [reason]", ui.MsgServer)
				return true
			}
			kickParts := strings.SplitN(parts[1], " ", 2)
			targetID := strings.TrimSpace(kickParts[0])
			reason := "No reason given."
			if len(kickParts) > 1 {
				reason = strings.TrimSpace(kickParts[1])
			}

			s.mu.Lock()
			var targetPerson *Person
			for _, person := range s.people {
				h := s.getPubKeyHash(person.PubKey)
				if person.Username == targetID || strings.HasPrefix(h, targetID) {
					targetPerson = person
					break
				}
			}
			s.mu.Unlock()

			if targetPerson == nil {
				addMessage(cmd, ui.MsgCommand)
				addMessage("User not found.", ui.MsgServer)
				return true
			}

			s.Broadcast("Server", fmt.Sprintf("*** %s was kicked by @%s (%s) ***", targetPerson.Username, p.Username, reason))
			targetPerson.Conn.Close()
			return true
		case "kickban":
			if !s.isOperator(p.PubKey) {
				addMessage(cmd, ui.MsgCommand)
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			if len(parts) < 2 {
				addMessage(cmd, ui.MsgCommand)
				addMessage("Usage: /kickban <user/hash> [reason]", ui.MsgServer)
				return true
			}
			banParts := strings.SplitN(parts[1], " ", 2)
			targetID := strings.TrimSpace(banParts[0])
			reason := "No reason given."
			if len(banParts) > 1 {
				reason = strings.TrimSpace(banParts[1])
			}

			s.mu.Lock()
			var targetPerson *Person
			var targetHash string
			for _, person := range s.people {
				h := s.getPubKeyHash(person.PubKey)
				if person.Username == targetID || strings.HasPrefix(h, targetID) {
					targetPerson = person
					targetHash = h
					break
				}
			}
			if targetPerson != nil {
				s.bannedHashes[targetHash] = reason
				s.mu.Unlock()
				s.Broadcast("Server", fmt.Sprintf("*** %s was banned by @%s (%s) ***", targetPerson.Username, p.Username, reason))
				targetPerson.Conn.Close()
			} else {
				// Handle offline ban by hash
				if len(targetID) >= 8 {
					s.bannedHashes[targetID] = reason
					s.mu.Unlock()
					addMessage(fmt.Sprintf("Banned hash prefix: %s", targetID), ui.MsgServer)
				} else {
					s.mu.Unlock()
					addMessage("User not found or hash too short.", ui.MsgServer)
				}
			}
			return true
		case "unban":
			if !s.isOperator(p.PubKey) {
				addMessage(cmd, ui.MsgCommand)
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			if len(parts) < 2 {
				addMessage(cmd, ui.MsgCommand)
				addMessage("Usage: /unban <hash>", ui.MsgServer)
				return true
			}
			hash := strings.TrimSpace(parts[1])
			s.mu.Lock()
			found := false
			for h := range s.bannedHashes {
				if strings.HasPrefix(h, hash) {
					delete(s.bannedHashes, h)
					found = true
					break
				}
			}
			s.mu.Unlock()
			if found {
				addMessage(fmt.Sprintf("Unbanned hash prefix: %s", hash), ui.MsgServer)
			} else {
				addMessage("Ban not found.", ui.MsgServer)
			}
			return true
		case "banlist":
			if !s.isOperator(p.PubKey) {
				addMessage(cmd, ui.MsgCommand)
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			addMessage("--- Banned Users ---", ui.MsgServer)
			s.mu.RLock()
			for h, r := range s.bannedHashes {
				addMessage(fmt.Sprintf("%s: %s", h[:12], r), ui.MsgServer)
			}
			s.mu.RUnlock()
			return true
		case "lock":
			if !s.isOperator(p.PubKey) {
				addMessage(cmd, ui.MsgCommand)
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			if len(parts) < 2 {
				addMessage(cmd, ui.MsgCommand)
				addMessage("Usage: /lock <key>", ui.MsgServer)
				return true
			}
			key := strings.TrimSpace(parts[1])
			s.mu.Lock()
			s.roomLockKey = key
			s.mu.Unlock()
			s.Broadcast("Server", fmt.Sprintf("*** @%s locked the room ***", p.Username))
			return true
		case "unlock":
			if !s.isOperator(p.PubKey) {
				addMessage(cmd, ui.MsgCommand)
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			s.mu.Lock()
			s.roomLockKey = ""
			s.mu.Unlock()
			s.Broadcast("Server", fmt.Sprintf("*** @%s unlocked the room ***", p.Username))
			return true
		case "kickall":
			if !s.isOperator(p.PubKey) {
				addMessage(cmd, ui.MsgCommand)
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			reason := "All users kicked."
			if len(parts) > 1 {
				reason = strings.TrimSpace(parts[1])
			}
			s.Broadcast("Server", fmt.Sprintf("*** @%s is kicking everyone: %s ***", p.Username, reason))

			s.mu.Lock()
			for _, person := range s.people {
				if !s.isOperator(person.PubKey) {
					person.Conn.Close()
				}
			}
			s.mu.Unlock()
			return true
		case "get", "download":
			addMessage(cmd, ui.MsgCommand)
			if len(parts) < 2 {
				addMessage("Usage: /get <filename>", ui.MsgServer)
				return true
			}
			fname := strings.TrimSpace(parts[1])
			p.PendingDownload = filepath.Clean(fname)
			p.ChatUI.Close(true)
			return true
		case "clear":
			s.mu.Lock()
			delete(s.histories, pubHash)
			s.mu.Unlock()
			p.ChatUI.ClearMessages()
			return true
		case "doors":
			addMessage(cmd, ui.MsgCommand)
			doorList := s.doorManager.List()
			addMessage("--- Available doors ---", ui.MsgServer)
			for _, door := range doorList {
				addMessage("• "+door, ui.MsgServer)
			}
			addMessage("Type /open <door> to launch a program.", ui.MsgServer)
			return true
		case "files":
			addMessage(cmd, ui.MsgCommand)
			s.showFiles(p.ChatUI)
			return true
		case "open":
			addMessage(cmd, ui.MsgCommand)
			if len(parts) < 2 {
				addMessage("Usage: /open <door>", ui.MsgServer)
				return true
			}
			doorName := strings.TrimSpace(parts[1])
			if _, ok := s.doorManager.Get(doorName); !ok {
				addMessage(fmt.Sprintf("Door not found: %s", doorName), ui.MsgServer)
				return true
			}
			// Door exists, return false to exit TUI and execute it in handleCommand
			return false
		default:
		}
	}
	return false // Not handled internally, exit Run() to check if it's a door
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

func (s *Server) showFiles(chatUI *ui.ChatUI) {
	s.mu.RLock()
	// Identify current person to log to history
	var pubHash string
	for _, p := range s.people {
		if p.ChatUI == chatUI {
			pubHash = s.getPubKeyHash(p.PubKey)
			break
		}
	}
	s.mu.RUnlock()

	addMessage := func(text string, msgType ui.MessageType) {
		chatUI.AddMessage(text, msgType)
		if pubHash != "" {
			s.mu.Lock()
			s.addMessageToHistory(pubHash, ui.Message{Text: text, Type: msgType})
			s.mu.Unlock()
		}
	}

	addMessage("--- Available Files ---", ui.MsgServer)
	found := false

	err := filepath.WalkDir(s.filesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		var rel string
		rel, err = filepath.Rel(s.filesDir, path)
		if err != nil {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		found = true
		size := formatSize(info.Size())
		modTime := info.ModTime().Format("2006-01-02 15:04")
		addMessage(fmt.Sprintf(" %-24s %10s  %s", rel, size, modTime), ui.MsgServer)
		return nil
	})

	if err != nil {
		addMessage(fmt.Sprintf("\033[1;31mError listing files: %v\033[0m", err), ui.MsgServer)
		return
	}

	if !found {
		addMessage("No files available.", ui.MsgServer)
	}
	addMessage("-----------------------", ui.MsgServer)
}

func (s *Server) showDownloadInfo(p *Person, filename string) {
	if filename == "" {
		return
	}

	// Sanitize and prevent path traversal
	cleanTarget := filepath.Clean(filename)
	path := filepath.Join(s.filesDir, cleanTarget)

	// Ensure the path is within s.filesDir
	absBase, _ := filepath.Abs(s.filesDir)
	absPath, _ := filepath.Abs(path)
	if !strings.HasPrefix(absPath, absBase) {
		fmt.Fprintf(p.Bus, "\033[1;31mAccess denied: %s\033[0m\r\n", filename)
		return
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(p.Bus, "\033[1;31mFile not found: %s\033[0m\r\n", filename)
		return
	}

	filename = cleanTarget

	transferID := uuid.New().String()

	// Start one-shot server
	s.mu.RLock()
	dt := s.downloadTimeout
	s.mu.RUnlock()

	filePort, err := fileserver.StartOneShot(fileserver.Options{
		HostKey:     s.hostKey,
		ClientKey:   p.PubKey,
		Filename:    filename,
		BaseDir:     s.filesDir,
		TransferID:  transferID,
		Timeout:     dt,
		UploadLimit: s.uploadLimit,
	})
	if err != nil {
		fmt.Fprintf(p.Bus, "\033[1;31mFailed to start download server: %v\033[0m\r\n", err)
		return
	}

	// Always clear screen before showing download info
	// First reset colors to avoid the black background from sticking around
	fmt.Fprint(p.Bus, "\033[m\033[2J\033[H")

	// Calculate file signature early to include it in the wrapper's download block
	sig := s.calculateFileSHA256(absPath)

	data := protocol.DownloadPayload{
		Filename:   filename,
		Port:       filePort,
		TransferID: transferID,
		Signature:  sig,
	}

	// Emit invisible ANSI OSC 9 sequence with download data
	data.Action = "download"
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(p.Bus, "\033]9;%s\007", string(jsonData))

	fmt.Fprintf(p.Bus, "\033[1;32mUNN DOWNLOAD READY\033[0m\r\n\r\n")
	fmt.Fprintf(p.Bus, "The wrapper is automatically downloading the file to your Downloads folder.\r\n")

	fmt.Fprintf(p.Bus, "\033[1;33mNote: The transfer must start within %d seconds.\033[0m\r\n", int(dt.Seconds()))

	fmt.Fprintf(p.Bus, "If the wrapper fails, you can download manually using:\r\n\r\n")

	// Get actual address for manual instruction
	host, _, _ := net.SplitHostPort(s.address)
	if host == "" || host == "0.0.0.0" || host == "127.0.0.1" || host == "::" {
		host = "localhost"
	}

	fmt.Fprintf(p.Bus, "  \033[1;36mscp -P %d %s:%s ~/Downloads/%s\033[0m\r\n\r\n", filePort, host, transferID, filepath.Base(filename))

	// Display the host key fingerprint so the user can verify it
	fingerprint := s.calculateHostKeyFingerprint()
	fmt.Fprintf(p.Bus, "\033[1mHost Verification Fingerprint (tunnel):\033[0m\r\n")
	fmt.Fprintf(p.Bus, "\033[1;36m%s\033[0m\r\n\r\n", fingerprint)

	// Display the file signature (matching entrypoint style)
	fmt.Fprintf(p.Bus, "\033[1mFile Verification Signature:\033[0m\r\n")
	fmt.Fprintf(p.Bus, "\033[1;36m%s  %s\033[0m\r\n\r\n", sig, filename)

	fmt.Fprintf(p.Bus, "Disconnecting to allow the transfer...\r\n")

	// Consume the data
	p.PendingDownload = ""

	// Give the wrapper documentation and time to start the download.
	// The connection will stay open until the person disconnects.
}

func (s *Server) calculateFileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "error"
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "error"
	}
	hash := h.Sum(nil)
	fingerprint := fmt.Sprintf("%x", hash)
	return fingerprint
}

func (s *Server) calculateHostKeyFingerprint() string {
	pubKey := s.hostKey.PublicKey()
	algo := strings.ToUpper(strings.TrimPrefix(pubKey.Type(), "ssh-"))
	hash := sha256.Sum256(pubKey.Marshal())
	fingerprint := "SHA256:" + base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
	return fmt.Sprintf("%s key fingerprint is %s.", algo, fingerprint)
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
