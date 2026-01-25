package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type StdinManager struct {
	mu     sync.Mutex
	writer io.Writer
	active bool
}

func (m *StdinManager) SetWriter(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writer = w
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
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			m.mu.Lock()
			if m.writer != nil {
				m.writer.Write(buf[:n])
			}
			m.mu.Unlock()
		}
	}()
}

var globalStdinManager StdinManager

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] ssh://[user@]entrypoint[:port]/roomname\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nTeleport to a UNN room via SSH.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s ssh://localhost:44322/myroom\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s ssh://alice@unn.example.com/hackerspace\n", os.Args[0])
	}

	verbose := flag.Bool("v", false, "Verbose output")
	identity := flag.String("identity", "", "Path to private key for authentication")
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	sshURL := flag.Arg(0)
	// Ignore SIGINT so it's passed as a byte to the SSH sessions
	signal.Ignore(os.Interrupt)
	if err := teleport(sshURL, *identity, *verbose); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func teleport(sshURL string, identPath string, verbose bool) error {
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
	}

	// Set terminal to raw mode for the entire duration
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err == nil {
		defer term.Restore(fd, oldState)
	}

	// Loop for persistent entry point session
	currentRoom := roomName
	for {
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
			candidates []string
			hostKeys   []string
			sshPort    int
			done       = make(chan bool)
			userExited = make(chan bool)
		)

		// Monitor output for connection data and echo to user
		go func() {
			var currentLine strings.Builder
			var inData bool
			buf := make([]byte, 1024)
			for {
				n, err := stdoutPipe.Read(buf)
				if err != nil {
					close(userExited)
					return
				}

				for i := 0; i < n; i++ {
					b := buf[i]

					if b == '\n' || b == '\r' {
						line := strings.TrimSpace(currentLine.String())

						if line == "[CONNECTION_DATA]" {
							inData = true
							currentLine.Reset()
							continue
						}
						if inData {
							if line == "[/CONNECTION_DATA]" {
								close(done)
								return
							}
							if strings.HasPrefix(line, "Candidates: ") {
								val := strings.TrimPrefix(line, "Candidates: ")
								candidates = strings.Split(val, ",")
								for i := range candidates {
									candidates[i] = strings.TrimSpace(candidates[i])
								}
							} else if strings.HasPrefix(line, "SSHPort: ") {
								val := strings.TrimPrefix(line, "SSHPort: ")
								sshPort, _ = strconv.Atoi(strings.TrimSpace(val))
							} else if strings.HasPrefix(line, "HostKey: ") {
								key := strings.TrimPrefix(line, "HostKey: ")
								hostKeys = append(hostKeys, strings.TrimSpace(key))
							}
							currentLine.Reset()
							continue
						}

						// Print accumulated character
						os.Stdout.Write([]byte{b})
						// Convert \n to \r\n for raw terminal if needed
						if b == '\n' && !strings.HasSuffix(currentLine.String(), "\r") {
							os.Stdout.Write([]byte{'\r'})
						}
						currentLine.Reset()
					} else {
						currentLine.WriteByte(b)
						if !inData {
							os.Stdout.Write([]byte{b})
						}
					}
				}
			}
		}()

		// Proxy stdin using the global manager
		globalStdinManager.Start()
		globalStdinManager.SetWriter(stdinPipe)

		// If room name was provided, send it immediately
		if currentRoom != "" {
			fmt.Fprintf(stdinPipe, "%s\r\n", currentRoom)
			currentRoom = "" // Only do it once per URL invocation
		}

		// Wait for connection info or manual exit
		var shouldConnect bool
		select {
		case <-done:
			shouldConnect = true
			// Give it a tiny moment to finish printing any trailing data
			time.Sleep(100 * time.Millisecond)
		case <-userExited:
			shouldConnect = false
		case <-time.After(5 * time.Minute):
			return fmt.Errorf("timeout waiting for connection info")
		}

		close(stopWinch)
		signal.Stop(winch)

		globalStdinManager.SetWriter(nil)
		session.Close()
		client.Close()

		if !shouldConnect {
			return nil // User exited manually
		}

		// Connect to room using native SSH client
		if err := runRoomSSH(candidates, sshPort, hostKeys, config, verbose); err != nil {
			fmt.Fprintf(os.Stderr, "Error connecting to room: %v\n", err)
			time.Sleep(2 * time.Second)
		}
		// Loop back to entry point
	}
}

func runRoomSSH(candidates []string, sshPort int, hostKeys []string, entrypointConfig *ssh.ClientConfig, verbose bool) error {
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

		session, err := client.NewSession()
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}
		defer session.Close()

		// Request PTY
		fd := int(os.Stdin.Fd())
		width, height, err := term.GetSize(fd)
		if err != nil {
			width, height = 80, 24
		}

		if err := session.RequestPty("xterm", height, width, ssh.TerminalModes{}); err != nil {
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
		defer func() {
			close(stopWinch)
			signal.Stop(winch)
		}()

		stdin, _ := session.StdinPipe()
		stdout, _ := session.StdoutPipe()
		stderr, _ := session.StderrPipe()

		if err := session.Shell(); err != nil {
			return fmt.Errorf("failed to start shell: %w", err)
		}

		// Proxy I/O
		globalStdinManager.SetWriter(stdin)
		errChan := make(chan error, 2)
		go func() {
			_, err := io.Copy(os.Stdout, stdout)
			errChan <- err
		}()
		go func() {
			_, err := io.Copy(os.Stderr, stderr)
			errChan <- err
		}()

		// Wait for session to end
		session.Wait()
		globalStdinManager.SetWriter(nil)
		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("failed to connect to any candidate: %w", lastErr)
	}
	return fmt.Errorf("no candidates available")
}

func loadKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(keyBytes)
}
