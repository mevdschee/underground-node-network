package entrypoint

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
)

// Client handles communication with an entry point via SSH
type Client struct {
	address    string
	sshConfig  *ssh.ClientConfig
	conn       ssh.Conn
	channel    ssh.Channel
	mu         sync.Mutex
	registered bool
	rooms      []protocol.RoomInfo
}

// NewClient creates a new entry point client
func NewClient(address, username string, signer ssh.Signer) *Client {
	authMethods := []ssh.AuthMethod{}
	if signer != nil {
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	return &Client{
		address: address,
		sshConfig: &ssh.ClientConfig{
			User:            username,
			Auth:            authMethods,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
		rooms: make([]protocol.RoomInfo, 0),
	}
}

// Connect establishes an SSH connection to the entry point
func (c *Client) Connect() error {
	conn, err := net.Dial("tcp", c.address)
	if err != nil {
		return fmt.Errorf("failed to connect to entry point: %w", err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, c.address, c.sshConfig)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SSH handshake failed: %w", err)
	}

	c.conn = sshConn

	// Discard incoming channels and requests
	go ssh.DiscardRequests(reqs)
	go func() {
		for range chans {
		}
	}()

	// Open control channel with subsystem
	channel, reqs, err := sshConn.OpenChannel("session", nil)
	if err != nil {
		sshConn.Close()
		return fmt.Errorf("failed to open session: %w", err)
	}

	go ssh.DiscardRequests(reqs)

	// Request unn-control subsystem
	ok, err := channel.SendRequest("subsystem", true, ssh.Marshal(struct{ Name string }{"unn-control"}))
	if err != nil || !ok {
		channel.Close()
		sshConn.Close()
		return fmt.Errorf("failed to request unn-control subsystem")
	}

	c.channel = channel
	return nil
}

// Close closes the connection to the entry point
func (c *Client) Close() error {
	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	return nil
}

// Register registers this node with the entry point
func (c *Client) Register(roomName string, doors []string, sshPort int, publicKeys []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	candidates := discoverCandidates()

	payload := protocol.RegisterPayload{
		RoomName:   roomName,
		Doors:      doors,
		Candidates: candidates,
		SSHPort:    sshPort,
		PublicKeys: publicKeys,
	}

	msg, err := protocol.NewMessage(protocol.MsgTypeRegister, payload)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(c.channel)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to send register message: %w", err)
	}

	c.registered = true
	return nil
}

// ListenForMessages starts listening for messages from the entry point
func (c *Client) ListenForMessages(onRoomList func([]protocol.RoomInfo), onError func(error), sshPort int, candidates []string) {
	decoder := json.NewDecoder(c.channel)
	encoder := json.NewEncoder(c.channel)

	for {
		var msg protocol.Message
		if err := decoder.Decode(&msg); err != nil {
			return
		}

		switch msg.Type {
		case protocol.MsgTypeRoomList:
			var payload protocol.RoomListPayload
			if err := msg.ParsePayload(&payload); err == nil {
				c.mu.Lock()
				c.rooms = payload.Rooms
				c.mu.Unlock()
				if onRoomList != nil {
					onRoomList(payload.Rooms)
				}
			}

		case protocol.MsgTypeError:
			var payload protocol.ErrorPayload
			if err := msg.ParsePayload(&payload); err == nil {
				if onError != nil {
					onError(fmt.Errorf(payload.Message))
				}
			}

		case protocol.MsgTypePunchOffer:
			// ... (rest of function)
			var offerPayload protocol.PunchOfferPayload
			if err := msg.ParsePayload(&offerPayload); err != nil {
				continue
			}

			// Send punch_answer with our candidates
			answerPayload := protocol.PunchAnswerPayload{
				VisitorID:  offerPayload.VisitorID,
				Candidates: candidates,
				SSHPort:    sshPort,
			}
			answerMsg, _ := protocol.NewMessage(protocol.MsgTypePunchAnswer, answerPayload)
			encoder.Encode(answerMsg)
		}
	}
}

// GetRooms returns the list of active rooms
func (c *Client) GetRooms() []protocol.RoomInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rooms
}

// discoverCandidates finds NAT traversal candidates
func discoverCandidates() []string {
	candidates := make([]string, 0)

	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					candidates = append(candidates, ipnet.IP.String())
				}
			}
		}
	}

	return candidates
}
