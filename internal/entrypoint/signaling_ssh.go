package entrypoint

import (
	"encoding/json"
	"io"
	"log"
	"net"

	"github.com/mevdschee/p2pquic-go/pkg/p2pquic"
	"golang.org/x/crypto/ssh"
)

// Signaling Message Types for unn-signaling subsystem
const (
	SignalTypeRegister = "register"
	SignalTypeGetPeer  = "get_peer"
	SignalTypeResponse = "response"
	SignalTypeError    = "error"
)

// SignalingMessage is the envelope for all signaling subsystem messages
type SignalingMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RegisterPeerRequest registers a p2pquic peer with candidates
type RegisterPeerRequest struct {
	PeerID     string              `json:"peer_id"`
	Candidates []p2pquic.Candidate `json:"candidates"`
}

// GetPeerRequest queries for a peer's candidates
type GetPeerRequest struct {
	PeerID string `json:"peer_id"`
}

// handleSignaling processes the unn-signaling SSH subsystem
// This handles p2pquic peer registration and candidate exchange
func (s *Server) handleSignaling(channel ssh.Channel, conn *ssh.ServerConn) {
	defer channel.Close()

	decoder := json.NewDecoder(channel)
	encoder := json.NewEncoder(channel)

	log.Printf("Signaling subsystem connection from %s", conn.User())

	for {
		var msg SignalingMessage
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				log.Printf("Signaling decode error: %v", err)
			}
			return
		}

		switch msg.Type {
		case SignalTypeRegister:
			var req RegisterPeerRequest
			if err := json.Unmarshal(msg.Payload, &req); err != nil {
				s.sendSignalingError(encoder, "invalid register payload")
				continue
			}
			s.handleSignalingRegister(encoder, conn, req)

		case SignalTypeGetPeer:
			var req GetPeerRequest
			if err := json.Unmarshal(msg.Payload, &req); err != nil {
				s.sendSignalingError(encoder, "invalid get_peer payload")
				continue
			}
			s.handleSignalingGetPeer(encoder, req)

		default:
			s.sendSignalingError(encoder, "unknown message type: "+msg.Type)
		}
	}
}

// handleSignalingRegister registers a p2pquic peer with its candidates
func (s *Server) handleSignalingRegister(encoder *json.Encoder, conn *ssh.ServerConn, req RegisterPeerRequest) {
	// Add server-reflexive candidate (peer's public IP as seen by entrypoint)
	remoteAddr := conn.RemoteAddr().String()
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && len(req.Candidates) > 0 {
		// Parse as IP to check if it's IPv4
		ip := net.ParseIP(host)
		if ip != nil && ip.To4() != nil {
			// Use the same port as the first candidate (should be the QUIC port)
			publicCandidate := p2pquic.Candidate{
				IP:   host,
				Port: req.Candidates[0].Port,
			}

			// Check if we already have this candidate
			hasPublic := false
			for _, c := range req.Candidates {
				if c.IP == host {
					hasPublic = true
					break
				}
			}

			// Add public IP as first candidate if not already present
			if !hasPublic {
				req.Candidates = append([]p2pquic.Candidate{publicCandidate}, req.Candidates...)
				log.Printf("Added server-reflexive IPv4 candidate: %s:%d", host, publicCandidate.Port)
			}
		} else {
			log.Printf("Skipping non-IPv4 server-reflexive address: %s", host)
		}
	}

	if err := s.signalingServer.Register(req.PeerID, req.Candidates); err != nil {
		s.sendSignalingError(encoder, err.Error())
		return
	}

	log.Printf("Registered p2pquic peer via SSH: %s with %d candidates", req.PeerID, len(req.Candidates))

	encoder.Encode(SignalingMessage{
		Type: SignalTypeResponse,
		Payload: mustMarshal(map[string]string{
			"status":  "registered",
			"peer_id": req.PeerID,
		}),
	})
}

// handleSignalingGetPeer retrieves peer information by ID
func (s *Server) handleSignalingGetPeer(encoder *json.Encoder, req GetPeerRequest) {
	peer, exists := s.signalingServer.GetPeer(req.PeerID)
	if !exists {
		s.sendSignalingError(encoder, "peer not found: "+req.PeerID)
		return
	}

	payload, _ := json.Marshal(peer)
	encoder.Encode(SignalingMessage{
		Type:    SignalTypeResponse,
		Payload: payload,
	})
}

func (s *Server) sendSignalingError(encoder *json.Encoder, message string) {
	payload, _ := json.Marshal(map[string]string{"message": message})
	encoder.Encode(SignalingMessage{
		Type:    SignalTypeError,
		Payload: payload,
	})
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
