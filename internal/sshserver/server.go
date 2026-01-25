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

	"github.com/mevdschee/underground-node-network/internal/doors"
	"golang.org/x/crypto/ssh"
)

// Visitor represents a connected visitor
type Visitor struct {
	Username string
	Conn     ssh.Conn
}

// Server is an ephemeral SSH server for the UNN node
type Server struct {
	address     string
	config      *ssh.ServerConfig
	doorManager *doors.Manager
	roomName    string
	visitors    map[string]*Visitor
	mu          sync.RWMutex
	listener    net.Listener
}

// NewServer creates a new SSH server
func NewServer(address, hostKeyPath, roomName string, doorManager *doors.Manager) (*Server, error) {
	config := &ssh.ServerConfig{
		NoClientAuth: true, // For now, allow any connection
	}

	// Load or generate host key
	hostKey, err := loadOrGenerateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load host key: %w", err)
	}
	config.AddHostKey(hostKey)

	return &Server{
		address:     address,
		config:      config,
		doorManager: doorManager,
		roomName:    roomName,
		visitors:    make(map[string]*Visitor),
	}, nil
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
	log.Printf("Visitor connected: %s", username)

	// Register visitor
	s.mu.Lock()
	s.visitors[username] = &Visitor{
		Username: username,
		Conn:     sshConn,
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.visitors, username)
		s.mu.Unlock()
		log.Printf("Visitor disconnected: %s", username)
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
	channel, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("Could not accept session: %v", err)
		return
	}
	defer channel.Close()

	// Handle session requests
	go func() {
		for req := range requests {
			switch req.Type {
			case "shell", "pty-req":
				req.Reply(true, nil)
			case "subsystem":
				subsystem := string(req.Payload[4:])
				if subsystem == "unn-room" {
					req.Reply(true, nil)
				} else {
					req.Reply(false, nil)
				}
			default:
				req.Reply(false, nil)
			}
		}
	}()

	// Welcome message
	s.sendWelcome(channel, username)

	// Main interaction loop
	s.handleInteraction(channel, username)
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

func (s *Server) sendWelcome(w io.Writer, username string) {
	fmt.Fprintf(w, "\r\n")
	fmt.Fprintf(w, "╔═══════════════════════════════════════════════════════════════╗\r\n")
	fmt.Fprintf(w, "║  Welcome to %-50s║\r\n", s.roomName+"'s room")
	fmt.Fprintf(w, "║  Underground Node Network                                     ║\r\n")
	fmt.Fprintf(w, "╚═══════════════════════════════════════════════════════════════╝\r\n")
	fmt.Fprintf(w, "\r\n")
	fmt.Fprintf(w, "Hello, %s! You have entered the room.\r\n", username)
	fmt.Fprintf(w, "\r\n")

	// List available doors
	doorList := s.doorManager.List()
	if len(doorList) > 0 {
		fmt.Fprintf(w, "Available doors:\r\n")
		for _, door := range doorList {
			fmt.Fprintf(w, "  /%s\r\n", door)
		}
	} else {
		fmt.Fprintf(w, "No doors available.\r\n")
	}
	fmt.Fprintf(w, "\r\nType /help for commands.\r\n\r\n")
}

func (s *Server) handleInteraction(channel ssh.Channel, username string) {
	buf := make([]byte, 1024)
	var line []byte
	var history []string
	historyIndex := -1
	currentLineBackup := ""
	escState := 0

	var doorStdin io.WriteCloser
	var doorDone chan struct{}
	var doorMu sync.Mutex

	for {
		n, err := channel.Read(buf)
		if err != nil {
			return
		}

		for i := 0; i < n; i++ {
			b := buf[i]

			doorMu.Lock()
			if doorDone != nil {
				select {
				case <-doorDone:
					doorStdin = nil
					doorDone = nil
				default:
				}
			}

			if doorStdin != nil {
				if b == 3 { // Ctrl+C
					fmt.Fprintf(channel, "\r\n[Interrupting door...]\r\n")
					doorStdin.Close()
					// doorStdin/doorDone will be cleared by the select in the next iteration
					// or we can clear them here to be immediate.
					doorStdin = nil
					// We don't clear doorDone here because the goroutine still needs to finish
					doorMu.Unlock()
					continue
				}
				doorStdin.Write([]byte{b})
				doorMu.Unlock()
				continue
			}
			doorMu.Unlock()

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

			// Handle special characters
			switch b {
			case '\r', '\n':
				fmt.Fprintf(channel, "\r\n")
				if len(line) > 0 {
					cmd := string(line)
					// Special case: handleCommand might start a door
					pw, done := s.handleCommand(channel, username, cmd)
					if pw != nil {
						doorMu.Lock()
						doorStdin = pw
						doorDone = done
						doorMu.Unlock()
					}

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

func (s *Server) handleCommand(channel ssh.Channel, username string, input string) (io.WriteCloser, chan struct{}) {
	input = strings.TrimSpace(input)

	if !strings.HasPrefix(input, "/") {
		// Regular chat message
		fmt.Fprintf(channel, "\r<%s> %s\r\n", username, input)
		return nil, nil
	}

	cmd := strings.TrimPrefix(input, "/")
	parts := strings.SplitN(cmd, " ", 2)
	command := parts[0]

	switch command {
	case "exit":
		channel.Close()
		return nil, nil
	case "help":
		fmt.Fprintf(channel, "\rCommands:\r\n")
		fmt.Fprintf(channel, "  /help     - Show this help\r\n")
		fmt.Fprintf(channel, "  /who      - List visitors in room\r\n")
		fmt.Fprintf(channel, "  /doors    - List available doors\r\n")
		fmt.Fprintf(channel, "  /<door>   - Enter a door\r\n")
		fmt.Fprintf(channel, "  /exit     - Exit room\r\n")
		fmt.Fprintf(channel, "  Ctrl+C    - Exit room\r\n")
		return nil, nil
	case "who":
		visitors := s.GetVisitors()
		fmt.Fprintf(channel, "\rVisitors in room:\r\n")
		for _, v := range visitors {
			fmt.Fprintf(channel, "  %s\r\n", v)
		}
		return nil, nil
	case "doors":
		doorList := s.doorManager.List()
		fmt.Fprintf(channel, "\rAvailable doors:\r\n")
		for _, door := range doorList {
			fmt.Fprintf(channel, "  /%s\r\n", door)
		}
		return nil, nil
	default:
		// Try to execute as a door
		if _, ok := s.doorManager.Get(command); ok {
			fmt.Fprintf(channel, "\r[Entering door: %s]\r\n", command)
			pr, pw := io.Pipe()
			done := make(chan struct{})
			go func() {
				if err := s.doorManager.Execute(command, pr, channel, channel); err != nil {
					fmt.Fprintf(channel, "\r[Door error: %v]\r\n", err)
				}
				pw.Close()
				fmt.Fprintf(channel, "\r[Exited door: %s]\r\n", command)
				close(done)
			}()
			return pw, done
		} else {
			fmt.Fprintf(channel, "\rUnknown command: %s\r\n", command)
		}
		return nil, nil
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
