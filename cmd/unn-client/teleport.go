package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mevdschee/p2pquic-go/pkg/p2pquic"
	"github.com/mevdschee/underground-node-network/internal/nat"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

var globalDownloadsDir string

// TeleportData received via OSC from server
type TeleportData struct {
	RoomName   string   `json:"room_name"`
	Candidates []string `json:"candidates"`
	SSHPort    int      `json:"ssh_port"`
	PublicKeys []string `json:"public_keys,omitempty"`
}

func teleport(unnUrl string, identPath string, verbose bool, batch bool, downloadsDir string) error {
	globalDownloadsDir = downloadsDir
	// Parse the SSH URL
	u, err := url.Parse(unnUrl)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "unn" {
		return fmt.Errorf("URL must use unn:// scheme")
	}

	// Extract components
	entrypoint := u.Host
	if entrypoint == "" {
		return fmt.Errorf("no entrypoint hostname specified")
	}

	// Default port if not specified
	if !strings.Contains(entrypoint, ":") {
		entrypoint += ":44322"
	}

	username := u.User.Username()
	if username == "" {
		username = os.Getenv("USER")
		if username == "" {
			username = "visitor"
		}
	}

	roomName := strings.TrimPrefix(u.Path, "/")

	if verbose {
		log.Printf("Connecting to entry point: %s@%s", username, entrypoint)
		if roomName != "" {
			log.Printf("Target room: %s", roomName)
		} else {
			log.Printf("Interactive selection mode")
		}
	}

	// Load identity key
	var authMethods []ssh.AuthMethod

	if identPath != "" {
		signer, err := loadKey(identPath)
		if err != nil {
			return fmt.Errorf("failed to load identity key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else {
		// Try standard SSH keys
		homeDir, _ := os.UserHomeDir()
		possibleKeys := []string{
			filepath.Join(homeDir, ".ssh", "id_ed25519"),
			filepath.Join(homeDir, ".ssh", "id_rsa"),
			filepath.Join(homeDir, ".unn", "user_key"),
		}

		for _, keyPath := range possibleKeys {
			signer, err := loadKey(keyPath)
			if err == nil {
				authMethods = append(authMethods, ssh.PublicKeys(signer))
				if verbose {
					log.Printf("Using identity: %s", keyPath)
				}
				break
			}
		}
	}

	if len(authMethods) == 0 {
		return fmt.Errorf("no SSH identity found. Use -identity or ensure ~/.ssh/id_rsa or id_ed25519 exists")
	}

	// Connect to entry point configuration
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
		ClientVersion:   "SSH-2.0-UNN-CLIENT",
	}

	// Set terminal to raw mode for the entire duration - only if NOT in batch mode
	fd := int(os.Stdin.Fd())
	var oldState *term.State
	if !batch {
		var err error
		oldState, err = term.MakeRaw(fd)
		if err == nil {
			defer term.Restore(fd, oldState)
		}
	}

	// Resolve to IPv4 only
	host, port, err := net.SplitHostPort(entrypoint)
	if err != nil {
		return fmt.Errorf("invalid entrypoint address: %w", err)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve %s: %w", host, err)
	}

	var ipv4Addr string
	for _, ip := range ips {
		if ip.To4() != nil {
			ipv4Addr = ip.String()
			break
		}
	}

	if ipv4Addr == "" {
		return fmt.Errorf("no IPv4 address found for %s", host)
	}

	ipv4Address := net.JoinHostPort(ipv4Addr, port)

	// Mutex-protected current stdin destination
	var stdinMu sync.Mutex
	var currentStdin io.Writer

	// Single goroutine reads from os.Stdin and writes to currentStdin
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				stdinMu.Lock()
				if currentStdin != nil {
					currentStdin.Write(buf[:n])
				}
				stdinMu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	// Main loop - reconnect to entrypoint after disconnecting from room
	for {
		entrypointSSH, err := ssh.Dial("tcp", ipv4Address, config)
		if err != nil {
			return fmt.Errorf("failed to connect to entrypoint: %w", err)
		}

		if verbose {
			log.Printf("Connected to entrypoint")
		}

		// Create a new session for the interactive TUI
		session, err := entrypointSSH.NewSession()
		if err != nil {
			entrypointSSH.Close()
			return fmt.Errorf("failed to create session: %w", err)
		}

		// Get terminal size
		width, height := 80, 24
		if !batch {
			var w, h int
			w, h, err = term.GetSize(fd)
			if err == nil {
				width, height = w, h
			}
		}

		// Request PTY
		if err := session.RequestPty("xterm-256color", height, width, ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}); err != nil {
			session.Close()
			entrypointSSH.Close()
			return fmt.Errorf("failed to request PTY: %w", err)
		}

		// Set up pipes for I/O with OSC parsing
		stdin, err := session.StdinPipe()
		if err != nil {
			session.Close()
			entrypointSSH.Close()
			return fmt.Errorf("failed to get stdin pipe: %w", err)
		}

		stdout, err := session.StdoutPipe()
		if err != nil {
			session.Close()
			entrypointSSH.Close()
			return fmt.Errorf("failed to get stdout pipe: %w", err)
		}

		// Channel to receive teleport data
		teleportChan := make(chan *TeleportData, 1)
		var teleportOnce sync.Once

		// Start shell
		if err := session.Shell(); err != nil {
			session.Close()
			entrypointSSH.Close()
			return fmt.Errorf("failed to start shell: %w", err)
		}

		// If user specified a room on first connection, send join command
		if roomName != "" {
			go func(room string) {
				//time.Sleep(500 * time.Millisecond) // Wait for TUI to initialize
				stdin.Write([]byte("/join " + room + "\r"))
			}(roomName)
			roomName = "" // Only auto-join on first connection
		}

		// Set current stdin destination
		stdinMu.Lock()
		currentStdin = stdin
		stdinMu.Unlock()

		// Copy session output to stdout, parsing OSC sequences
		go func() {
			parseOSCOutput(stdout, os.Stdout, func(data *TeleportData) {
				teleportOnce.Do(func() {
					teleportChan <- data
				})
			})
		}()

		// Wait for session to end or teleport data
		sessionDone := make(chan error, 1)
		go func() {
			sessionDone <- session.Wait()
		}()

		var teleportData *TeleportData
		shouldReconnect := false

		select {
		case teleportData = <-teleportChan:
			// We received teleport data - connect to room via p2pquic
			stdinMu.Lock()
			currentStdin = nil
			stdinMu.Unlock()
			session.Close()

			err := connectToRoom(entrypointSSH, config, teleportData, verbose, batch, &stdinMu, &currentStdin)
			entrypointSSH.Close()

			if err != nil {
				log.Printf("Room connection error: %v", err)
			}

			// After room disconnect, reconnect to entrypoint
			shouldReconnect = true

		case err := <-sessionDone:
			stdinMu.Lock()
			currentStdin = nil
			stdinMu.Unlock()
			session.Close()
			entrypointSSH.Close()

			if err != nil {
				if _, ok := err.(*ssh.ExitError); ok {
					// Normal exit - user quit
					return nil
				}
				if err.Error() == "wait: remote command exited without exit status or exit signal" {
					return nil
				}
				return fmt.Errorf("session error: %w", err)
			}
			// Clean exit from entrypoint UI
			return nil
		}

		if !shouldReconnect {
			break
		}

		if verbose {
			log.Printf("Reconnecting to entrypoint...")
		}
	}

	return nil
}

