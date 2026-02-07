package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mevdschee/p2pquic-go/pkg/p2pquic"
	"github.com/mevdschee/underground-node-network/internal/nat"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// RoomInfo is an alias for the entrypoint client's roomInfo type
type RoomInfo = roomInfo

var globalDownloadsDir string

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

	// Loop for persistent entry point session
	currentRoom := roomName

	// First, connect to entrypoint to handle registration
	if verbose {
		log.Printf("Connecting to entrypoint for registration check...")
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
	entrypointClient, err := ssh.Dial("tcp", ipv4Address, config)
	if err != nil {
		return fmt.Errorf("failed to connect to entrypoint: %w", err)
	}
	defer entrypointClient.Close()

	// Handle registration/verification
	verifiedUsername, err := handleRegistration(entrypointClient, username)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	if verbose {
		log.Printf("Verified as: %s", verifiedUsername)
	}

	// Now we can proceed with room teleportation

	// Create an EntrypointClient to query for room info
	epClient, err := NewEntrypointClient(entrypointClient)
	if err != nil {
		return fmt.Errorf("failed to create entrypoint client: %w", err)
	}
	defer epClient.Close()

	// Get list of rooms
	rooms, err := epClient.GetRooms()
	if err != nil {
		return fmt.Errorf("failed to get room list: %w", err)
	}

	// If no room specified, show interactive browser
	if currentRoom == "" {
		if len(rooms) == 0 {
			fmt.Printf("\nNo rooms currently online.\n")
			return nil
		}

		fmt.Printf("\n\033[1mAvailable Rooms:\033[0m\n\n")
		for i, room := range rooms {
			peopleText := "person"
			if room.PeopleCount != 1 {
				peopleText = "people"
			}
			fmt.Printf("  \033[1;36m%d.\033[0m \033[1m%s\033[0m (%d %s)\n",
				i+1, room.Name, room.PeopleCount, peopleText)
			if len(room.Doors) > 0 {
				fmt.Printf("     Doors: %s\n", strings.Join(room.Doors, ", "))
			}
		}

		fmt.Printf("\n\033[1mEnter room number or name (or 'q' to quit):\033[0m ")

		var input string
		fmt.Scanln(&input)

		if input == "q" || input == "quit" || input == "" {
			return nil
		}

		// Try to parse as number first
		var selectedRoom *roomInfo
		num := 0
		if _, err := fmt.Sscanf(input, "%d", &num); err == nil && num > 0 && num <= len(rooms) {
			selectedRoom = &rooms[num-1]
		} else {
			// Try to match by name
			for i := range rooms {
				if rooms[i].Name == input {
					selectedRoom = &rooms[i]
					break
				}
			}
		}

		if selectedRoom == nil {
			return fmt.Errorf("invalid selection: %s", input)
		}

		currentRoom = selectedRoom.Name
		if verbose {
			log.Printf("Selected room: %s", currentRoom)
		}
	}

	// User has selected or requested a specific room
	if verbose {
		log.Printf("Connecting to room: %s", currentRoom)
	}

	// Find the requested room in the list
	var targetRoom *RoomInfo
	for i := range rooms {
		if rooms[i].Name == currentRoom {
			targetRoom = &rooms[i]
			break
		}
	}

	if targetRoom == nil {
		fmt.Printf("Room '%s' not found.\n", currentRoom)
		fmt.Printf("\nAvailable rooms:\n")
		if len(rooms) == 0 {
			fmt.Printf("  (no rooms currently online)\n")
		} else {
			for _, room := range rooms {
				fmt.Printf("  - %s (%d people)\n", room.Name, room.PeopleCount)
			}
		}
		return fmt.Errorf("room not found")
	}

	if verbose {
		log.Printf("Found room: %s with %d people", targetRoom.Name, targetRoom.PeopleCount)
		log.Printf("Room SSH port: %d", targetRoom.SSHPort)
		log.Printf("Room candidates: %v", targetRoom.Candidates)
	}

	// Connect to room via p2pquic using SSH signaling
	if verbose {
		log.Printf("Connecting to room via p2pquic")
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
	// Use Bind() instead of Listen() since clients only dial out, not accept connections
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
	signalingClient, err := nat.NewSSHSignalingClient(entrypointClient)
	if err != nil {
		return fmt.Errorf("failed to create signaling client: %w", err)
	}
	defer signalingClient.Close()

	if err := signalingClient.Register(clientID, clientCandidates); err != nil {
		return fmt.Errorf("failed to register with signaling:  %w", err)
	}

	if verbose {
		log.Printf("Registered with signaling server")
	}

	// Get room's peer info from signaling
	roomPeerID := fmt.Sprintf("room-%s", currentRoom)
	roomPeerInfo, err := signalingClient.GetPeer(roomPeerID)
	if err != nil {
		return fmt.Errorf("failed to get room peer info: %w", err)
	}

	if verbose {
		log.Printf("Got room peer info with %d candidates", len(roomPeerInfo.Candidates))
	}

	// Convert p2pquic.Candidate to string addresses for connection
	roomCandidates := make([]string, len(roomPeerInfo.Candidates))
	for i, cand := range roomPeerInfo.Candidates {
		roomCandidates[i] = fmt.Sprintf("%s:%d", cand.IP, cand.Port)
	}

	// Request coordinated hole-punching via entrypoint
	// This tells the room to start punching to us while we punch to it
	clientCandidateStrs := make([]string, len(clientCandidates))
	for i, c := range clientCandidates {
		clientCandidateStrs[i] = fmt.Sprintf("%s:%d", c.IP, c.Port)
	}

	if verbose {
		log.Printf("Requesting coordinated punch to room %s", currentRoom)
	}

	if err := epClient.RequestPreparePunch(currentRoom, clientID, clientCandidateStrs); err != nil {
		log.Printf("Warning: Coordinated punch request failed: %v (continuing anyway)", err)
	} else if verbose {
		log.Printf("Room notified to start punching, waiting briefly...")
	}

	// Connect via p2pquic using the peer info we got from SSH signaling
	if verbose {
		log.Printf("Attempting p2pquic connection to room peer: %s with %d candidates", roomPeerID, len(roomCandidates))
	}

	// Convert room candidates to p2pquic.Candidate
	p2pRoomCandidates := make([]p2pquic.Candidate, len(roomPeerInfo.Candidates))
	for i, c := range roomPeerInfo.Candidates {
		p2pRoomCandidates[i] = p2pquic.Candidate{
			IP:   c.IP,
			Port: c.Port,
		}
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
	sshConnWrapper, chans, reqs, err := ssh.NewClientConn(sshConn, targetRoom.Name, config)
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

	// Set up pipes
	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	// Request PTY
	fd = int(os.Stdin.Fd())
	var width, height = 80, 24
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

	// Start shell
	if err := session.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %w", err)
	}

	fmt.Printf("\n\033[1;32m>>> Connected to room: %s <<<\033[0m\n\n", currentRoom)

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

	fmt.Printf("\n\033[1;33m>>> Disconnected from room: %s <<<\033[0m\n\n", currentRoom)

	return nil
}
