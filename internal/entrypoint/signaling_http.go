package entrypoint

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/mevdschee/p2pquic-go/pkg/signaling"
)

// StartSignalingHTTPServer starts an HTTP server for p2pquic signaling
func (s *Server) StartSignalingHTTPServer(port int) error {
	mux := http.NewServeMux()

	// Register endpoint - POST /register
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var peer signaling.PeerInfo
		if err := json.NewDecoder(r.Body).Decode(&peer); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := s.signalingServer.Register(peer.ID, peer.Candidates); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("Registered p2pquic peer: %s with %d candidates", peer.ID, len(peer.Candidates))
		w.WriteHeader(http.StatusOK)
	})

	// Get peer endpoint - GET /peer?id=<peerID>
	mux.HandleFunc("/peer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		peerID := r.URL.Query().Get("id")
		if peerID == "" {
			http.Error(w, "Missing peer ID", http.StatusBadRequest)
			return
		}

		peer, exists := s.signalingServer.GetPeer(peerID)
		if !exists {
			http.Error(w, "Peer not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(peer)
	})

	// Get all peers endpoint - GET /peers
	mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		peers := s.signalingServer.GetAllPeers()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(peers)
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Starting p2pquic signaling HTTP server on %s", addr)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("Signaling HTTP server error: %v", err)
		}
	}()

	return nil
}