// parseOSCOutput reads from r, writes to w, and calls onTeleport when OSC 31337 teleport data is found
func parseOSCOutput(r io.Reader, w io.Writer, onTeleport func(*TeleportData)) {
	buf := make([]byte, 4096)
	oscBuffer := make([]byte, 0, 8192)
	inOSC := false

	for {
		n, err := r.Read(buf)
		if n > 0 {
			data := buf[:n]
			writeStart := 0

			for i := 0; i < len(data); i++ {
				if inOSC {
					if data[i] == 0x07 { // BEL - end of OSC
						oscBuffer = append(oscBuffer, data[writeStart:i]...)
						handleOSC(oscBuffer, onTeleport)
						oscBuffer = oscBuffer[:0]
						inOSC = false
						writeStart = i + 1
					} else if data[i] == 0x1b && i+1 < len(data) && data[i+1] == '\\' {
						// ST (\x1b\\) - alternative end of OSC
						oscBuffer = append(oscBuffer, data[writeStart:i]...)
						handleOSC(oscBuffer, onTeleport)
						oscBuffer = oscBuffer[:0]
						inOSC = false
						writeStart = i + 2
						i++ // Skip the backslash
					}
				} else {
					// Check for OSC start: ESC ]
					if data[i] == 0x1b && i+1 < len(data) && data[i+1] == ']' {
						// Write everything before this OSC
						w.Write(data[writeStart:i])
						inOSC = true
						oscBuffer = oscBuffer[:0]
						writeStart = i + 2 // Skip ESC ]
						i++                // Skip the ]
					}
				}
			}

			if inOSC {
				// Still in OSC, buffer the rest
				oscBuffer = append(oscBuffer, data[writeStart:]...)
			} else {
				// Write the rest normally
				w.Write(data[writeStart:])
			}
		}
		if err != nil {
			break
		}
	}
}

