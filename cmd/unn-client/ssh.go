package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mevdschee/p2pquic-go/pkg/p2pquic"
	"github.com/mevdschee/underground-node-network/internal/nat"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func runRoomSSH(candidates []string, sshPort int, hostKeys []string, entrypointConfig *ssh.ClientConfig, identPath string, verbose bool, normalState *term.State, batch bool) (bool, *protocol.PopupPayload, error) {
	if verbose {
		log.Printf("Got connection info: candidates=%v port=%d keys=%d", candidates, sshPort, len(hostKeys))
	}

	// Prepare host key callback
	parsedHostKeys := make([]ssh.PublicKey, 0, len(hostKeys))
	for _, keyStr := range hostKeys {
		pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
		if err == nil {
			parsedHostKeys = append(parsedHostKeys, pubKey)
		}
	}

	hostKeyCallback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		keyBytes := key.Marshal()
		for _, hk := range parsedHostKeys {
			if bytes.Equal(hk.Marshal(), keyBytes) {
				return nil
			}
		}
		return fmt.Errorf("host key mismatch")
	}

	// Use the same auth as the entrypoint
	config := &ssh.ClientConfig{
		User:            entrypointConfig.User,
		Auth:            entrypointConfig.Auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
		ClientVersion:   "SSH-2.0-UNN-CLIENT",
	}

	// Try each candidate
	var lastErr error
	for _, candidate := range candidates {
		target := candidate
		if strings.Count(candidate, ":") >= 2 {
			parts := strings.Split(candidate, ":")
			if len(parts) == 3 {
				target = net.JoinHostPort(parts[1], parts[2])
			}
		} else if !strings.Contains(candidate, ":") {
			target = net.JoinHostPort(candidate, strconv.Itoa(sshPort))
		}

		if verbose {
			log.Printf("Attempting to connect to %s", target)
		}

		// Extract room peer ID from candidate (format: "roomname:ip:port")
		var roomPeerID string
		if strings.Count(candidate, ":") >= 2 {
			parts := strings.Split(candidate, ":")
			if len(parts) >= 1 {
				roomPeerID = parts[0]
			}
		}

		if roomPeerID == "" {
			if verbose {
				log.Printf("Could not extract room peer ID from candidate: %s", candidate)
			}
			continue
		}

		// Create p2pquic peer for room connection
		clientID := fmt.Sprintf("client-%s-%d", entrypointConfig.User, time.Now().UnixNano())

		// Extract signaling URL from target
		host, _, err := net.SplitHostPort(target)
		if err != nil {
			lastErr = err
			continue
		}
		signalingURL := fmt.Sprintf("http://%s:8080", host)

		if verbose {
			log.Printf("Creating p2pquic peer for room connection: %s", clientID)
			log.Printf("Signaling URL: %s", signalingURL)
			log.Printf("Room peer ID: %s", roomPeerID)
		}

		p2pConfig := p2pquic.Config{
			PeerID:       clientID,
			LocalPort:    0,
			SignalingURL: signalingURL,
			EnableSTUN:   true,
		}
		p2pPeer, err := p2pquic.NewPeer(p2pConfig)
		if err != nil {
			lastErr = err
			if verbose {
				log.Printf("Failed to create p2pquic peer: %v", err)
			}
			continue
		}

		// Discover candidates and register
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

		// Connect to room via p2pquic
		quicConn, err := p2pPeer.Connect(roomPeerID)
		if err != nil {
			p2pPeer.Close()
			lastErr = err
			if verbose {
				log.Printf("p2pquic connection failed to %s: %v", roomPeerID, err)
			}
			continue
		}

		// Open a stream for SSH
		ctx := context.Background()
		stream, err := quicConn.OpenStreamSync(ctx)
		if err != nil {
			quicConn.CloseWithError(0, "failed to open stream")
			p2pPeer.Close()
			lastErr = err
			continue
		}

		conn := nat.NewQUICStreamConn(stream, quicConn)
		defer p2pPeer.Close()

		sshConn, chans, reqs, err := ssh.NewClientConn(conn, target, config)
		if err != nil {
			conn.Close()
			p2pPeer.Close()
			lastErr = err
			continue
		}
		client := ssh.NewClient(sshConn, chans, reqs)
		defer client.Close()

		if verbose {
			log.Printf("Connected to room at %s", target)
		}

		session, err := client.NewSession()
		if err != nil {
			return false, nil, fmt.Errorf("failed to create session: %w", err)
		}
		defer session.Close()

		stdin, _ := session.StdinPipe()
		globalStdinManager.SetWriter(stdin)

		stdout, _ := session.StdoutPipe()
		stderr, _ := session.StderrPipe()

		fd := int(os.Stdin.Fd())
		w, h, err := term.GetSize(fd)
		if err != nil {
			w, h = 80, 24
		}

		if err := session.RequestPty("xterm-256color", h, w, ssh.TerminalModes{}); err != nil {
			return false, nil, fmt.Errorf("failed to request pty: %w", err)
		}

		if err := session.Shell(); err != nil {
			return false, nil, fmt.Errorf("failed to start session: %w", err)
		}
		// Handle window changes
		winch := make(chan os.Signal, 1)
		signal.Notify(winch, syscall.SIGWINCH)
		defer signal.Stop(winch)

		go func() {
			for range winch {
				w, h, err := term.GetSize(fd)
				if err == nil {
					session.WindowChange(h, w)
				}
			}
		}()

		// Track session state
		var (
			stdoutDone = make(chan struct{})
			lastPopup  *protocol.PopupPayload
			popupMu    sync.Mutex
		)

		go func() {
			defer close(stdoutDone)
			buf := make([]byte, 1024)
			var oscBuffer strings.Builder
			var inOSC bool

			for {
				n, err := stdout.Read(buf)
				if err != nil {
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

							var payload protocol.PopupPayload
							if err := json.Unmarshal([]byte(jsonData), &payload); err == nil {
								if payload.Action == "popup" {
									popupMu.Lock()
									lastPopup = &payload
									popupMu.Unlock()
								} else if payload.Action == "transfer_block" {
									var blockPayload protocol.FileBlockPayload
									if err := json.Unmarshal([]byte(jsonData), &blockPayload); err == nil {
										handleOSCBlockTransfer(blockPayload, verbose)
									}
								}
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

		go func() {
			io.Copy(os.Stderr, stderr)
		}()

		// Wait for session to end OR for a download to trigger
		session.Wait()
		globalStdinManager.SetWriter(nil)
		<-stdoutDone

		return false, lastPopup, nil // Exit normally
	}

	if lastErr != nil {
		return false, nil, fmt.Errorf("failed to connect to any candidate: %w", lastErr)
	}
	return false, nil, fmt.Errorf("no candidates available")
}

func loadKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(keyBytes)
}
