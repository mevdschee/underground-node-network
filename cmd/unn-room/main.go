package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	hostKey := flag.String("hostkey", "", "Path to SSH host key (auto-generated if not specified)")
	entryPointAddr := flag.String("entrypoint", "", "Entry point address (e.g., localhost:44322)")
	identity := flag.String("identity", "", "Path to private key for entrypoint registration")
	roomFiles := flag.String("files", "", "Directory containing files for download")
	headless := flag.Bool("headless", false, "Disable TUI (headless mode)")
	flag.Parse()

	// Handle room files symlink
	if *roomFiles != "" {
		absFiles, err := filepath.Abs(*roomFiles)
		if err == nil {
			os.Remove("./room_files") // Remove existing if any
			os.Symlink(absFiles, "./room_files")
		}
	}

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
	server.SetHeadless(*headless)

	// Get actual port (important when port 0 is used for random port)
	actualPort := server.GetPort()

	log.Printf("UNN Room '%s' is now online", *roomName)
	log.Printf("Connect with: ssh -p %d %s", actualPort, *bind)

	// Connect to entry point if specified
	var epClient *entrypoint.Client
	if *entryPointAddr != "" {
		// Determine entrypoint connection username (matches client logic)
		epUser := os.Getenv("USER")
		if epUser == "" {
			epUser = "visitor"
		}

		signer := findPragmaticSigner(server.GetHostKey(), *identity)

		log.Printf("Connecting to entry point: %s as %s", *entryPointAddr, epUser)

		go func() {
			backoff := 1 * time.Second
			maxBackoff := 256 * time.Second

			for {
				epClient = entrypoint.NewClient(*entryPointAddr, epUser, signer)
				if err := epClient.Connect(); err != nil {
					log.Printf("Failed to connect to entry point: %v. Reconnecting in %v...", err, backoff)
					time.Sleep(backoff)
					backoff *= 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					continue
				}

				// Reset backoff on successful connection
				backoff = 1 * time.Second

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
				if err := epClient.Register(*roomName, doorList, actualPort, publicKeys, 0); err != nil {
					log.Printf("Failed to register with entry point: %v. Reconnecting...", err)
					epClient.Close()
					time.Sleep(1 * time.Second)
					continue
				}

				log.Printf("Registered with entry point as '%s'", *roomName)

				// Report people count updates
				server.OnPeopleChange = func(count int) {
					if epClient != nil {
						epClient.Register(*roomName, doorList, actualPort, publicKeys, count)
					}
				}

				// Initial update
				server.OnPeopleChange(len(server.GetPeople()))

				// Listen for messages (this blocks until the connection is lost)
				err = epClient.ListenForMessages(nil, func(offer protocol.PunchOfferPayload) {
					if offer.PersonKey != "" {
						pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(offer.PersonKey))
						if err == nil {
							server.AuthorizeKey(pubKey, offer.Username)
						} else {
							log.Printf("Warning: Failed to parse person public key: %v", err)
						}
					}
				}, nil, actualPort, candidateStrs)

				// If we reach here, the connection was lost
				log.Printf("Entry point connection broken: %v. Reconnecting in %v...", err, backoff)
				epClient.Close()
				time.Sleep(backoff)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
		}()
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

func findPragmaticSigner(hostKey ssh.Signer, identityPath string) ssh.Signer {
	if identityPath != "" {
		if signer, err := loadKey(identityPath); err == nil {
			return signer
		}
	}
	homeDir, _ := os.UserHomeDir()
	possibleKeys := []string{
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_rsa"),
		filepath.Join(homeDir, ".unn", "user_key"),
	}

	for _, path := range possibleKeys {
		if signer, err := loadKey(path); err == nil {
			return signer
		}
	}

	return hostKey
}

func loadKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(keyBytes)
}
