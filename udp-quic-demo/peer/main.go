package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
)

// discoverPublicIP uses a STUN-like approach to discover public IP
func discoverPublicIP(localPort int) (*Candidate, error) {
	// Use a simple UDP echo service to discover our public endpoint
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: localPort})
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Send a packet to a public STUN server
	stunAddr, _ := net.ResolveUDPAddr("udp4", "stun.l.google.com:19302")

	// Simple STUN binding request
	stunReq := []byte{
		0x00, 0x01, // Binding Request
		0x00, 0x00, // Length
		0x21, 0x12, 0xa4, 0x42, // Magic Cookie
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Transaction ID
	}

	conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.WriteToUDP(stunReq, stunAddr)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}

	// Parse STUN response (simplified)
	if n > 20 {
		// Look for XOR-MAPPED-ADDRESS attribute (0x0020)
		for i := 20; i < n-8; i++ {
			if buf[i] == 0x00 && buf[i+1] == 0x20 {
				// Found XOR-MAPPED-ADDRESS
				port := int(buf[i+6])<<8 | int(buf[i+7])
				port ^= 0x2112 // XOR with magic cookie

				ip := net.IPv4(
					buf[i+8]^0x21,
					buf[i+9]^0x12,
					buf[i+10]^0xa4,
					buf[i+11]^0x42,
				)

				return &Candidate{
					IP:   ip.String(),
					Port: port,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to parse STUN response")
}

// getLocalCandidates returns local network candidates
func getLocalCandidates(port int) []Candidate {
	candidates := []Candidate{}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return candidates
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				candidates = append(candidates, Candidate{
					IP:   ipnet.IP.String(),
					Port: port,
				})
			}
		}
	}

	return candidates
}

// registerWithSignaling registers this peer with the signaling server
func registerWithSignaling(signalingURL, peerID string, candidates []Candidate) error {
	peer := PeerInfo{
		ID:         peerID,
		Candidates: candidates,
	}

	data, err := json.Marshal(peer)
	if err != nil {
		return err
	}

	resp, err := http.Post(signalingURL+"/register", "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed: %s", body)
	}

	return nil
}

// getPeerInfo retrieves peer information from signaling server
func getPeerInfo(signalingURL, peerID string) (*PeerInfo, error) {
	resp, err := http.Get(fmt.Sprintf("%s/peer?id=%s", signalingURL, peerID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("peer not found")
	}

	var peer PeerInfo
	if err := json.NewDecoder(resp.Body).Decode(&peer); err != nil {
		return nil, err
	}

	return &peer, nil
}

// getAllPeers retrieves all registered peers from signaling server
func getAllPeers(signalingURL string) ([]PeerInfo, error) {
	resp, err := http.Get(fmt.Sprintf("%s/peers", signalingURL))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get peers")
	}

	var peers []PeerInfo
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return nil, err
	}

	return peers, nil
}

