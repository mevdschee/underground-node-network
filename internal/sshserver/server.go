package sshserver

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/doors"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"golang.org/x/crypto/ssh"
)

// Visitor represents a connected visitor
type Visitor struct {
	Username string
	Conn     ssh.Conn
	ChatUI   *ui.ChatUI
	Bus      *ui.SSHBus
	Bridge   *ui.InputBridge
}

type Server struct {
	address        string
	config         *ssh.ServerConfig
	doorManager    *doors.Manager
	roomName       string
	visitors       map[string]*Visitor
	authorizedKeys map[string]bool // Marshaled pubkey -> true
	mu             sync.RWMutex
	listener       net.Listener
}

func NewServer(address, hostKeyPath, roomName string, doorManager *doors.Manager) (*Server, error) {
	s := &Server{
		address:        address,
		doorManager:    doorManager,
		roomName:       roomName,
		visitors:       make(map[string]*Visitor),
		authorizedKeys: make(map[string]bool),
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

		return nil, nil
	}

	// Load or generate host key
	hostKey, err := loadOrGenerateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load host key: %w", err)
	}
	config.AddHostKey(hostKey)

	s.config = config
	return s, nil
}

// AuthorizeKey registers a public key that is allowed to connect to this room
func (s *Server) AuthorizeKey(pubKey ssh.PublicKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorizedKeys[string(pubKey.Marshal())] = true
	log.Printf("Authorized key for visitor")
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
}

// Broadcast sends a message to all connected visitors
func (s *Server) Broadcast(sender, message string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	chatMsg := fmt.Sprintf("<%s> %s", sender, message)
	for _, v := range s.visitors {
		if v.ChatUI != nil {
			v.ChatUI.AddMessage(chatMsg)
		}
	}
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

	// Register visitor
	s.mu.Lock()
	v := &Visitor{
		Username: username,
		Conn:     sshConn,
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
	default:
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", channelType))
	}
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
	// Create UI bus
	v.Bridge = ui.NewInputBridge(rawChannel)
	v.Bus = ui.NewSSHBus(v.Bridge, 80, 24)

	// Handle session requests
	go func() {
		for req := range requests {
			switch req.Type {
			case "pty-req":
				if w, h, ok := ui.ParsePtyRequest(req.Payload); ok {
					v.Bus.Resize(int(w), int(h))
				}
				req.Reply(true, nil)
			case "window-change":
				if w, h, ok := ui.ParseWindowChange(req.Payload); ok {
					v.Bus.Resize(int(w), int(h))
				}
				req.Reply(true, nil)
			case "shell":
				req.Reply(true, nil)
			default:
				req.Reply(false, nil)
			}
		}
	}()

	// Main interaction loop
	s.handleInteraction(rawChannel, username)
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
	chatUI.SetTitle(fmt.Sprintf("UNN Room Interaction: %s", s.roomName))
	v.ChatUI = chatUI

	chatUI.OnSend(func(msg string) {
		s.Broadcast(username, msg)
	})

	chatUI.OnClose(func() {
		v.Bus.SignalExit()
	})

	chatUI.OnCmd(func(cmd string) bool {
		if strings.HasPrefix(cmd, "/") {
			// Echo the command in the chat history
			chatUI.AddMessage(fmt.Sprintf("<%s> %s", username, cmd))

			parts := strings.SplitN(strings.TrimPrefix(cmd, "/"), " ", 2)
			command := parts[0]

			switch command {
			case "help":
				chatUI.AddMessage("--- Available Commands ---")
				chatUI.AddMessage("/help     - Show this help")
				chatUI.AddMessage("/who      - List visitors in room")
				chatUI.AddMessage("/doors    - List available doors")
				chatUI.AddMessage("/<door>   - Enter a door")
				chatUI.AddMessage("Ctrl+C    - Exit room")
				return true
			case "who":
				visitors := s.GetVisitors()
				chatUI.AddMessage("--- Visitors in room ---")
				for _, name := range visitors {
					chatUI.AddMessage("• " + name)
				}
				return true
			case "doors":
				doorList := s.doorManager.List()
				chatUI.AddMessage("--- Available doors ---")
				for _, door := range doorList {
					chatUI.AddMessage("/" + door)
				}
				return true
			}
		}
		return false // Not handled internally, exit Run() to check if it's a door
	})

	// Add welcome message initially
	bannerPath := s.roomName + ".asc"
	if b, err := os.ReadFile(bannerPath); err == nil {
		lines := strings.Split(string(b), "\n")
		for _, line := range lines {
			chatUI.AddMessage(strings.TrimRight(line, "\r\n"))
		}
	} else {
		chatUI.AddMessage(fmt.Sprintf("*** You joined %s as %s ***", s.roomName, username))
		chatUI.AddMessage("*** Type /help for commands ***")
	}

	for {
		// Reset bus and UI for each TUI run
		v.Bus.Reset()
		chatUI.Reset()

		// Create a fresh screen for each run to avoid "already engaged" errors
		screen, err := tcell.NewTerminfoScreenFromTty(v.Bus)
		if err != nil {
			log.Printf("Failed to create screen: %v", err)
			return
		}

		if err := screen.Init(); err != nil {
			log.Printf("Failed to init screen: %v", err)
			return
		}

		// Update ChatUI with the new screen
		chatUI.SetScreen(screen)

		// Update visitors list
		s.updateVisitorList(v)

		cmd := chatUI.Run()
		screen.Fini() // Suspend tcell immediately after Run

		if cmd == "" {
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
	}
}

func (s *Server) handleCommand(channel ssh.Channel, username string, input string) chan struct{} {
	input = strings.TrimSpace(input)

	if !strings.HasPrefix(input, "/") {
		// Regular chat message
		s.Broadcast(username, input)
		return nil
	}

	cmd := strings.TrimPrefix(input, "/")
	parts := strings.SplitN(cmd, " ", 2)
	command := parts[0]

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
