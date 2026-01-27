package sshserver

import (
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/mevdschee/underground-node-network/internal/ui"
	"golang.org/x/crypto/ssh"
)

// Visitor represents a connected visitor
type Visitor struct {
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
	visitors        map[string]*Visitor
	authorizedKeys  map[string]bool // Marshaled pubkey -> true
	filesDir        string
	hostKey         ssh.Signer
	downloadTimeout time.Duration
	mu              sync.RWMutex
	listener        net.Listener
	headless        bool
	uploadLimit     int64                   // bytes per second
	histories       map[string][]ui.Message // keyed by pubkey hash (hex)
}

func NewServer(address, hostKeyPath, roomName, filesDir string, doorManager *doors.Manager) (*Server, error) {
	s := &Server{
		address:         address,
		doorManager:     doorManager,
		roomName:        roomName,
		filesDir:        filesDir,
		visitors:        make(map[string]*Visitor),
		authorizedKeys:  make(map[string]bool),
		histories:       make(map[string][]ui.Message),
		downloadTimeout: 60 * time.Second,
	}

	config := &ssh.ServerConfig{
		NoClientAuth: false,
	}

	config.PublicKeyCallback = func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()

		marshaled := pubKey.Marshal()
		if !s.authorizedKeys[string(marshaled)] {
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

func (s *Server) AuthorizeKey(pubKey ssh.PublicKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorizedKeys[string(pubKey.Marshal())] = true
	log.Printf("Authorized key for visitor")
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

// GetVisitors returns a list of current visitors
func (s *Server) GetVisitors() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.visitors))
	for name := range s.visitors {
		names = append(names, name)
	}
	return names
}

func (s *Server) updateAllVisitors() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.visitors {
		s.updateVisitorList(v)
	}
}

func (s *Server) updateVisitorList(v *Visitor) {
	if v.ChatUI == nil {
		return
	}
	s.mu.RLock()
	names := make([]string, 0, len(s.visitors))
	for name := range s.visitors {
		names = append(names, name)
	}
	s.mu.RUnlock()
	v.ChatUI.SetVisitors(names)
	v.ChatUI.SetDoors(s.doorManager.List())
}

