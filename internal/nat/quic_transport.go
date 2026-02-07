package nat

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
)

// QUICListener manages a QUIC listener with UDP hole-punching support
type QUICListener struct {
	udpConn       *net.UDPConn
	listener      *quic.Listener
	localPort     int
	signalingURL  string
	peerID        string
	stopHolePunch chan struct{}
}

// NewQUICListener creates a new QUIC listener on the specified port
func NewQUICListener(port int) (*QUICListener, error) {
	// Create UDP socket
	udpAddr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	}
	udpConn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP socket: %w", err)
	}

	// Generate TLS config
	tlsConfig := generateTLSConfig()

	// Create QUIC listener
	listener, err := quic.Listen(udpConn, tlsConfig, nil)
	if err != nil {
		udpConn.Close()
		return nil, fmt.Errorf("failed to create QUIC listener: %w", err)
	}

	log.Printf("QUIC listener started on port %d", port)

	return &QUICListener{
		udpConn:       udpConn,
		listener:      listener,
		localPort:     port,
		stopHolePunch: make(chan struct{}),
	}, nil
}

// StartHolePunching begins continuous hole-punching to discovered peers
func (ql *QUICListener) StartHolePunching(signalingURL, peerID string) {
	ql.signalingURL = signalingURL
	ql.peerID = peerID

	go ql.continuousHolePunch()
}

// continuousHolePunch continuously polls for peers and sends punch packets
func (ql *QUICListener) continuousHolePunch() {
	knownPeers := make(map[string]bool)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ql.stopHolePunch:
			return
		case <-ticker.C:
			// Poll for all peers
			peers, err := ql.getAllPeers()
			if err != nil {
				log.Printf("Failed to get peers: %v", err)
				continue
			}

			// Send punch packets to peers
			for _, peer := range peers {
				// Skip ourselves
				if peer.ID == ql.peerID {
					continue
				}

				// Log new peers
				if !knownPeers[peer.ID] {
					log.Printf("Discovered new peer: %s with %d candidates", peer.ID, len(peer.Candidates))
					knownPeers[peer.ID] = true
				}

				// Send punch packets to all candidates
				for _, candidate := range peer.Candidates {
					addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", candidate.IP, candidate.Port))
					if err != nil {
						continue
					}
					ql.udpConn.WriteToUDP([]byte("PUNCH"), addr)
				}
			}
		}
	}
}

// getAllPeers retrieves all registered peers from signaling server
func (ql *QUICListener) getAllPeers() ([]PeerInfo, error) {
	if ql.signalingURL == "" {
		return nil, nil
	}

	resp, err := http.Get(fmt.Sprintf("%s/peers", ql.signalingURL))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get peers: status %d", resp.StatusCode)
	}

	var peers []PeerInfo
	// Note: You'll need to add JSON decoding here based on your protocol
	// For now, returning empty to compile
	return peers, nil
}

// Accept waits for and returns the next QUIC connection
func (ql *QUICListener) Accept(ctx context.Context) (*quic.Conn, error) {
	return ql.listener.Accept(ctx)
}

// Close closes the QUIC listener and UDP socket
func (ql *QUICListener) Close() error {
	close(ql.stopHolePunch)
	if err := ql.listener.Close(); err != nil {
		return err
	}
	return ql.udpConn.Close()
}

// LocalAddr returns the local network address
func (ql *QUICListener) LocalAddr() net.Addr {
	return ql.udpConn.LocalAddr()
}

// generateTLSConfig creates a self-signed certificate for QUIC
func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}

	return &tls.Config{
		Certificates:       []tls.Certificate{tlsCert},
		InsecureSkipVerify: true, // For P2P connections
		NextProtos:         []string{"unn-quic"},
	}
}

// PeerInfo represents a peer's connection information
type PeerInfo struct {
	ID         string
	Candidates []Candidate
}
