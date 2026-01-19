package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mevdschee/underground-node-network/internal/doors"
	"github.com/mevdschee/underground-node-network/internal/entrypoint"
	"github.com/mevdschee/underground-node-network/internal/nat"
	"github.com/mevdschee/underground-node-network/internal/sshserver"
)

func main() {
	// Parse command-line flags
	port := flag.Int("port", 2222, "SSH server port")
	bind := flag.String("bind", "127.0.0.1", "Address to bind to")
	doorsDir := flag.String("doors", "./doors", "Directory containing door executables")
	roomName := flag.String("room", "anonymous", "Name of your room")
	hostKey := flag.String("hostkey", "", "Path to SSH host key (auto-generated if not specified)")
	entryPointAddr := flag.String("entrypoint", "", "Entry point address (e.g., localhost:44322)")
	flag.Parse()

	// Set default host key path
	if *hostKey == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get home directory: %v", err)
		}
		*hostKey = filepath.Join(homeDir, ".unn", "host_key")

		// Ensure .unn directory exists
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

	// Get actual port (important when port 0 is used for random port)
	actualPort := server.GetPort()

	log.Printf("UNN Room '%s' is now online", *roomName)
	log.Printf("Connect with: ssh -p %d %s", actualPort, *bind)

	// Connect to entry point if specified
	var epClient *entrypoint.Client
	if *entryPointAddr != "" {
		log.Printf("Connecting to entry point: %s", *entryPointAddr)
		epClient = entrypoint.NewClient(*entryPointAddr, *roomName)
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

			// Register with entry point
			if err := epClient.Register(*roomName, doorList, actualPort); err != nil {
				log.Printf("Warning: Failed to register with entry point: %v", err)
			} else {
				log.Printf("Registered with entry point as '%s'", *roomName)
			}

			// Listen for messages in background (handles punch_offer)
			go epClient.ListenForMessages(nil, actualPort, candidateStrs)
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
