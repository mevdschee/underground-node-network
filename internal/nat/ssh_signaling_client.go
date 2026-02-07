package nat

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/mevdschee/p2pquic-go/pkg/p2pquic"
	"golang.org/x/crypto/ssh"
)

// SSHSignalingClient implements signaling over SSH subsystem
type SSHSignalingClient struct {
	sshClient *ssh.Client
	session   *ssh.Session
	stdin     io.WriteCloser
	stdout    io.Reader
	encoder   *json.Encoder
	decoder   *json.Decoder
}

// SignalingMessage types (must match entrypoint/signaling_ssh.go)
const (
	SignalTypeRegister = "register"
	SignalTypeGetPeer  = "get_peer"
	SignalTypeResponse = "response"
	SignalTypeError    = "error"
)

type signalingMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type registerPeerRequest struct {
	PeerID     string              `json:"peer_id"`
	Candidates []p2pquic.Candidate `json:"candidates"`
}

type getPeerRequest struct {
	PeerID string `json:"peer_id"`
}

// NewSSHSignalingClient creates a new SSH-based signaling client
func NewSSHSignalingClient(sshClient *ssh.Client) (*SSHSignalingClient, error) {
	session, err := sshClient.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to get stdin: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to get stdout: %w", err)
	}

	if err := session.RequestSubsystem("unn-signaling"); err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to request unn-signaling subsystem: %w", err)
	}

	client := &SSHSignalingClient{
		sshClient: sshClient,
		session:   session,
		stdin:     stdin,
		stdout:    stdout,
		encoder:   json.NewEncoder(stdin),
		decoder:   json.NewDecoder(stdout),
	}

	return client, nil
}

// Register registers a peer with the signaling server
func (c *SSHSignalingClient) Register(peerID string, candidates []p2pquic.Candidate) error {
	req := registerPeerRequest{
		PeerID:     peerID,
		Candidates: candidates,
	}
	payload, _ := json.Marshal(req)

	msg := signalingMessage{
		Type:    SignalTypeRegister,
		Payload: payload,
	}

	if err := c.encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to send register: %w", err)
	}

	var response signalingMessage
	if err := c.decoder.Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Type == SignalTypeError {
		var errMsg map[string]string
		json.Unmarshal(response.Payload, &errMsg)
		return fmt.Errorf("signaling error: %s", errMsg["message"])
	}

	return nil
}

// GetPeer retrieves peer information
func (c *SSHSignalingClient) GetPeer(peerID string) (*p2pquic.PeerInfo, error) {
	req := getPeerRequest{PeerID: peerID}
	payload, _ := json.Marshal(req)

	msg := signalingMessage{
		Type:    SignalTypeGetPeer,
		Payload: payload,
	}

	if err := c.encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("failed to send get_peer: %w", err)
	}

	var response signalingMessage
	if err := c.decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Type == SignalTypeError {
		var errMsg map[string]string
		json.Unmarshal(response.Payload, &errMsg)
		return nil, fmt.Errorf("signaling error: %s", errMsg["message"])
	}

	var peer p2pquic.PeerInfo
	if err := json.Unmarshal(response.Payload, &peer); err != nil {
		return nil, fmt.Errorf("failed to parse peer info: %w", err)
	}

	return &peer, nil
}

// Close closes the signaling session
func (c *SSHSignalingClient) Close() error {
	if c.session != nil {
		return c.session.Close()
	}
	return nil
}
