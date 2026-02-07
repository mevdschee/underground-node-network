package main

import "time"

// Candidate represents a NAT traversal candidate
type Candidate struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// PeerInfo stores information about a peer
type PeerInfo struct {
	ID         string      `json:"id"`
	Candidates []Candidate `json:"candidates"`
	Timestamp  time.Time   `json:"timestamp"`
}
