package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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

	// Set default host key path
	if *hostKey == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get home directory: %v", err)
		}
		*hostKey = filepath.Join(homeDir, ".unn", "room_host_key")

		if err := os.MkdirAll(filepath.Dir(*hostKey), 0700); err != nil {
			log.Fatalf("Failed to create .unn directory: %v", err)
		}
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

	// Configure p2pquic peer with signaling URL if entrypoint is specified
	if *entryPointAddr != "" {
		// Extract host from entrypoint address for signaling URL
		epHost := strings.Split(*entryPointAddr, ":")[0]
		signalingURL := fmt.Sprintf("http://%s:8080", epHost)

		// Get the p2pquic peer from server and configure it
		p2pPeer := server.GetP2PQUICPeer()
		if p2pPeer != nil {
			// Update signaling URL
			p2pPeer.SetSignalingURL(signalingURL)

			// Discover candidates (STUN + local)
			candidates, err := p2pPeer.DiscoverCandidates()
			if err != nil {
				log.Printf("Warning: Failed to discover candidates: %v", err)
			} else {
				log.Printf("Discovered %d candidates", len(candidates))
			}

			// Register with signaling server
			if err := p2pPeer.Register(); err != nil {
				log.Printf("Warning: Failed to register with signaling server: %v", err)
			} else {
				log.Printf("Registered with p2pquic signaling server at %s", signalingURL)
			}

			// Start continuous hole-punching
			go p2pPeer.ContinuousHolePunch(context.Background())
		}
	}

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

		// Calculate host key hash for registration advice
		var hostKeyHash string
		pubKeyBytes, err := os.ReadFile(*hostKey + ".pub")
		if err == nil {
			hPubKey, _, _, _, err := ssh.ParseAuthorizedKey(pubKeyBytes)
			if err == nil {
				hostKeyHash = protocol.CalculatePubKeyHash(hPubKey)
			}
		}

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
				publicKeys := []string{string(pubKeyBytes)}

				// Register with entry point
				peopleCount := len(server.GetPeople())
				if err := epClient.Register(*roomName, doorList, actualPort, publicKeys, peopleCount); err != nil {
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

				// Listen for messages (this blocks until the connection is lost)
				err = epClient.ListenForMessages(nil, func(offer protocol.PunchOfferPayload) {
					// Authorize the person's key
					if offer.PersonKey != "" {
						pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(offer.PersonKey))
						if err == nil {
							server.AuthorizeKey(pubKey, offer.Username)
						} else {
							log.Printf("Warning: Failed to parse person public key: %v", err)
						}
					}

					// Send UDP punch packets to client's candidates
					if len(offer.Candidates) > 0 {
						log.Printf("Sending UDP punch packets to client %s at %v", offer.Username, offer.Candidates)

						// Get the room's UDP connection from the server
						udpConn := server.GetUDPConn()
						if udpConn != nil {
							for _, candidate := range offer.Candidates {
								addr, err := net.ResolveUDPAddr("udp4", candidate)
								if err != nil {
									log.Printf("Failed to resolve candidate %s: %v", candidate, err)
									continue
								}

								// Send multiple punch packets
								for i := 0; i < 5; i++ {
									udpConn.WriteToUDP([]byte("PUNCH"), addr)
									time.Sleep(100 * time.Millisecond)
								}
								log.Printf("Sent UDP punch packets to %s", candidate)
							}
						} else {
							log.Printf("Warning: No UDP connection available for hole-punching")
						}
					}

					// Send PunchAnswer back to entrypoint with room's candidates
					answer := protocol.PunchAnswerPayload{
						PersonID:   offer.PersonID,
						Candidates: candidateStrs,
						SSHPort:    actualPort,
					}
					if err := epClient.SendPunchAnswer(answer); err != nil {
						log.Printf("Failed to send punch answer: %v", err)
					} else {
						log.Printf("Sent PunchAnswer for person %s", offer.PersonID)
					}
				}, nil, actualPort, candidateStrs)

				// If we reach here, the connection was lost
				if err != nil {
					errMsg := err.Error()
					if strings.Contains(errMsg, "taken") || strings.Contains(errMsg, "Invalid") {
						fmt.Printf("\n\033[1;31mRegistration Error: %s\033[0m\n", errMsg)
						if strings.Contains(errMsg, "taken") {
							fmt.Printf("\033[1mYour Room Host Key Hash is:\033[0m \033[1;36m%s\033[0m\n", hostKeyHash)
							fmt.Printf("If you are the owner, run with \033[1m-identity <your_personal_key>\033[0m to authorize this host key.\n")
							fmt.Printf("Otherwise, please choose a different room name.\n\n")
						}
						os.Exit(1)
					}
				}

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
	// 1. Explicit identity takes precedence
	if identityPath != "" {
		if signer, err := loadKey(identityPath); err == nil {
			return signer
		}
	}

	// 2. Personal SSH keys (Social Identity) preferred over host key
	homeDir, _ := os.UserHomeDir()
	possibleKeys := []string{
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_rsa"),
	}

	for _, path := range possibleKeys {
		if signer, err := loadKey(path); err == nil {
			return signer
		}
	}

	// 3. Last resort: Host Key (Room Identity)
	if hostKey != nil {
		return hostKey
	}

	return nil
}

func loadKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(keyBytes)
}
