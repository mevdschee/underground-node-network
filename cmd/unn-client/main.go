package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mevdschee/underground-node-network/internal/doors"
	"github.com/mevdschee/underground-node-network/internal/entrypoint"
	"github.com/mevdschee/underground-node-network/internal/nat"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/sshserver"
	"golang.org/x/crypto/ssh"
)

func main() {
	// Parse command-line flags
	port := flag.Int("port", 2222, "SSH server port")
	bind := flag.String("bind", "127.0.0.1", "Address to bind to")
	doorsDir := flag.String("doors", "./doors", "Directory containing door executables")
	roomName := flag.String("room", "anonymous", "Name of your room")
	username := flag.String("user", "", "Username to use for entry point connection (defaults to room name)")
	identity := flag.String("identity", "", "Path to user private key for authentication (defaults to ~/.unn/user_key)")
	hostKey := flag.String("hostkey", "", "Path to SSH host key (auto-generated if not specified)")
	entryPointAddr := flag.String("entrypoint", "", "Entry point address (e.g., localhost:44322)")
	flag.Parse()

	// Set default host key path to ephemeral file
	if *hostKey == "" {
		f, err := os.CreateTemp("", "unn_host_key_*")
		if err != nil {
			log.Fatalf("Failed to create temp host key: %v", err)
		}
		f.Close()
		*hostKey = f.Name()
		os.Remove(*hostKey) // Remove empty file so it gets generated
		defer os.Remove(*hostKey)
	}

	// Initialize door manager
	doorManager := doors.NewManager(*doorsDir)
	if err := doorManager.Scan(); err != nil {
		log.Printf("Warning: Could not scan doors directory: %v", err)
	}

	doorList := doorManager.List()
	if len(doorList) > 0 {
		log.Printf("Found %d doors: %v", len(doorList), doorList)
	} else {
		log.Printf("No doors found in %s", *doorsDir)
	}

	// Create and start SSH server
	address := fmt.Sprintf("%s:%d", *bind, *port)
	server, err := sshserver.NewServer(address, *hostKey, *roomName, doorManager)
	if err != nil {
		log.Fatalf("Failed to create SSH server: %v", err)
	}

	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start SSH server: %v", err)
	}

	// Get actual port (important when port 0 is used for random port)
	actualPort := server.GetPort()

	log.Printf("UNN Room '%s' is now online", *roomName)
	log.Printf("Connect with: ssh -p %d %s", actualPort, *bind)

	// Connect to entry point if specified
	var epClient *entrypoint.Client
	if *entryPointAddr != "" {
		// Determine username
		epUser := *username
		if epUser == "" {
			epUser = *roomName
		}

		// Load identity key
		var signer ssh.Signer

		if *identity != "" {
			// Specific key requested
			var err error
			signer, err = loadKey(*identity)
			if err != nil {
				log.Fatalf("Failed to load identity key %s: %v", *identity, err)
			}
			log.Printf("Using identity: %s", *identity)
		} else {
			// Try standard SSH keys
			homeDir, _ := os.UserHomeDir()
			possibleKeys := []string{
				filepath.Join(homeDir, ".ssh", "id_ed25519"),
				filepath.Join(homeDir, ".ssh", "id_rsa"),
			}

			for _, keyPath := range possibleKeys {
				s, err := loadKey(keyPath)
				if err == nil {
					signer = s
					log.Printf("Using system identity: %s", keyPath)
					break
				}
			}

			if signer == nil {
				log.Fatalf("No SSH identity found. Please specify -identity or ensure ~/.ssh/id_ed25519 or ~/.ssh/id_rsa exists.")
			}
		}

		log.Printf("Connecting to entry point: %s as %s", *entryPointAddr, epUser)
		epClient = entrypoint.NewClient(*entryPointAddr, epUser, signer)
		if err := epClient.Connect(); err != nil {
			log.Printf("Warning: Failed to connect to entry point: %v", err)
		} else {
			// Discover NAT candidates using actual port
			candidates := nat.GetLocalCandidates(actualPort)
			stunCand, err := nat.DiscoverPublicAddress(actualPort) // STUN from actual port
			if err == nil {
				candidates = append([]nat.Candidate{*stunCand}, candidates...)
				log.Printf("STUN discovered: %s:%d", stunCand.IP, stunCand.Port)
			}

			candidateStrs := nat.CandidatesToStrings(candidates)

			// Read public key
			pubKeyPath := *hostKey + ".pub"
			var publicKeys []string
			pubKeyBytes, err := os.ReadFile(pubKeyPath)
			if err != nil {
				log.Printf("Warning: Could not read public key %s: %v", pubKeyPath, err)
			} else {
				publicKeys = []string{string(pubKeyBytes)}
			}

			// Register with entry point
			if err := epClient.Register(*roomName, doorList, actualPort, publicKeys); err != nil {
				log.Printf("Warning: Failed to register with entry point: %v", err)
			} else {
				log.Printf("Registered with entry point as '%s'", *roomName)
			}

			// Listen for messages in background (handles punch_offer and server errors)
			go epClient.ListenForMessages(nil, func(offer protocol.PunchOfferPayload) {
				if offer.VisitorKey != "" {
					pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(offer.VisitorKey))
					if err == nil {
						server.AuthorizeKey(pubKey)
					} else {
						log.Printf("Warning: Failed to parse visitor public key: %v", err)
					}
				}
			}, func(err error) {
				log.Fatalf("Entry point error: %v", err)
			}, actualPort, candidateStrs)
		}
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutting down...")
	if epClient != nil {
		epClient.Close()
	}
	server.Stop()
}

func loadKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(keyBytes)
}

func loadOrGenerateKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err == nil {
		return ssh.ParsePrivateKey(keyBytes)
	}

	log.Printf("Generating new user key at %s", path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// Use ssh-keygen to generate a proper OpenSSH format key
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", path, "-N", "", "-q")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ssh-keygen failed: %w", err)
	}

	keyBytes, err = os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return ssh.ParsePrivateKey(keyBytes)
}