// Broadcast sends a message to all connected visitors and stores it in their histories
func (s *Server) Broadcast(sender, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	chatMsg := fmt.Sprintf("<%s> %s", sender, message)

	for _, v := range s.visitors {
		msgType := ui.MsgChat
		if v.Username == sender {
			msgType = ui.MsgSelf
		}

		// Add to UI if available
		if v.ChatUI != nil {
			v.ChatUI.AddMessage(chatMsg, msgType)
		}

		// Add to history (Security: only because they are connected now)
		pubHash := s.getPubKeyHash(v.PubKey)
		s.addMessageToHistory(pubHash, ui.Message{Text: chatMsg, Type: msgType})
	}
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
	defer sshConn.Close()

	username := sshConn.User()
	log.Printf("Visitor connected: %s", username)

	var pubKey ssh.PublicKey
	if b64, ok := sshConn.Permissions.Extensions["pubkey"]; ok {
		marshaled, _ := base64.StdEncoding.DecodeString(b64)
		pubKey, _ = ssh.ParsePublicKey(marshaled)
	}

	// Register visitor
	s.mu.Lock()
	v := &Visitor{
		Username: username,
		Conn:     sshConn,
		PubKey:   pubKey,
	}
	s.visitors[username] = v
	s.mu.Unlock()
	s.updateAllVisitors()

	defer func() {
		s.mu.Lock()
		delete(s.visitors, username)
		s.mu.Unlock()
		log.Printf("Visitor disconnected: %s", username)
		s.updateAllVisitors()
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

	var v *Visitor
	s.mu.RLock()
	v = s.visitors[username]
	s.mu.RUnlock()

	if v == nil {
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
			v.Bridge = ui.NewInputBridge(rawChannel)
			v.Bus = ui.NewSSHBus(v.Bridge, int(initialW), int(initialH))

			// Handle remaining requests in background (e.g., resize)
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
					default:
						r.Reply(false, nil)
					}
				}
			}()

			// Main interaction loop
			s.handleInteraction(rawChannel, username)

			// Ensure channel close after visitor done
			v.Bus.ForceClose()
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
	var v *Visitor
	s.mu.RLock()
	v = s.visitors[username]
	s.mu.RUnlock()

	if v == nil {
		return
	}

	chatUI := ui.NewChatUI(nil) // Screen will be set in loop
	chatUI.SetUsername(username)
	chatUI.SetTitle(fmt.Sprintf("Underground Node Network - Room: %s", s.roomName))
	chatUI.Headless = s.headless
	chatUI.Input = v.Bus
	v.ChatUI = chatUI

	chatUI.OnSend(func(msg string) {
		s.Broadcast(username, msg)
	})

	chatUI.OnClose(func() {
		v.Bus.SignalExit()
	})

	chatUI.OnCmd(func(cmd string) bool {
		return s.handleInternalCommand(v, cmd)
	})

	// REPLAY HISTORY
	s.mu.Lock()
	pubHash := s.getPubKeyHash(v.PubKey)
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
		v.Bus.Reset()
		chatUI.Reset()

		// Create a fresh screen for each run to avoid "already engaged" errors
		if !s.headless {
			screen, err := tcell.NewTerminfoScreenFromTty(v.Bus)
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
		s.updateVisitorList(v)

		cmd := chatUI.Run()

		// Explicitly finalize screen immediately after Run() to restore terminal state
		s.mu.RLock()
		if !s.headless && chatUI.GetScreen() != nil {
			chatUI.GetScreen().Fini()
		}
		s.mu.RUnlock()

		if cmd == "" && v.PendingDownload == "" {
			v.Conn.Close() // Force immediate disconnect
			return         // User exited
		}

		// Prepare bus for potential door command (since chatUI.Run signaled it to exit)
		v.Bus.Reset()

		// Handle external "door" command (anything that returned from Run)
		done := s.handleCommand(channel, username, cmd)
		if done != nil {
			// Wait for door to finish
			<-done
			// Force the stdin goroutine to exit
			v.Bus.SignalExit()
		}

		// If a download was requested, exit the loop to show info in plain terminal
		if v.PendingDownload != "" {
			break
		}
	}

	// Post-TUI exit download info (mirroring teleport flow)
	if v.PendingDownload != "" {
		s.showDownloadInfo(v, v.PendingDownload)
	} else {
		// Clear screen on manual exit - first reset colors to avoid black background spill
		fmt.Fprint(v.Bus, "\033[m\033[2J\033[H")
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
	parts := strings.SplitN(cmd, " ", 2)
	command := parts[0]

	if command == "get" {
		if len(parts) < 2 {
			fmt.Fprint(channel, "\rUsage: /get <filename>\r\n")
			return nil
		}
		fname := strings.TrimSpace(parts[1])
		s.mu.RLock()
		v := s.visitors[username]
		s.mu.RUnlock()
		if v != nil {
			go s.showDownloadInfo(v, fname)
		}
		return nil
	}
	// Try to execute as a door
	if _, ok := s.doorManager.Get(command); ok {
		fmt.Fprintf(channel, "\r[Entering door: %s]\r\n", command)
		done := make(chan struct{})
		go func() {
			// Get current visitor to access bridge
			s.mu.RLock()
			v := s.visitors[username]
			s.mu.RUnlock()

			var input io.Reader = channel
			if v != nil && v.Bus != nil {
				input = v.Bus
			}

			if err := s.doorManager.Execute(command, input, channel, channel); err != nil {
				fmt.Fprintf(channel, "\r[Door error: %v]\r\n", err)
			}
			fmt.Fprintf(channel, "\r[Exited door: %s]\r\n", command)
			close(done)
		}()
		return done
	} else {
		fmt.Fprintf(channel, "\rUnknown command: %s\r\n", command)
	}
	return nil
}

