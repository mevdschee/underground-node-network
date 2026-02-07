package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mevdschee/underground-node-network/internal/nat"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

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
	for {
		// Reset connection variables for each entry point session
		var (
			candidates []string
			hostKeys   []string
			sshPort    int
		)

		// Create p2pquic peer for entrypoint connection
		clientID := fmt.Sprintf("client-%s-%d", username, time.Now().UnixNano())

		// Extract host from entrypoint for signaling URL
		epHost := strings.Split(entrypoint, ":")[0]
		signalingURL := fmt.Sprintf("http://%s:8080", epHost)

		if verbose {
			log.Printf("Creating p2pquic peer: %s", clientID)
			log.Printf("Signaling server: %s", signalingURL)
		}

		p2pPeer, err := nat.NewP2PQUICPeer(clientID, 0, signalingURL, true)
		if err != nil {
			return fmt.Errorf("failed to create p2pquic peer: %w", err)
		}

		// Discover candidates and register with signaling server
		if _, err := p2pPeer.DiscoverCandidates(); err != nil {
			p2pPeer.Close()
			if verbose {
				log.Printf("Warning: Failed to discover candidates: %v", err)
			}
		}

		if err := p2pPeer.Register(); err != nil {
			p2pPeer.Close()
			if verbose {
				log.Printf("Warning: Failed to register with signaling server: %v", err)
			}
		}

		if verbose {
			log.Printf("Registered client with signaling server")
		}

		// Extract entrypoint peer ID (use "entrypoint" as the peer ID)
		entrypointPeerID := "entrypoint"

		// Connect to entry point via p2pquic
		if verbose {
			log.Printf("Connecting to entrypoint peer: %s", entrypointPeerID)
		}

		conn, err := p2pPeer.Connect(entrypointPeerID)
		if err != nil {
			p2pPeer.Close()
			return fmt.Errorf("failed to connect to entry point: %w", err)
		}
		defer p2pPeer.Close()

		sshConn, chans, reqs, err := ssh.NewClientConn(conn, entrypoint, config)
		if err != nil {
			conn.Close()
			return fmt.Errorf("SSH handshake failed: %w", err)
		}
		client := ssh.NewClient(sshConn, chans, reqs)

		if verbose {
			log.Printf("Connected to entry point")
		}

		// Open a session
		session, err := client.NewSession()
		if err != nil {
			client.Close()
			return fmt.Errorf("failed to create session: %w", err)
		}

		// Set up pipes
		stdinPipe, err := session.StdinPipe()
		if err != nil {
			session.Close()
			client.Close()
			return fmt.Errorf("failed to get stdin pipe: %w", err)
		}

		stdoutPipe, err := session.StdoutPipe()
		if err != nil {
			session.Close()
			client.Close()
			return fmt.Errorf("failed to get stdout pipe: %w", err)
		}

		// Request PTY
		width, height, err := term.GetSize(fd)
		if err != nil {
			width, height = 80, 24
		}

		if err := session.RequestPty("xterm", height, width, ssh.TerminalModes{}); err != nil {
			session.Close()
			client.Close()
			return fmt.Errorf("failed to request PTY: %w", err)
		}

		// Handle window size changes
		winch := make(chan os.Signal, 1)
		signal.Notify(winch, syscall.SIGWINCH)
		stopWinch := make(chan bool)

		go func() {
			for {
				select {
				case <-winch:
					width, height, err := term.GetSize(fd)
					if err == nil {
						session.WindowChange(height, width)
					}
				case <-stopWinch:
					return
				}
			}
		}()

		// Start interactive session
		if err := session.Shell(); err != nil {
			session.Close()
			client.Close()
			return fmt.Errorf("failed to start interactive session: %w", err)
		}

		// Variables for connection info
		var (
			done       = make(chan bool)
			userExited = make(chan bool)
		)

		// Monitor output for connection data and echo to user
		go func() {
			buf := make([]byte, 1024)
			var oscBuffer strings.Builder
			var inOSC bool
			for {
				n, err := stdoutPipe.Read(buf)
				if err != nil {
					close(userExited)
					return
				}

				for i := 0; i < n; i++ {
					b := buf[i]

					// OSC 31337 Detection: \x1b]31337; ... \x07
					if b == 0x1b && !inOSC {
						// Peek for ]31337;
						if i+7 < n && string(buf[i+1:i+8]) == "]31337;" {
							inOSC = true
							oscBuffer.Reset()
							i += 7
							continue
						}
					}

					if inOSC {
						if b == 0x07 {
							inOSC = false
							jsonData := oscBuffer.String()
							if verbose {
								log.Printf("Captured OSC 31337 data (%d bytes)", len(jsonData))
							}

							var startPayload protocol.PunchStartPayload
							if err := json.Unmarshal([]byte(jsonData), &startPayload); err == nil {
								if startPayload.Action == "teleport" {
									candidates = startPayload.Candidates
									sshPort = startPayload.SSHPort
									hostKeys = startPayload.PublicKeys
									close(done)
								} else if startPayload.Action == "popup" {
									var popupPayload protocol.PopupPayload
									if err := json.Unmarshal([]byte(jsonData), &popupPayload); err == nil {
										handleOSCPopup(popupPayload)
										if !batch {
											globalStdinManager.Pause()
											width := 80
											if w, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
												width = w
											}
											prompt := ">>> Press [ENTER] to continue <<<"
											padding := (width - len(prompt)) / 2
											if padding < 0 {
												padding = 0
											}
											fmt.Printf("\r%s\033[1m%s\033[0m", strings.Repeat(" ", padding), prompt)

											buf := make([]byte, 1)
											for {
												n, err := os.Stdin.Read(buf)
												if err != nil || n == 0 || buf[0] == '\n' || buf[0] == '\r' {
													break
												}
											}
											fmt.Print("\033[H\033[J")
											globalStdinManager.Resume()
										}
									}
								}
							} else if verbose {
								log.Printf("JSON unmarshal failed: %v", err)
							}
						} else {
							oscBuffer.WriteByte(b)
						}
						continue
					}

					os.Stdout.Write([]byte{b})
				}
			}
		}()

		// Proxy stdin using the global manager
		globalStdinManager.Start()
		globalStdinManager.SetWriter(stdinPipe)

		// If room name was provided, send it automatically
		if currentRoom != "" {
			if verbose {
				log.Printf("Automatically selecting room: %s", currentRoom)
			}
			//time.Sleep(500 * time.Millisecond) // Small delay to allow server to be ready
			fmt.Fprintf(stdinPipe, "/join %s\r\n", currentRoom)
			currentRoom = "" // Only do it once per URL invocation
		}
		var (
			shouldConnect bool
			timeout       = 300 * time.Second
		)
		select {
		case <-userExited:
			// Server closed connection, hopefully after printing data and instructions
			shouldConnect = len(candidates) > 0
		case <-done:
			// Captured connection data, jump to room immediately
			shouldConnect = true
		case <-time.After(timeout):
			return fmt.Errorf("timeout waiting for connection info")
		}

		// Ensure we've at least tried to parse the data
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
		}

		globalStdinManager.SetWriter(nil)
		session.Close()
		client.Close()

		if !shouldConnect {
			return nil // User exited manually
		}

		// Connect to room using native SSH client - stay in room if download triggered
		for {
			downloaded, popup, err := runRoomSSH(candidates, sshPort, hostKeys, config, identPath, verbose, oldState, batch)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error connecting to room: %v\n", err)
				time.Sleep(2 * time.Second)
				break
			}

			// Show disconnect message
			fmt.Fprintf(os.Stderr, "\r\n\033[1;33m>>> Disconnected from room: %s <<<\033[0m\r\n\r\n", roomName)

			if popup != nil {
				handleOSCPopup(*popup)
				if !batch {
					globalStdinManager.Pause()
					// Centered prompt
					width := 80
					if w, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
						width = w
					}
					prompt := ">>> Press [ENTER] to continue <<<"
					padding := (width - len(prompt)) / 2
					if padding < 0 {
						padding = 0
					}
					fmt.Printf("\r%s\033[1m%s\033[0m", strings.Repeat(" ", padding), prompt)

					// Wait for Enter (CR or LF)
					buf := make([]byte, 1)
					for {
						n, err := os.Stdin.Read(buf)
						if err != nil || n == 0 || buf[0] == '\n' || buf[0] == '\r' {
							break
						}
					}
					fmt.Print("\033[H\033[J") // Clear screen again after Enter
					globalStdinManager.Resume()
				}
				if popup.Type == "error" {
					return nil // Stop teleportation if it was a kick/error
				}
			}

			if !downloaded {
				break // Exit back to entry point
			}
			if verbose {
				log.Printf("Reconnecting to room after automatic download...")
			}
		}
	}
}
