package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// SignalingServer coordinates peer discovery
type SignalingServer struct {
	peers map[string]*PeerInfo
	mu    sync.RWMutex
}

func NewSignalingServer() *SignalingServer {
	return &SignalingServer{
		peers: make(map[string]*PeerInfo),
	}
}

// Register a peer with its candidates
func (s *SignalingServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var peer PeerInfo
	if err := json.NewDecoder(r.Body).Decode(&peer); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	peer.Timestamp = time.Now()

	s.mu.Lock()
	s.peers[peer.ID] = &peer
	s.mu.Unlock()

	log.Printf("Registered peer %s with %d candidates", peer.ID, len(peer.Candidates))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}

// Get peer information
func (s *SignalingServer) handleGetPeer(w http.ResponseWriter, r *http.Request) {
	peerID := r.URL.Query().Get("id")
	if peerID == "" {
		http.Error(w, "Missing peer ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	peer, exists := s.peers[peerID]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Peer not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peer)
}

// List all peers
func (s *SignalingServer) handleListPeers(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	peerList := make([]*PeerInfo, 0, len(s.peers))
	for _, peer := range s.peers {
		peerList = append(peerList, peer)
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peerList)
}

func main() {
	server := NewSignalingServer()

	http.HandleFunc("/register", server.handleRegister)
	http.HandleFunc("/peer", server.handleGetPeer)
	http.HandleFunc("/peers", server.handleListPeers)

	port := ":8080"
	log.Printf("Signaling server listening on %s", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