func handleOSC(data []byte, onTeleport func(*TeleportData)) {
	// OSC format: 31337;{"action":"teleport",...}
	content := string(data)
	if !strings.HasPrefix(content, "31337;") {
		return
	}

	jsonData := content[6:] // Skip "31337;"

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &payload); err != nil {
		return
	}

	action, _ := payload["action"].(string)
	if action == "teleport" {
		teleportData := &TeleportData{}
		if roomName, ok := payload["room_name"].(string); ok {
			teleportData.RoomName = roomName
		}
		if candidates, ok := payload["candidates"].([]interface{}); ok {
			for _, c := range candidates {
				if s, ok := c.(string); ok {
					teleportData.Candidates = append(teleportData.Candidates, s)
				}
			}
		}
		if sshPort, ok := payload["ssh_port"].(float64); ok {
			teleportData.SSHPort = int(sshPort)
		}
		if keys, ok := payload["public_keys"].([]interface{}); ok {
			for _, k := range keys {
				if s, ok := k.(string); ok {
					teleportData.PublicKeys = append(teleportData.PublicKeys, s)
				}
			}
		}
		onTeleport(teleportData)
	}
}

func connectToRoom(entrypointSSH *ssh.Client, config *ssh.ClientConfig, teleportData *TeleportData, verbose, batch bool, stdinMu *sync.Mutex, currentStdin *io.Writer) error {
	// Suppress log output during connection unless verbose
	if !verbose {
		log.SetOutput(io.Discard)
		defer log.SetOutput(os.Stderr)
	}

	// Create p2pquic peer for client
	clientID := fmt.Sprintf("client-%d", time.Now().UnixNano())
	p2pConfig := p2pquic.Config{
		PeerID:       clientID,
		LocalPort:    0,  // OS will assign an available port
		SignalingURL: "", // Using SSH-based signaling
		EnableSTUN:   false,
	}
	p2pPeer, err := p2pquic.NewPeer(p2pConfig)
	if err != nil {
		return fmt.Errorf("failed to create p2pquic peer: %w", err)
	}
	defer p2pPeer.Close()

	// Bind the UDP socket first to get the actual port assigned by the OS
	if err := p2pPeer.Bind(); err != nil {
		return fmt.Errorf("failed to bind p2pquic socket: %w", err)
	}

	if verbose {
		log.Printf("Client bound to UDP port %d", p2pPeer.GetActualPort())
	}

	// Discover client candidates (now with actual port)
	clientCandidates, err := p2pPeer.DiscoverCandidates()
	if err != nil {
		return fmt.Errorf("failed to discover candidates: %w", err)
	}

	if verbose {
		log.Printf("Client discovered %d candidates", len(clientCandidates))
	}

	// Register client with signaling via SSH
	signalingClient, err := nat.NewSSHSignalingClient(entrypointSSH)
	if err != nil {
		return fmt.Errorf("failed to create signaling client: %w", err)
	}
	defer signalingClient.Close()

	if err := signalingClient.Register(clientID, clientCandidates); err != nil {
		return fmt.Errorf("failed to register with signaling: %w", err)
	}

	if verbose {
		log.Printf("Registered with signaling server")
	}

	// Get room's peer info from signaling
	roomPeerID := fmt.Sprintf("room-%s", teleportData.RoomName)
	roomPeerInfo, err := signalingClient.GetPeer(roomPeerID)
	if err != nil {
		return fmt.Errorf("failed to get room peer info: %w", err)
	}

	if verbose {
		log.Printf("Got room peer info with %d candidates", len(roomPeerInfo.Candidates))
	}

	// Convert p2pquic.Candidate to p2pquic.Candidate for connection
	p2pRoomCandidates := make([]p2pquic.Candidate, len(roomPeerInfo.Candidates))
	for i, c := range roomPeerInfo.Candidates {
		p2pRoomCandidates[i] = p2pquic.Candidate{
			IP:   c.IP,
			Port: c.Port,
		}
	}

	// Request coordinated hole-punching via entrypoint
	clientCandidateStrs := make([]string, len(clientCandidates))
	for i, c := range clientCandidates {
		clientCandidateStrs[i] = fmt.Sprintf("%s:%d", c.IP, c.Port)
	}

	if verbose {
		log.Printf("Requesting coordinated punch to room %s", teleportData.RoomName)
	}

	// Create entrypoint API client for punch request
	epClient, err := NewEntrypointClient(entrypointSSH)
	if err != nil {
		log.Printf("Warning: Could not create entrypoint client for punch coordination: %v", err)
	} else {
		defer epClient.Close()
		if err := epClient.RequestPreparePunch(teleportData.RoomName, clientID, clientCandidateStrs); err != nil {
			log.Printf("Warning: Coordinated punch request failed: %v (continuing anyway)", err)
		} else if verbose {
			log.Printf("Room notified to start punching, waiting briefly...")
		}
	}

	// Connect via p2pquic using the peer info we got from SSH signaling
	if verbose {
		log.Printf("Attempting p2pquic connection to room peer: %s with %d candidates", roomPeerID, len(p2pRoomCandidates))
	}

	// Connect and get the underlying QUIC connection using peer info
	ctx := context.Background()
	quicConn, err := p2pPeer.Connect(roomPeerID, p2pquic.WithCandidates(p2pRoomCandidates...))
	if err != nil {
		return fmt.Errorf("failed to connect via p2pquic: %w", err)
	}
	defer quicConn.CloseWithError(0, "client disconnecting")

	if verbose {
		log.Printf("p2pquic connection established")
	}

	// Open a stream for SSH
	stream, err := quicConn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}

	// Wrap stream as net.Conn for SSH
	sshConn := nat.NewQUICStreamConn(stream, quicConn)

	// Connect SSH client over the QUIC stream
	sshConnWrapper, chans, reqs, err := ssh.NewClientConn(sshConn, teleportData.RoomName, config)
	if err != nil {
		return fmt.Errorf("failed to establish SSH over p2pquic: %w", err)
	}

	roomSSHClient := ssh.NewClient(sshConnWrapper, chans, reqs)
	defer roomSSHClient.Close()

	// Open a session
	session, err := roomSSHClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create room session: %w", err)
	}
	defer session.Close()

	// Get stdin pipe for room session
	roomStdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get room stdin pipe: %w", err)
	}

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	// Request PTY
	fd := int(os.Stdin.Fd())
	width, height := 80, 24
	if !batch {
		var w, h int
		w, h, err = term.GetSize(fd)
		if err == nil {
			width, height = w, h
		}
	}

	if err := session.RequestPty("xterm-256color", height, width, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		return fmt.Errorf("failed to request PTY: %w", err)
	}

	if err := session.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %w", err)
	}

	// Set room stdin as current destination
	stdinMu.Lock()
	*currentStdin = roomStdin
	stdinMu.Unlock()

	fmt.Printf("\n\033[1;32m>>> Connected to room: %s <<<\033[0m\n\n", teleportData.RoomName)

	// Wait for session to end
	if err := session.Wait(); err != nil {
		// Exit status errors are normal when user disconnects
		if _, ok := err.(*ssh.ExitError); ok {
			// Normal exit
		} else if err.Error() == "wait: remote command exited without exit status or exit signal" {
			// Connection closed without clean exit - still normal
		} else {
			return fmt.Errorf("session error: %w", err)
		}
	}

	fmt.Printf("\n\033[1;33m>>> Disconnected from room: %s <<<\033[0m\n\n", teleportData.RoomName)

	return nil
}
