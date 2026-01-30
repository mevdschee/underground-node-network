package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type StdinManager struct {
	mu     sync.Mutex
	writer io.Writer
	active bool
	paused bool
}

func (m *StdinManager) SetWriter(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writer = w
}

func (m *StdinManager) Pause() {
	m.mu.Lock()
	m.paused = true
	m.mu.Unlock()
}

func (m *StdinManager) Resume() {
	m.mu.Lock()
	m.paused = false
	m.mu.Unlock()
}

func (m *StdinManager) Start() {
	m.mu.Lock()
	if m.active {
		m.mu.Unlock()
		return
	}
	m.active = true
	m.mu.Unlock()

	go func() {
		buf := make([]byte, 1024)
		fd := int(os.Stdin.Fd())
		for {
			m.mu.Lock()
			paused := m.paused
			m.mu.Unlock()

			if paused {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Use syscall.Select to wait for input without blocking forever
			// This allows us to check the 'paused' flag frequently.
			readfds := &syscall.FdSet{}
			readfds.Bits[fd/64] |= 1 << (uint(fd) % 64)
			timeout := &syscall.Timeval{Sec: 0, Usec: 100000} // 100ms

			n, err := syscall.Select(fd+1, readfds, nil, nil, timeout)
			if err != nil && err != syscall.EINTR {
				return
			}
			if n > 0 {
				n, err := os.Stdin.Read(buf)
				if err != nil {
					return
				}
				m.mu.Lock()
				if m.writer != nil && !m.paused {
					m.writer.Write(buf[:n])
				}
				m.mu.Unlock()
			}
		}
	}()
}

var globalStdinManager StdinManager

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] ssh://entrypoint[:port]/roomname\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nTeleport to a UNN room via SSH.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s ssh://localhost:44322/myroom\n", os.Args[0])
	}

	verbose := flag.Bool("v", false, "Verbose output")
	identity := flag.String("identity", "", "Path to private key for authentication")
	batch := flag.Bool("batch", false, "Non-interactive batch mode")
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	sshURL := flag.Arg(0)
	// Ignore SIGINT so it's passed as a byte to the SSH sessions
	signal.Ignore(os.Interrupt)
	if err := teleport(sshURL, *identity, *verbose, *batch); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func teleport(sshURL string, identPath string, verbose bool, batch bool) error {
	// Parse the SSH URL
	u, err := url.Parse(sshURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "ssh" {
		return fmt.Errorf("URL must use ssh:// scheme")
	}

	// Extract components
	entrypoint := u.Host
	if entrypoint == "" {
		return fmt.Errorf("no entrypoint hostname specified")
	}

	// Default port if not specified
	if !strings.Contains(entrypoint, ":") {
		entrypoint += ":22"
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
		ClientVersion:   "SSH-2.0-UNN-SSH",
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

		// Connect to entry point
		client, err := ssh.Dial("tcp", entrypoint, config)
		if err != nil {
			return fmt.Errorf("failed to connect to entry point: %w", err)
		}

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

		// Start shell
		if err := session.Shell(); err != nil {
			session.Close()
			client.Close()
			return fmt.Errorf("failed to start shell: %w", err)
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

					// OSC 9 Detection: \x1b]9; ... \x07
					if b == 0x1b && !inOSC {
						// Peek for ]9;
						if i+3 < n && string(buf[i+1:i+4]) == "]9;" {
							inOSC = true
							oscBuffer.Reset()
							i += 3
							continue
						}
					}

					if inOSC {
						if b == 0x07 {
							inOSC = false
							jsonData := oscBuffer.String()
							if verbose {
								log.Printf("Captured OSC 9 data (%d bytes)", len(jsonData))
							}

							var startPayload protocol.PunchStartPayload
							if err := json.Unmarshal([]byte(jsonData), &startPayload); err == nil {
								if startPayload.Action == "reconnect" {
									candidates = startPayload.Candidates
									sshPort = startPayload.SSHPort
									hostKeys = startPayload.PublicKeys
									close(done)
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
			time.Sleep(500 * time.Millisecond) // Small delay to allow server to be ready
			fmt.Fprintf(stdinPipe, "%s\r\n", currentRoom)
			currentRoom = "" // Only do it once per URL invocation
		}

		// Wait for connection info or manual exit
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
			downloaded, err := runRoomSSH(candidates, sshPort, hostKeys, config, identPath, verbose, oldState, batch)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error connecting to room: %v\n", err)
				time.Sleep(2 * time.Second)
				break
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

func runRoomSSH(candidates []string, sshPort int, hostKeys []string, entrypointConfig *ssh.ClientConfig, identPath string, verbose bool, normalState *term.State, batch bool) (bool, error) {
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

		client, err := ssh.Dial("tcp", target, config)
		if err != nil {
			lastErr = err
			continue
		}
		defer client.Close()

		if verbose {
			log.Printf("Connected to room at %s", target)
		}

		session, err := client.NewSession()
		if err != nil {
			return false, fmt.Errorf("failed to create session: %w", err)
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
			return false, fmt.Errorf("failed to request pty: %w", err)
		}

		if err := session.Shell(); err != nil {
			return false, fmt.Errorf("failed to start shell: %w", err)
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

		type downloadInfo struct {
			port  int
			tid   string
			sig   string
			fname string
		}

		// Track download state in the session
		var (
			stdoutDone = make(chan struct{})
			triggerDl  = make(chan downloadInfo, 1)
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

					// OSC 9 Detection: \x1b]9; ... \x07
					if b == 0x1b && !inOSC {
						// Peek for ]9;
						if i+3 < n && string(buf[i+1:i+4]) == "]9;" {
							inOSC = true
							oscBuffer.Reset()
							i += 3
							continue
						}
					}

					if inOSC {
						if b == 0x07 {
							inOSC = false
							jsonData := oscBuffer.String()
							if verbose {
								log.Printf("Captured OSC 9 data (%d bytes)", len(jsonData))
							}

							var downloadPayload protocol.DownloadPayload
							if err := json.Unmarshal([]byte(jsonData), &downloadPayload); err == nil {
								if downloadPayload.Action == "download" {
									triggerDl <- downloadInfo{
										port:  downloadPayload.Port,
										tid:   downloadPayload.TransferID,
										sig:   downloadPayload.Signature,
										fname: downloadPayload.Filename,
									}
									session.Close()
									return
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

		go func() {
			io.Copy(os.Stderr, stderr)
		}()

		// Wait for session to end OR for a download to trigger
		session.Wait()
		globalStdinManager.SetWriter(nil)
		<-stdoutDone

		// Check if we exited because of a download
		select {
		case info := <-triggerDl:
			err := downloadFile(client, info.port, info.tid, config, info.fname, info.sig, identPath, verbose, normalState, batch)
			if err != nil && verbose {
				log.Printf("Download tool exited with error: %v", err)
			}

			if verbose {
				log.Printf("Waiting for reconnection to room...")
			}
			time.Sleep(500 * time.Millisecond) // Give server time to clean up previous session
			return true, nil                   // Reconnect to room
		default:
			return false, nil // Exit normally
		}
	}

	if lastErr != nil {
		return false, fmt.Errorf("failed to connect to any candidate: %w", lastErr)
	}
	return false, fmt.Errorf("no candidates available")
}

func loadKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(keyBytes)
}

var ansiRegex = regexp.MustCompile(`\x1b(\[([0-9;]*[a-zA-Z])|c|\[\?[0-9]+[hl])`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func downloadFile(client *ssh.Client, remotePort int, transferID string, config *ssh.ClientConfig, filename string, expectedSig string, identPath string, verbose bool, normalState *term.State, batch bool) error {
	// 1. Pause the global stdin manager to yield terminal to unn-dl
	if !batch {
		globalStdinManager.Pause()
		defer globalStdinManager.Resume()
	}

	// 2. Setup local tunnel listener on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start local tunnel: %w", err)
	}
	defer listener.Close()
	localPort := listener.Addr().(*net.TCPAddr).Port

	if verbose {
		log.Printf("Starting local tunnel proxy on port %d -> remote %d", localPort, remotePort)
	}

	// 3. Start background proxy
	go func() {
		for {
			lconn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Dial the remote one-shot server via the SSH client
				rconn, err := client.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(remotePort)))
				if err != nil {
					if verbose {
						log.Printf("Tunnel dial failed: %v", err)
					}
					return
				}
				defer rconn.Close()

				errc := make(chan error, 2)
				go func() {
					_, err := io.Copy(rconn, c)
					errc <- err
				}()
				go func() {
					_, err := io.Copy(c, rconn)
					errc <- err
				}()
				<-errc
			}(lconn)
		}
	}()

	// 4. Restore normal terminal state before running TUI
	fd := int(os.Stdin.Fd())
	if normalState != nil && !batch {
		term.Restore(fd, normalState)
	}

	args := []string{
		"-port", strconv.Itoa(localPort),
		"-id", transferID,
		"-file", filename,
		"-sig", expectedSig,
		"-identity", identPath,
	}
	if batch {
		args = append(args, "-batch")
	}

	cmd := exec.Command("./unn-dl-bin", args...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	downloadErr := cmd.Run()

	// 5. Re-enter raw mode after TUI exit
	if normalState != nil && !batch {
		term.MakeRaw(fd)
	}

	return downloadErr
}

func getUniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}

	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	counter := 1
	for {
		newPath := fmt.Sprintf("%s (%d)%s", base, counter, ext)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
		counter++
	}
}
