package main

import (
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type StdinProxy struct {
	mu     sync.Mutex
	cond   *sync.Cond
	writer io.Writer
	active bool
	paused bool
}

func (p *StdinProxy) SetWriter(w io.Writer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writer = w
}

func (p *StdinProxy) Pause() {
	p.mu.Lock()
	p.paused = true
	p.mu.Unlock()
}

func (p *StdinProxy) Resume() {
	p.mu.Lock()
	p.paused = false
	if p.cond != nil {
		p.cond.Broadcast()
	}
	p.mu.Unlock()
}

func (p *StdinProxy) Start() {
	p.mu.Lock()
	if p.active {
		p.mu.Unlock()
		return
	}
	p.active = true
	p.cond = sync.NewCond(&p.mu)
	p.mu.Unlock()

	go func() {
		buf := make([]byte, 1024)
		for {
			p.mu.Lock()
			for p.paused {
				p.cond.Wait()
			}
			p.mu.Unlock()

			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			p.mu.Lock()
			if p.writer != nil {
				p.writer.Write(buf[:n])
			}
			p.mu.Unlock()
		}
	}()
}

var globalProxy StdinProxy

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
		fd := int(os.Stdin.Fd())
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
		defer signal.Stop(winch)

		go func() {
			for range winch {
				width, height, err := term.GetSize(fd)
				if err == nil {
					session.WindowChange(height, width)
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

		// Set terminal to raw mode for interaction
		oldState, _ := term.MakeRaw(fd)

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

		// Proxy stdin using the global proxy to avoid competition
		globalProxy.Start()
		globalProxy.SetWriter(stdinPipe)

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
			if oldState != nil {
				term.Restore(fd, oldState)
			}
			return fmt.Errorf("timeout waiting for connection info")
		}

		// Restore terminal before closing EP session
		if oldState != nil {
			term.Restore(fd, oldState)
		}

		globalProxy.SetWriter(nil)
		globalProxy.Pause()
		session.Close()
		client.Close()

		if !shouldConnect {
			return nil // User exited manually
		}

		// Connect to room...
		if err := runRoomSSH(candidates, sshPort, hostKeys, verbose); err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				// SSH exited with non-zero status, which is common when the server
				// closes a BBS-style session. We'll treat it as a normal session end.
			} else {
				fmt.Fprintf(os.Stderr, "Error connecting to room: %v\n", err)
				time.Sleep(2 * time.Second)
			}
		}
		globalProxy.Resume()
		// Loop back to entry point
	}
}

func runRoomSSH(candidates []string, sshPort int, hostKeys []string, verbose bool) error {
	if verbose {
		log.Printf("Got connection info: candidates=%v port=%d keys=%d", candidates, sshPort, len(hostKeys))
	}

	// Create temp known_hosts file
	knownHostsFile, err := os.CreateTemp("", "unn_known_hosts")
	if err != nil {
		return fmt.Errorf("failed to create temp known_hosts: %w", err)
	}
	defer os.Remove(knownHostsFile.Name())

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

		conn, err := net.DialTimeout("tcp", target, 2*time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		conn.Close()

		host, port, _ := net.SplitHostPort(target)
		for _, key := range hostKeys {
			parts := strings.Fields(key)
			if len(parts) >= 2 {
				entry := fmt.Sprintf("[%s]:%s %s %s\n", host, port, parts[0], parts[1])
				knownHostsFile.WriteString(entry)
			}
		}
		knownHostsFile.Close()

		finalHost, finalPort, _ := net.SplitHostPort(target)
		sshArgs := []string{
			"-p", finalPort,
			"-o", fmt.Sprintf("UserKnownHostsFile=%s", knownHostsFile.Name()),
			"-o", "StrictHostKeyChecking=yes",
			"-o", "LogLevel=ERROR",
			finalHost,
		}

		cmd := exec.Command("ssh", sshArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		return cmd.Run()
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
