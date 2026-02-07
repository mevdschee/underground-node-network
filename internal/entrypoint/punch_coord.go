// P2PQUIC Connection Coordination
//
// This file implements coordinated hole-punching for p2pquic connections.
// When a client wants to connect to a room via p2pquic, it sends a "prepare_punch"
// request to the entrypoint, which then coordinates simultaneous UDP punching from
// both sides to establish the QUIC connection through NAT.

package entrypoint

import (
	"fmt"
	"log"
	"net"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/crypto/ssh"
)

// SendPunchPrepare notifies a room to start hole-punching to a client
func (s *Server) SendPunchPrepare(roomName string, clientPeerID string, clientCandidates []string, conn *ssh.ServerConn) error {
	s.mu.RLock()
	room := s.rooms[roomName]
	s.mu.RUnlock()

	if room == nil {
		return fmt.Errorf("room %s not found", roomName)
	}

	if room.Encoder == nil {
		return fmt.Errorf("room %s has no encoder", roomName)
	}

	// Add server-reflexive candidate (client's public IP as seen by entrypoint)
	candidates := clientCandidates
	if conn != nil {
		remoteAddr := conn.RemoteAddr().String()
		host, _, err := net.SplitHostPort(remoteAddr)
		if err == nil {
			ip := net.ParseIP(host)
			if ip != nil && ip.To4() != nil {
				// Use port 9000 (default QUIC port) for the public candidate
				publicCandidate := fmt.Sprintf("%s:9000", host)

				// Check if we already have this candidate
				hasPublic := false
				for _, c := range candidates {
					if c == publicCandidate || c == fmt.Sprintf("%s:9000", host) {
						hasPublic = true
						break
					}
				}

				// Add public IP as first candidate if not already present
				if !hasPublic {
					candidates = append([]string{publicCandidate}, candidates...)
					log.Printf("Added server-reflexive candidate: %s", publicCandidate)
				}
			}
		}
	}

	// Get client's public key from SSH connection for authorization
	var personKey string
	var username string
	if conn != nil && conn.Permissions != nil {
		if key, ok := conn.Permissions.Extensions["pubkey"]; ok {
			personKey = key
		}
		// Use the SSH username
		username = conn.User()
	}

	// Use the protocol.PunchOfferPayload format that room expects
	payload := protocol.PunchOfferPayload{
		PersonID:    clientPeerID,
		Candidates:  candidates,
		PersonKey:   personKey,
		DisplayName: username,
		Username:    username,
	}

	msg, err := protocol.NewMessage(protocol.MsgTypePunchOffer, payload)
	if err != nil {
		return fmt.Errorf("failed to create punch offer message: %w", err)
	}

	if err := room.Encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to send punch offer to room: %w", err)
	}

	log.Printf("Sent punch_offer to room %s for client %s with %d candidates", roomName, clientPeerID, len(candidates))
	return nil
}
