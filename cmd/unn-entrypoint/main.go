package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mevdschee/underground-node-network/internal/entrypoint"
)

func main() {
	port := flag.Int("port", 44322, "SSH server port")
	bind := flag.String("bind", "0.0.0.0", "Address to bind to")
	hostKey := flag.String("hostkey", "", "Path to SSH host key")
	usersDir := flag.String("users", "", "Path to users directory (defaults to <hostkey_dir>)")
	flag.Parse()

	// Set default host key path
	if *hostKey == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get home directory: %v", err)
		}
		*hostKey = filepath.Join(homeDir, ".unn", "entrypoint_host_key")

		if err := os.MkdirAll(filepath.Dir(*hostKey), 0700); err != nil {
			log.Fatalf("Failed to create .unn directory: %v", err)
		}
	}

	if *usersDir == "" {
		homeDir, _ := os.UserHomeDir()
		*usersDir = filepath.Join(homeDir, ".unn")
	}
	address := fmt.Sprintf("%s:%d", *bind, *port)
	server, err := entrypoint.NewServer(address, *hostKey, *usersDir)
	if err != nil {
		log.Fatalf("Failed to create entry point: %v", err)
	}

	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start entry point: %v", err)
	}

	log.Printf("UNN Entry Point is online")
	log.Printf("Connect with: ssh -p %d %s", *port, *bind)
	log.Printf("Available subsystems: unn-control (rooms), unn-api (clients), unn-signaling (p2p)")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutting down...")
	server.Stop()
}
