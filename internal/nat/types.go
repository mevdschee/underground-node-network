package nat

import (
	"fmt"
	"net"
)

// Candidate represents a NAT traversal candidate (IP:Port pair)
type Candidate struct {
	Type string // "host", "srflx", "relay"
	IP   string
	Port int
}

// GetLocalCandidates discovers local interface candidates with the given port
func GetLocalCandidates(port int) []Candidate {
	candidates := make([]Candidate, 0)

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return candidates
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				candidates = append(candidates, Candidate{
					Type: "host",
					IP:   ipnet.IP.String(),
					Port: port,
				})
			}
		}
	}

	return candidates
}

// DiscoverPublicAddress attempts to discover the public IP address using STUN
// Returns nil if discovery fails
func DiscoverPublicAddress(port int) (*Candidate, error) {
	// For now, return nil - p2pquic handles STUN internally
	return nil, fmt.Errorf("STUN not implemented - use p2pquic")
}

// CandidatesToStrings converts candidates to string representations
func CandidatesToStrings(candidates []Candidate) []string {
	strs := make([]string, len(candidates))
	for i, c := range candidates {
		if c.Port > 0 {
			strs[i] = fmt.Sprintf("%s:%d", c.IP, c.Port)
		} else {
			strs[i] = c.IP
		}
	}
	return strs
}