// continuousHolePunch continuously polls for peers and sends punch packets
func continuousHolePunch(conn *net.UDPConn, signalingURL, myPeerID string) {
	knownPeers := make(map[string]bool)

	for {
		// Poll for all peers
		peers, err := getAllPeers(signalingURL)
		if err != nil {
			log.Printf("Failed to get peers: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Send punch packets to new peers
		for _, peer := range peers {
			// Skip ourselves
			if peer.ID == myPeerID {
				continue
			}

			// Check if this is a new peer
			if !knownPeers[peer.ID] {
				log.Printf("Discovered new peer: %s with %d candidates", peer.ID, len(peer.Candidates))
				knownPeers[peer.ID] = true
			}

			// Send punch packets to all candidates of this peer
			for _, candidate := range peer.Candidates {
				addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", candidate.IP, candidate.Port))
				if err != nil {
					continue
				}

				conn.WriteToUDP([]byte("PUNCH"), addr)
			}
		}

		// Poll every 5 seconds
		time.Sleep(5 * time.Second)
	}
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
		NotAfter:     time.Now().Add(24 * time.Hour),
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
		InsecureSkipVerify: true, // For demo purposes only
		NextProtos:         []string{"quic-hole-punch-demo"},
	}
}

// udpHolePunch performs UDP hole-punching to remote candidates using an existing connection
func udpHolePunch(conn *net.UDPConn, remoteCandidates []Candidate) error {
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	log.Printf("Starting UDP hole-punch from port %d", localAddr.Port)

	// Send packets to all remote candidates to punch holes
	for _, candidate := range remoteCandidates {
		addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", candidate.IP, candidate.Port))
		if err != nil {
			log.Printf("Failed to resolve %s:%d: %v", candidate.IP, candidate.Port, err)
			continue
		}

		// Send multiple packets to ensure hole is punched
		for i := 0; i < 5; i++ {
			_, err = conn.WriteToUDP([]byte("PUNCH"), addr)
			if err != nil {
				log.Printf("Failed to send punch packet to %s: %v", addr, err)
			} else {
				log.Printf("Sent punch packet to %s", addr)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// startQUICListener starts a QUIC listener on an existing UDP connection
func startQUICListener(conn *net.UDPConn) (*quic.Listener, error) {
	tlsConfig := generateTLSConfig()

	listener, err := quic.Listen(conn, tlsConfig, nil)
	if err != nil {
		return nil, err
	}

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	log.Printf("QUIC listener started on port %d", localAddr.Port)
	return listener, nil
}

// connectQUIC attempts to connect to remote candidates via QUIC using an existing UDP connection
func connectQUIC(conn *net.UDPConn, remoteCandidates []Candidate) (quic.Connection, error) {
	tlsConfig := generateTLSConfig()

	// Try each candidate
	for _, candidate := range remoteCandidates {
		addr := fmt.Sprintf("%s:%d", candidate.IP, candidate.Port)
		log.Printf("Attempting QUIC connection to %s", addr)

		remoteAddr, err := net.ResolveUDPAddr("udp4", addr)
		if err != nil {
			log.Printf("Failed to resolve %s: %v", addr, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		quicConn, err := quic.Dial(ctx, conn, remoteAddr, tlsConfig, nil)
		if err != nil {
			log.Printf("Failed to connect to %s: %v", addr, err)
			continue
		}

		log.Printf("Successfully connected to %s", addr)
		return quicConn, nil
	}

	return nil, fmt.Errorf("failed to connect to any candidate")
}

func main() {
	mode := flag.String("mode", "server", "Mode: server or client")
	peerID := flag.String("id", "peer1", "This peer's ID")
	remotePeerID := flag.String("remote", "", "Remote peer ID (for client mode)")
	signalingURL := flag.String("signaling", "http://localhost:8080", "Signaling server URL")
	port := flag.Int("port", 9000, "Local UDP port")
	flag.Parse()

	log.Printf("Starting in %s mode as %s on port %d", *mode, *peerID, *port)

	// Discover candidates
	log.Println("Discovering NAT candidates...")

	var candidates []Candidate

	// Try STUN discovery
	if stunCand, err := discoverPublicIP(*port); err == nil {
		log.Printf("STUN discovered: %s:%d", stunCand.IP, stunCand.Port)
		candidates = append(candidates, *stunCand)
	} else {
		log.Printf("STUN discovery failed: %v", err)
	}

	// Add local candidates
	localCands := getLocalCandidates(*port)
	candidates = append(candidates, localCands...)

	log.Printf("Total candidates: %d", len(candidates))
	for _, c := range candidates {
		log.Printf("  - %s:%d", c.IP, c.Port)
	}

	// Register with signaling server
	log.Println("Registering with signaling server...")
	if err := registerWithSignaling(*signalingURL, *peerID, candidates); err != nil {
		log.Fatalf("Failed to register: %v", err)
	}
	log.Println("Registration successful")

	if *mode == "server" {
		// Server mode: create UDP socket and listen for incoming QUIC connections
		udpAddr := &net.UDPAddr{
			IP:   net.IPv4zero,
			Port: *port,
		}
		udpConn, err := net.ListenUDP("udp4", udpAddr)
		if err != nil {
			log.Fatalf("Failed to create UDP socket: %v", err)
		}
		defer udpConn.Close()

		listener, err := startQUICListener(udpConn)
		if err != nil {
			log.Fatalf("Failed to start QUIC listener: %v", err)
		}
		defer listener.Close()

		// Start background hole-punching to discovered peers
		go continuousHolePunch(udpConn, *signalingURL, *peerID)

		log.Println("Waiting for incoming connections...")

		for {
			conn, err := listener.Accept(context.Background())
			if err != nil {
				log.Printf("Failed to accept connection: %v", err)
				continue
			}

			log.Printf("Accepted connection from %s", conn.RemoteAddr())

			go func(c quic.Connection) {
				defer c.CloseWithError(0, "done")

				stream, err := c.AcceptStream(context.Background())
				if err != nil {
					log.Printf("Failed to accept stream: %v", err)
					return
				}
				defer stream.Close()

				buf := make([]byte, 1024)
				n, err := stream.Read(buf)
				if err != nil {
					log.Printf("Failed to read: %v", err)
					return
				}

				message := string(buf[:n])
				log.Printf("Received from %s: %s", c.RemoteAddr(), message)

				// Continuously exchange messages
				for {
					// Send response
					response := fmt.Sprintf("Hello from server!")
					_, err := stream.Write([]byte(response))
					if err != nil {
						log.Printf("Failed to write: %v", err)
						return
					}
					log.Printf("Sent to %s: %s", c.RemoteAddr(), response)

					// Wait for next message
					time.Sleep(5 * time.Second)

					n, err := stream.Read(buf)
					if err != nil {
						log.Printf("Connection closed: %v", err)
						return
					}
					log.Printf("Received from %s: %s", c.RemoteAddr(), string(buf[:n]))
				}
			}(conn)
		}
	} else {
		// Client mode: connect to remote peer
		if *remotePeerID == "" {
			log.Fatal("Remote peer ID required in client mode")
		}

		log.Printf("Waiting for remote peer %s to register...", *remotePeerID)

		var remotePeer *PeerInfo
		for i := 0; i < 30; i++ {
			peer, err := getPeerInfo(*signalingURL, *remotePeerID)
			if err == nil {
				remotePeer = peer
				break
			}
			time.Sleep(1 * time.Second)
		}

		if remotePeer == nil {
			log.Fatal("Remote peer not found")
		}

		log.Printf("Found remote peer with %d candidates", len(remotePeer.Candidates))
		for _, c := range remotePeer.Candidates {
			log.Printf("  - %s:%d", c.IP, c.Port)
		}

		// Create UDP socket for hole-punching and QUIC
		udpAddr := &net.UDPAddr{
			IP:   net.IPv4zero,
			Port: *port,
		}
		udpConn, err := net.ListenUDP("udp4", udpAddr)
		if err != nil {
			log.Fatalf("Failed to create UDP socket: %v", err)
		}
		defer udpConn.Close()

		// Perform UDP hole-punching
		log.Println("Performing UDP hole-punch...")
		if err := udpHolePunch(udpConn, remotePeer.Candidates); err != nil {
			log.Printf("Hole-punch error: %v", err)
		}

		// Wait a bit for holes to be established
		time.Sleep(2 * time.Second)

		// Attempt QUIC connection
		log.Println("Attempting QUIC connection...")
		conn, err := connectQUIC(udpConn, remotePeer.Candidates)
		if err != nil {
			log.Fatalf("Failed to establish QUIC connection: %v", err)
		}
		defer conn.CloseWithError(0, "done")

		log.Println("QUIC connection established!")

		// Open a stream and send a message
		stream, err := conn.OpenStreamSync(context.Background())
		if err != nil {
			log.Fatalf("Failed to open stream: %v", err)
		}
		defer stream.Close()

		message := "Hello from UDP hole-punched QUIC connection!"
		log.Printf("Sending: %s", message)
		stream.Write([]byte(message))

		buf := make([]byte, 1024)
		n, err := stream.Read(buf)
		if err != nil {
			log.Fatalf("Failed to read response: %v", err)
		}

		log.Printf("Received response: %s", string(buf[:n]))
		log.Println("Demo completed successfully!")
	}
}
