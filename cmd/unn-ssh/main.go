package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

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
	if roomName == "" {
		return fmt.Errorf("no room name specified in URL path")
	}

	if verbose {
		log.Printf("Connecting to entry point: %s@%s", username, entrypoint)
		log.Printf("Target room: %s", roomName)
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

	// Connect to entry point
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", entrypoint, config)
	if err != nil {
		return fmt.Errorf("failed to connect to entry point: %w", err)
	}
	defer client.Close()

	if verbose {
		log.Printf("Connected to entry point")
	}

	// Open a session
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Set up pipes
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	// Request PTY
	if err := session.RequestPty("xterm", 80, 24, ssh.TerminalModes{}); err != nil {
		return fmt.Errorf("failed to request PTY: %w", err)
	}

	// Start shell
	if err := session.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %w", err)
	}

	if verbose {
		log.Printf("Requesting room: %s", roomName)
	}

	// Wait for the prompt and send room name
	scanner := bufio.NewScanner(stdout)
	var candidates []string
	var hostKeys []string
	var sshPort int

	// Read output and look for connection info
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if verbose {
				log.Printf("< %s", line)
			}

			// Look for candidate IPs and port
			if strings.Contains(line, "Candidates:") {
				// Parse candidates from output like "Candidates: [1IP 2IP]"
				re := regexp.MustCompile(`Candidates:\s*\[(.*?)\]`)
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					candidates = strings.Fields(matches[1])
				}
			}

			if strings.Contains(line, "SSH Port:") {
				re := regexp.MustCompile(`SSH Port:\s*(\d+)`)
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					sshPort, _ = strconv.Atoi(matches[1])
				}
			}

			// Look for host keys
			if strings.Contains(line, "Host Keys:") {
				// Parse keys from output like "Host Keys: [key1 key2]"
				re := regexp.MustCompile(`Host Keys:\s*\[(.*?)\]`)
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					// Keys might be space separated, but keys themselves have spaces.
					// However, the standard public key format is "type blob [comment]".
					// Since we printed with %v, it's just spaces.
					// It's safer to just take the whole content and split by "ssh-" if we support multiple types
					// But usually there is one key. Simple space splitting might break if comments have spaces.
					// For now, let's assume standard format and try to reconstruct.
					// The simplest way to handle %v output [a b c] is to take the string content and parse it.
					// Actually, the keys printed are from pub key file which is "type blob comment\n".
					// So it might look messy.
					// Let's iterate and try to find valid key prefixes.
					content := matches[1]

					// Split by known key types
					for _, keyType := range []string{"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256"} {
						parts := strings.Split(content, keyType)
						for i, part := range parts {
							if i == 0 {
								continue
							} // Skip everything before first key
							// part starts after key type. It should start with space then blob.
							// Reconstruct: keyType + part
							fullKey := keyType + part
							// Clean up: stop at next key type or end of string
							for _, nextType := range []string{"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256"} {
								if idx := strings.Index(fullKey, " "+nextType); idx != -1 {
									fullKey = fullKey[:idx]
								}
							}
							hostKeys = append(hostKeys, strings.TrimSpace(fullKey))
						}
					}
					// Fallback: if empty, just try to split by space and take pairs?
					// No, let's treat the whole content as one key if parsing failed, assuming 1 key
					if len(hostKeys) == 0 {
						hostKeys = []string{content}
					}
				}
			}
		}
	}()

	// Give it a moment to display the welcome message
	time.Sleep(500 * time.Millisecond)

	// Send the room name
	if verbose {
		log.Printf("> %s", roomName)
	}
	fmt.Fprintf(stdin, "%s\r\n", roomName)

	// Wait for connection info
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for connection info")
		case <-ticker.C:
			if len(candidates) > 0 && sshPort > 0 {
				// We don't strictly wait for keys, but we should try
				if len(hostKeys) > 0 {
					goto connect
				}
				// If we have candidates but no keys after a while, proceed without keys?
				// No, secure by default. Wait a bit more?
				// The server prints keys immediately after candidates/port.
			}
		}
	}

connect:
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
		target := fmt.Sprintf("%s:%d", candidate, sshPort)
		if verbose {
			log.Printf("Attempting to connect to %s", target)
		}

		// Test if we can reach it
		conn, err := net.DialTimeout("tcp", target, 2*time.Second)
		if err != nil {
			lastErr = err
			if verbose {
				log.Printf("Failed to connect to %s: %v", target, err)
			}
			continue
		}
		conn.Close()

		// Write to known_hosts for this candidate
		// entry format: [ip]:port key-type key-blob
		for _, key := range hostKeys {
			// Extract type and blob (remove comment)
			parts := strings.Fields(key)
			if len(parts) >= 2 {
				entry := fmt.Sprintf("[%s]:%d %s %s\n", candidate, sshPort, parts[0], parts[1])
				knownHostsFile.WriteString(entry)
			}
		}
		knownHostsFile.Close()

		// Success! Exec real SSH client
		if verbose {
			log.Printf("Connection successful! Launching SSH client...")
		}

		sshArgs := []string{
			"-p", strconv.Itoa(sshPort),
			"-o", fmt.Sprintf("UserKnownHostsFile=%s", knownHostsFile.Name()),
			"-o", "StrictHostKeyChecking=yes",
			"-o", "LogLevel=ERROR", // Suppress "permanently added" warnings
			candidate,
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
