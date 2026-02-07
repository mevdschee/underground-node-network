package entrypoint

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
)

// Client handles communication with an entry point via SSH subsystem
// This is used by room servers to register with the entrypoint
type Client struct {
	address   string
	sshConfig *ssh.ClientConfig
	sshClient *ssh.Client
	channel   ssh.Channel
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
			ClientVersion:   "SSH-2.0-UNN-ROOM",
		},
	}
}

// Connect establishes an SSH connection to the entry point
func (c *Client) Connect() error {
	// Resolve address and force IPv4
	host, port, err := net.SplitHostPort(c.address)
	if err != nil {
		return fmt.Errorf("invalid address format: %w", err)
	}

	// Resolve to IPv4 only
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve %s: %w", host, err)
	}

	// Find first IPv4 address
	var ipv4Addr string
	for _, ip := range ips {
		if ip.To4() != nil {
			ipv4Addr = ip.String()
			break
		}
	}

	if ipv4Addr == "" {
		return fmt.Errorf("no IPv4 address found for %s", host)
	}

	// Connect via TCP SSH using IPv4
	ipv4Address := net.JoinHostPort(ipv4Addr, port)
	sshClient, err := ssh.Dial("tcp", ipv4Address, c.sshConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to entrypoint: %w", err)
	}
	c.sshClient = sshClient

	// Open control channel with subsystem
	channel, reqs, err := sshClient.OpenChannel("session", nil)
	if err != nil {
		sshClient.Close()
		return fmt.Errorf("failed to open session: %w", err)
	}

	go ssh.DiscardRequests(reqs)

	// Request unn-control subsystem
	ok, err := channel.SendRequest("subsystem", true, ssh.Marshal(struct{ Name string }{"unn-control"}))
	if err != nil || !ok {
		channel.Close()
		sshClient.Close()
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
	if c.sshClient != nil {
		c.sshClient.Close()
	}
	return nil
}

// Connection returns the underlying SSH client for use with subsystems
func (c *Client) Connection() *ssh.Client {
	return c.sshClient
}

// Register registers this room with the entry point
func (c *Client) Register(roomName string, doors []string, sshPort int, publicKeys []string, peopleCount int) error {
	// Discover candidates
	candidates := discoverCandidates()

	payload := protocol.RegisterPayload{
		RoomName:    roomName,
		Doors:       doors,
		Candidates:  candidates,
		SSHPort:     sshPort,
		PublicKeys:  publicKeys,
		PeopleCount: peopleCount,
	}

	msg, err := protocol.NewMessage(protocol.MsgTypeRegister, payload)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(c.channel)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to send register message: %w", err)
	}

	return nil
}

// ListenForMessages starts listening for messages from the entry point
func (c *Client) ListenForMessages(onRoomList func([]protocol.RoomInfo), onPunchOffer func(protocol.PunchOfferPayload), onError func(error), sshPort int, candidates []string) error {
	decoder := json.NewDecoder(c.channel)
	encoder := json.NewEncoder(c.channel)

	for {
		var msg protocol.Message
		if err := decoder.Decode(&msg); err != nil {
			if onError != nil {
				onError(err)
			}
			return err
		}

		switch msg.Type {
		case protocol.MsgTypeRoomList:
			var payload protocol.RoomListPayload
			if err := msg.ParsePayload(&payload); err == nil {
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
			var offerPayload protocol.PunchOfferPayload
			if err := msg.ParsePayload(&offerPayload); err != nil {
				continue
			}

			if onPunchOffer != nil {
				onPunchOffer(offerPayload)
			}

			// Send punch_answer with our candidates
			answerPayload := protocol.PunchAnswerPayload{
				PersonID:   offerPayload.PersonID,
				Candidates: candidates,
				SSHPort:    sshPort,
			}
			answerMsg, _ := protocol.NewMessage(protocol.MsgTypePunchAnswer, answerPayload)
			encoder.Encode(answerMsg)
		}
	}
}

// SendPunchAnswer sends a punch answer back to the entry point
func (c *Client) SendPunchAnswer(answer protocol.PunchAnswerPayload) error {
	encoder := json.NewEncoder(c.channel)
	msg, err := protocol.NewMessage(protocol.MsgTypePunchAnswer, answer)
	if err != nil {
		return err
	}
	return encoder.Encode(msg)
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
