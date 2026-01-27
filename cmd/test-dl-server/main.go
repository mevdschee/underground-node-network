package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func main() {
	// 1. Identify directory to serve
	serveDir, _ := os.Getwd()
	if len(os.Args) > 1 {
		serveDir = os.Args[1]
	}
	fmt.Printf("Mock SFTP server serving directory: %s\n", serveDir)

	// 2. Setup SSH Server
	config := &ssh.ServerConfig{
		NoClientAuth: true,
	}

	// Host key - try to find one
	var signer ssh.Signer
	keyPaths := []string{
		"tests/integration/test_user_key",
		"internal/sshserver/test_key", // fallback
	}
	for _, p := range keyPaths {
		keyBytes, err := os.ReadFile(p)
		if err == nil {
			signer, _ = ssh.ParsePrivateKey(keyBytes)
			break
		}
	}
	if signer == nil {
		log.Fatal("Could not find a host key for the test server. Please ensure tests/integration/test_user_key exists.")
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:44323")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	fmt.Println("Test server listening on 127.0.0.1:44323")

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		go func(c net.Conn) {
			_, chans, reqs, err := ssh.NewServerConn(c, config)
			if err != nil {
				return
			}
			go ssh.DiscardRequests(reqs)

			for newChannel := range chans {
				if newChannel.ChannelType() != "session" {
					newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
					continue
				}
				channel, requests, _ := newChannel.Accept()

				go func(in <-chan *ssh.Request) {
					for req := range in {
						if req.Type == "subsystem" && string(req.Payload[4:]) == "sftp" {
							req.Reply(true, nil)

							// Scoping to serveDir for the test server
							// Note: This is a global change for the process, which is fine for this test utility
							os.Chdir(serveDir)

							server, _ := sftp.NewServer(channel)
							if err := server.Serve(); err != io.EOF {
								log.Printf("SFTP server exited with error: %v", err)
							}
							return
						}
						req.Reply(false, nil)
					}
				}(requests)
			}
		}(conn)
	}
}
