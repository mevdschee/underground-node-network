package fileserver

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Options configures the one-shot file server
type Options struct {
	HostKey    ssh.Signer
	ClientKey  ssh.PublicKey
	Filename   string
	BaseDir    string
	TransferID string
	Timeout    time.Duration
}

// StartOneShot starts a single-connection SSH server that serves one file via SFTP
func StartOneShot(opts Options) (int, error) {
	// 1. Listen on a random port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port

	// 2. Configure mutual auth
	config := &ssh.ServerConfig{
		NoClientAuth: false,
	}

	marshaledAuthKey := opts.ClientKey.Marshal()
	config.PublicKeyCallback = func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
		if string(pubKey.Marshal()) == string(marshaledAuthKey) {
			return nil, nil
		}
		log.Printf("One-shot SFTP: unauthorized key from %s", c.RemoteAddr())
		return nil, fmt.Errorf("unauthorized")
	}
	config.AddHostKey(opts.HostKey)

	// 3. Handle the one connection with timeout
	go func() {
		defer ln.Close()

		// Set the configured timer to close the listener if no connection arrives
		timeout := time.AfterFunc(opts.Timeout, func() {
			ln.Close()
		})
		defer timeout.Stop()

		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Strictly one-shot: close the listener immediately after the first connection
		ln.Close()

		sconn, chans, reqs, err := ssh.NewServerConn(conn, config)
		if err != nil {
			return
		}
		defer sconn.Close()

		go ssh.DiscardRequests(reqs)

		// Wait for the SFTP session to finish
		finished := make(chan struct{})

		go func() {
			for newChan := range chans {
				if newChan.ChannelType() != "session" {
					newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
					continue
				}
				ch, requests, err := newChan.Accept()
				if err != nil {
					continue
				}

				go func(in <-chan *ssh.Request) {
					for req := range in {
						if req.Type == "subsystem" && string(req.Payload[4:]) == "sftp" {
							req.Reply(true, nil)
							handleSFTP(ch, opts)
							// Signal clean exit to client
							ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
							ch.Close()
							close(finished)
							return
						}
						req.Reply(false, nil)
					}
				}(requests)
			}
		}()

		// Block until the session is finished or timeout
		select {
		case <-finished:
			// Graceful teardown: wait a moment for the client to receive the exit-status and close packets
			time.Sleep(100 * time.Millisecond)
		case <-time.After(opts.Timeout):
		}
		sconn.Close()
	}()

	return port, nil
}

func handleSFTP(channel ssh.Channel, opts Options) {
	handler := &Handler{
		BaseDir:    opts.BaseDir,
		Filename:   opts.Filename,
		TransferID: opts.TransferID,
	}
	handlers := sftp.Handlers{
		FileGet:  handler,
		FilePut:  handler,
		FileCmd:  handler,
		FileList: handler,
	}

	server := sftp.NewRequestServer(channel, handlers)
	if err := server.Serve(); err != nil {
		if err != io.EOF {
			log.Printf("One-shot SFTP server error: %v", err)
		}
	}
}
