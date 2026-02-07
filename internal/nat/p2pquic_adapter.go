package nat

import (
	"context"
	"fmt"
	"net"

	"github.com/mevdschee/p2pquic-go/pkg/p2pquic"
	"github.com/quic-go/quic-go"
)

// P2PQUICPeer wraps p2pquic.Peer to provide UNN-compatible interface
type P2PQUICPeer struct {
	peer   *p2pquic.Peer
	config p2pquic.Config
}

// NewP2PQUICPeer creates a new P2P QUIC peer wrapper
func NewP2PQUICPeer(peerID string, localPort int, signalingURL string, enableSTUN bool) (*P2PQUICPeer, error) {
	config := p2pquic.Config{
		PeerID:       peerID,
		LocalPort:    localPort,
		SignalingURL: signalingURL,
		EnableSTUN:   false, // Disabled - using server-reflexive IPv4 from entrypoint
	}

	peer, err := p2pquic.NewPeer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create p2pquic peer: %w", err)
	}

	return &P2PQUICPeer{
		peer:   peer,
		config: config,
	}, nil
}

// DiscoverCandidates discovers NAT traversal candidates using STUN and local interfaces
func (p *P2PQUICPeer) DiscoverCandidates() ([]Candidate, error) {
	p2pCandidates, err := p.peer.DiscoverCandidates()
	if err != nil {
		return nil, err
	}

	// Convert p2pquic.Candidate to nat.Candidate
	candidates := make([]Candidate, len(p2pCandidates))
	for i, c := range p2pCandidates {
		candidates[i] = Candidate{
			Type: "host", // p2pquic doesn't distinguish types
			IP:   c.IP,
			Port: c.Port,
		}
	}

	return candidates, nil
}

// Register registers this peer with the signaling server
func (p *P2PQUICPeer) Register() error {
	return p.peer.Register()
}

// Listen starts listening for incoming QUIC connections
func (p *P2PQUICPeer) Listen() error {
	return p.peer.Listen()
}

// Accept accepts an incoming QUIC connection and returns a stream wrapped as net.Conn
func (p *P2PQUICPeer) Accept(ctx context.Context) (net.Conn, error) {
	quicConn, err := p.peer.Accept(ctx)
	if err != nil {
		return nil, err
	}

	// Accept a stream from the QUIC connection
	stream, err := quicConn.AcceptStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to accept stream: %w", err)
	}

	// Wrap stream as net.Conn for SSH
	return NewQUICStreamConn(stream, quicConn), nil
}

// Connect connects to a remote peer and returns a stream wrapped as net.Conn
func (p *P2PQUICPeer) Connect(remotePeerID string) (net.Conn, error) {
	quicConn, err := p.peer.Connect(remotePeerID)
	if err != nil {
		return nil, err
	}

	// Open a stream on the QUIC connection
	stream, err := quicConn.OpenStreamSync(context.Background())
	if err != nil {
		quicConn.CloseWithError(0, "failed to open stream")
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}

	// Wrap stream as net.Conn for SSH
	return NewQUICStreamConn(stream, quicConn), nil
}

// ConnectQUIC connects to a remote peer and returns the raw QUIC connection
// This allows the caller to manage streams themselves
func (p *P2PQUICPeer) ConnectQUIC(ctx context.Context, remotePeerID string) (*quic.Conn, error) {
	return p.peer.Connect(remotePeerID)
}

// ConnectWithPeerInfo establishes a QUIC connection using pre-fetched peer info
// This bypasses the HTTP signaling server lookup
func (p *P2PQUICPeer) ConnectWithPeerInfo(ctx context.Context, remotePeerID string, candidateAddrs []string) (*quic.Conn, error) {
	return p.peer.ConnectWithCandidates(remotePeerID, candidateAddrs)
}

// ContinuousHolePunch starts continuous hole-punching to discovered peers
func (p *P2PQUICPeer) ContinuousHolePunch(ctx context.Context) {
	p.peer.ContinuousHolePunch(ctx)
}

// Close closes the peer and releases resources
func (p *P2PQUICPeer) Close() error {
	return p.peer.Close()
}

// GetUDPConn returns the underlying UDP connection for manual hole-punching if needed
func (p *P2PQUICPeer) GetUDPConn() *net.UDPConn {
	return p.peer.GetUDPConn()
}

// GetPort returns the local port the peer is listening on
func (p *P2PQUICPeer) GetPort() int {
	return p.config.LocalPort
}

// SetSignalingURL updates the signaling server URL
func (p *P2PQUICPeer) SetSignalingURL(url string) {
	p.config.SignalingURL = url
	// Recreate the signaling client with the new URL
	// We need to access the peer's internal signaling client field
	// Since p2pquic.Peer doesn't expose a setter, we'll use reflection or recreate
	p.peer.UpdateSignalingClient(url)
}

// GetActualPort returns the actual port the peer is listening on from the UDP listener
func (p *P2PQUICPeer) GetActualPort() int {
	return p.peer.GetActualPort()
}

// UpdatePort updates the peer's port configuration
func (p *P2PQUICPeer) UpdatePort(port int) {
	p.config.LocalPort = port
}

// ConvertToP2PQUICCandidates converts nat.Candidate to p2pquic.Candidate
func ConvertToP2PQUICCandidates(candidates []Candidate) []p2pquic.Candidate {
	p2pCandidates := make([]p2pquic.Candidate, len(candidates))
	for i, c := range candidates {
		p2pCandidates[i] = p2pquic.Candidate{
			IP:   c.IP,
			Port: c.Port,
		}
	}
	return p2pCandidates
}

// ConvertFromP2PQUICCandidates converts p2pquic.Candidate to nat.Candidate
func ConvertFromP2PQUICCandidates(p2pCandidates []p2pquic.Candidate) []Candidate {
	candidates := make([]Candidate, len(p2pCandidates))
	for i, c := range p2pCandidates {
		candidates[i] = Candidate{
			Type: "host",
			IP:   c.IP,
			Port: c.Port,
		}
	}
	return candidates
}