func (s *Server) handleInternalCommand(v *Visitor, cmd string) bool {
	if strings.HasPrefix(cmd, "/") {
		log.Printf("Internal command from %s: %s", v.Username, cmd)
		// Echo the command in the chat history
		parts := strings.SplitN(strings.TrimPrefix(cmd, "/"), " ", 2)
		command := parts[0]
		pubHash := s.getPubKeyHash(v.PubKey)

		addMessage := func(text string, msgType ui.MessageType) {
			v.ChatUI.AddMessage(text, msgType)
			s.mu.Lock()
			s.addMessageToHistory(pubHash, ui.Message{Text: text, Type: msgType})
			s.mu.Unlock()
		}

		switch command {
		case "help":
			addMessage(cmd, ui.MsgCommand)
			addMessage("--- Available Commands ---", ui.MsgServer)
			addMessage("/help       - Show this help", ui.MsgServer)
			addMessage("/who        - List visitors in room", ui.MsgServer)
			addMessage("/doors      - List available doors", ui.MsgServer)
			addMessage("/files      - List available files", ui.MsgServer)
			addMessage("/get <file> - Download a file", ui.MsgServer)
			addMessage("/clear      - Clear your chat history", ui.MsgServer)
			addMessage("/<door>     - Enter a door", ui.MsgServer)
			addMessage("Ctrl+C      - Exit room", ui.MsgServer)
			return true
		case "who":
			addMessage(cmd, ui.MsgCommand)
			visitors := s.GetVisitors()
			addMessage("--- Visitors in room ---", ui.MsgServer)
			for _, name := range visitors {
				addMessage("• "+name, ui.MsgServer)
			}
			return true
		case "get":
			addMessage(cmd, ui.MsgCommand)
			if len(parts) < 2 {
				addMessage("Usage: /get <filename>", ui.MsgServer)
				return true
			}
			fname := strings.TrimSpace(parts[1])
			v.PendingDownload = filepath.Clean(fname)
			v.ChatUI.Close(true)
			return true
		case "clear":
			s.mu.Lock()
			delete(s.histories, pubHash)
			s.mu.Unlock()
			v.ChatUI.ClearMessages()
			return true
		case "doors":
			addMessage(cmd, ui.MsgCommand)
			doorList := s.doorManager.List()
			addMessage("--- Available doors ---", ui.MsgServer)
			for _, door := range doorList {
				addMessage("/"+door, ui.MsgServer)
			}
			return true
		case "files":
			addMessage(cmd, ui.MsgCommand)
			s.showFiles(v.ChatUI)
			return true
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
	// Identify current visitor to log to history
	var pubHash string
	for _, v := range s.visitors {
		if v.ChatUI == chatUI {
			pubHash = s.getPubKeyHash(v.PubKey)
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

func (s *Server) showDownloadInfo(v *Visitor, filename string) {
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
		fmt.Fprintf(v.Bus, "\033[1;31mAccess denied: %s\033[0m\r\n", filename)
		return
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(v.Bus, "\033[1;31mFile not found: %s\033[0m\r\n", filename)
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
		ClientKey:   v.PubKey,
		Filename:    filename,
		BaseDir:     s.filesDir,
		TransferID:  transferID,
		Timeout:     dt,
		UploadLimit: s.uploadLimit,
	})
	if err != nil {
		fmt.Fprintf(v.Bus, "\033[1;31mFailed to start download server: %v\033[0m\r\n", err)
		return
	}

	// Always clear screen before showing download info
	// First reset colors to avoid the black background from sticking around
	fmt.Fprint(v.Bus, "\033[m\033[2J\033[H")

	// Calculate file signature early to include it in the wrapper's download block
	sig := s.calculateFileSHA256(absPath)

	fmt.Fprintf(v.Bus, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\r\n")
	fmt.Fprintf(v.Bus, "[DOWNLOAD FILE]\r\n%s\r\n%d\r\n%s\r\n%s\r\n[/DOWNLOAD FILE]\r\n", filename, filePort, transferID, sig)
	fmt.Fprintf(v.Bus, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\r\n\r\n")

	fmt.Fprintf(v.Bus, "\033[1;32mUNN DOWNLOAD READY\033[0m\r\n\r\n")
	fmt.Fprintf(v.Bus, "The wrapper is automatically downloading the file to your Downloads folder.\r\n")

	fmt.Fprintf(v.Bus, "\033[1;33mNote: The transfer must start within %d seconds.\033[0m\r\n", int(dt.Seconds()))

	fmt.Fprintf(v.Bus, "If the wrapper fails, you can download manually using:\r\n\r\n")

	// Get actual address for manual instruction
	host, _, _ := net.SplitHostPort(s.address)
	if host == "" || host == "0.0.0.0" || host == "127.0.0.1" || host == "::" {
		host = "localhost"
	}

	fmt.Fprintf(v.Bus, "  \033[1;36mscp -P %d %s:%s ~/Downloads/%s\033[0m\r\n\r\n", filePort, host, transferID, filepath.Base(filename))

	// Display the host key fingerprint so the user can verify it
	fingerprint := s.calculateHostKeyFingerprint()
	fmt.Fprintf(v.Bus, "\033[1mHost Verification Fingerprint (tunnel):\033[0m\r\n")
	fmt.Fprintf(v.Bus, "\033[1;36m%s\033[0m\r\n\r\n", fingerprint)

	// Display the file signature (matching entrypoint style)
	fmt.Fprintf(v.Bus, "\033[1mFile Verification Signature:\033[0m\r\n")
	fmt.Fprintf(v.Bus, "\033[1;36m%s  %s\033[0m\r\n\r\n", sig, filename)

	fmt.Fprintf(v.Bus, "Disconnecting to allow the transfer...\r\n")

	// Consume the data
	v.PendingDownload = ""

	// Give the wrapper documentation and time to start the download.
	// The connection will stay open until the visitor disconnects.
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
