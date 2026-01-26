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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
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
		fmt.Fprintf(os.Stderr, "Usage: %s [options] ssh://entrypoint[:port]/roomname\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nTeleport to a UNN room via SSH.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s ssh://localhost:44322/myroom\n", os.Args[0])
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
			var currentLine strings.Builder
			var inData bool
			var lastByte byte
			buf := make([]byte, 1024)
			var yamlBuffer strings.Builder
			for {
				n, err := stdoutPipe.Read(buf)
				if err != nil {
					close(userExited)
					return
				}

				for i := 0; i < n; i++ {
					b := buf[i]

					// RELAY: Always relay every byte to human RAW to avoid corrupting TUI data.
					// The server is responsible for sending \r\n for regular text.
					os.Stdout.Write([]byte{b})

					// ACCUMULATE: Build line buffer for line-based data capture
					if b == '\n' || b == '\r' {
						rawLine := currentLine.String()
						currentLine.Reset()

						// Skip the \n if we just processed \r from a \r\n pair (already handled the line)
						if b == '\n' && lastByte == '\r' {
							lastByte = b
							continue
						}
						lastByte = b

						cleanLine := stripANSI(rawLine)
						line := strings.TrimSpace(cleanLine)

						if line == "[CONNECTION DATA]" {
							inData = true
							yamlBuffer.Reset()
						} else if inData {
							if line == "[/CONNECTION DATA]" {
								yamlStr := yamlBuffer.String()
								if verbose {
									log.Printf("Captured connection data (%d bytes)", len(yamlStr))
								}
								var startPayload protocol.PunchStartPayload
								if err := yaml.Unmarshal([]byte(yamlStr), &startPayload); err == nil {
									candidates = startPayload.Candidates
									sshPort = startPayload.SSHPort
									hostKeys = startPayload.PublicKeys
								} else if verbose {
									log.Printf("YAML unmarshal failed: %v", err)
								}
								inData = false
								close(done)
							} else {
								yamlBuffer.WriteString(strings.TrimRight(cleanLine, "\r") + "\n")
							}
						}
					} else {
						// Non-newline byte: add to line buffer
						currentLine.WriteByte(b)
					}
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

		var downloadFilename string

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
			var currentLine strings.Builder
			var lastByte byte
			buf := make([]byte, 1024)
			for {
				n, err := stdout.Read(buf)
				if err != nil {
					errChan <- err
					return
				}

				for i := 0; i < n; i++ {
					b := buf[i]
					os.Stdout.Write([]byte{b})

					if b == '\n' || b == '\r' {
						rawLine := currentLine.String()
						currentLine.Reset()

						if b == '\n' && lastByte == '\r' {
							lastByte = b
							continue
						}
						lastByte = b

						cleanLine := stripANSI(rawLine)
						line := strings.TrimSpace(cleanLine)

						if strings.HasPrefix(line, "[DOWNLOAD FILE]") && strings.HasSuffix(line, "[/DOWNLOAD FILE]") {
							downloadFilename = strings.TrimSuffix(strings.TrimPrefix(line, "[DOWNLOAD FILE]"), "[/DOWNLOAD FILE]")
						}
					} else {
						currentLine.WriteByte(b)
					}
				}
			}
		}()
		go func() {
			_, err := io.Copy(os.Stderr, stderr)
			errChan <- err
		}()

		// Wait for session to end
		session.Wait()
		globalStdinManager.SetWriter(nil)

		if downloadFilename != "" {
			if verbose {
				log.Printf("Automatic download triggered: %s", downloadFilename)
			}
			if err := downloadFile(client, downloadFilename, verbose); err != nil {
				fmt.Fprintf(os.Stderr, "\r\nError during automatic download: %v\r\n", err)
			}
		}

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

var ansiRegex = regexp.MustCompile(`\x1b(\[([0-9;]*[a-zA-Z])|c|\[\?[0-9]+[hl])`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func downloadFile(client *ssh.Client, filename string, verbose bool) error {
	// 1. Determine destination path (~/Downloads)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	destDir := filepath.Join(home, "Downloads")
	// Make sure it exists
	os.MkdirAll(destDir, 0755)

	destPath := getUniquePath(filepath.Join(destDir, filepath.Base(filename)))

	if verbose {
		log.Printf("Downloading to: %s", destPath)
	}

	fmt.Fprintf(os.Stderr, "\033[1;32mDownloading %s to %s...\033[0m\r\n", filename, destPath)

	// 2. Open session for SCP
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdout, _ := session.StdoutPipe()
	stdin, _ := session.StdinPipe()

	// 3. Run scp in "sink" mode (to receive)
	// scp -v -f <filename> (remote scp -f is used for sending)
	if err := session.Start("scp -f " + filename); err != nil {
		return err
	}

	// 4. Implement SCP sink side (receiving)
	// Send initial null byte to start
	stdin.Write([]byte{0})

	// Read C header
	buf := make([]byte, 1024)
	n, err := stdout.Read(buf)
	if err != nil {
		return err
	}
	header := string(buf[:n])
	if !strings.HasPrefix(header, "C") {
		return fmt.Errorf("unexpected SCP header: %s", header)
	}

	// Parse header: C0644 22 filename\n
	parts := strings.Fields(header)
	if len(parts) < 3 {
		return fmt.Errorf("invalid SCP header: %s", header)
	}
	size, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return err
	}

	// Acknowledge header
	stdin.Write([]byte{0})

	// Open local file
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Receive file data
	if _, err := io.CopyN(f, stdout, size); err != nil {
		return err
	}

	// Read final null byte
	stdout.Read(buf[:1])
	// Send final null byte
	stdin.Write([]byte{0})

	fmt.Fprintf(os.Stderr, "\033[1;32mTransfer complete!\033[0m\r\n")
	return nil
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
