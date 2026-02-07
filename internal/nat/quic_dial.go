package nat

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/quic-go/quic-go"
)

// DialQUIC establishes a QUIC connection and returns a stream wrapped as net.Conn
// This is the client-side equivalent of Accept for QUIC connections
func DialQUIC(address string) (net.Conn, error) {
	// Parse address
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", address, err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return nil, fmt.Errorf("invalid port in address %s: %w", address, err)
	}

	// Resolve UDP address
	udpAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	// Create UDP connection
	udpConn, err := net.ListenUDP("udp4", nil) // Random local port
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP socket: %w", err)
	}

	return dialQUICWithConn(udpConn, udpAddr, address)
}

// DialQUICWithUDP establishes a QUIC connection using an existing UDP connection
// This is used when UDP hole-punching has already been performed
func DialQUICWithUDP(udpConn *net.UDPConn, address string) (net.Conn, error) {
	// Parse address
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", address, err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return nil, fmt.Errorf("invalid port in address %s: %w", address, err)
	}

	// Resolve UDP address
	udpAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	return dialQUICWithConn(udpConn, udpAddr, address)
}

func dialQUICWithConn(udpConn *net.UDPConn, udpAddr *net.UDPAddr, address string) (net.Conn, error) {
	// TLS config for client
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // For P2P connections
		NextProtos:         []string{"unn-quic"},
	}

	// QUIC config with keep-alive
	quicConfig := &quic.Config{
		KeepAlivePeriod:         15 * time.Second, // Send keep-alive every 15s
		MaxIdleTimeout:          60 * time.Second, // Timeout after 60s of inactivity
		EnableDatagrams:         false,
		DisablePathMTUDiscovery: false,
	}

	// Dial QUIC connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	quicConn, err := quic.Dial(ctx, udpConn, udpAddr, tlsConfig, quicConfig)
	if err != nil {
		udpConn.Close()
		return nil, fmt.Errorf("failed to dial QUIC: %w", err)
	}

	// Open a stream
	stream, err := quicConn.OpenStreamSync(ctx)
	if err != nil {
		quicConn.CloseWithError(0, "failed to open stream")
		udpConn.Close()
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}

	// Wrap as net.Conn
	return NewQUICStreamConn(stream, quicConn), nil
}
