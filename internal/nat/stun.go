package nat

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// STUN servers to query
var stunServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

// STUN message types
const (
	stunBindingRequest  = 0x0001
	stunBindingResponse = 0x0101
	stunMagicCookie     = 0x2112A442
)

// STUN attribute types
const (
	attrMappedAddress    = 0x0001
	attrXorMappedAddress = 0x0020
)

// Candidate represents a NAT traversal candidate
type Candidate struct {
	Type     string // "host", "srflx" (server reflexive), "relay"
	IP       string
	Port     int
	Priority int
}

// DiscoverPublicAddress uses STUN to discover our public IP:port
func DiscoverPublicAddress(localPort int) (*Candidate, error) {
	// Bind to local port
	localAddr := &net.UDPAddr{Port: localPort}
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind UDP: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Try each STUN server
	for _, server := range stunServers {
		candidate, err := queryStunServer(conn, server)
		if err == nil {
			return candidate, nil
		}
	}

	return nil, fmt.Errorf("all STUN servers failed")
}

func queryStunServer(conn *net.UDPConn, server string) (*Candidate, error) {
	serverAddr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return nil, err
	}

	// Build STUN binding request
	txID := make([]byte, 12)
	for i := range txID {
		txID[i] = byte(i + 1) // Simple transaction ID
	}

	request := buildStunRequest(txID)

	// Send request
	if _, err := conn.WriteToUDP(request, serverAddr); err != nil {
		return nil, err
	}

	// Read response
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}

	return parseStunResponse(buf[:n], txID)
}

func buildStunRequest(txID []byte) []byte {
	msg := make([]byte, 20)

	// Message type: Binding Request
	binary.BigEndian.PutUint16(msg[0:2], stunBindingRequest)
	// Message length: 0 (no attributes)
	binary.BigEndian.PutUint16(msg[2:4], 0)
	// Magic cookie
	binary.BigEndian.PutUint32(msg[4:8], stunMagicCookie)
	// Transaction ID
	copy(msg[8:20], txID)

	return msg
}

func parseStunResponse(data []byte, expectedTxID []byte) (*Candidate, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("response too short")
	}

	// Check message type
	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != stunBindingResponse {
		return nil, fmt.Errorf("unexpected message type: %x", msgType)
	}

	// Check magic cookie
	cookie := binary.BigEndian.Uint32(data[4:8])
	if cookie != stunMagicCookie {
		return nil, fmt.Errorf("invalid magic cookie")
	}

	// Parse attributes
	msgLen := binary.BigEndian.Uint16(data[2:4])
	attrs := data[20 : 20+msgLen]

	for len(attrs) >= 4 {
		attrType := binary.BigEndian.Uint16(attrs[0:2])
		attrLen := binary.BigEndian.Uint16(attrs[2:4])

		if len(attrs) < int(4+attrLen) {
			break
		}

		attrValue := attrs[4 : 4+attrLen]

		switch attrType {
		case attrXorMappedAddress:
			return parseXorMappedAddress(attrValue)
		case attrMappedAddress:
			return parseMappedAddress(attrValue)
		}

		// Move to next attribute (4-byte aligned)
		padding := (4 - (attrLen % 4)) % 4
		attrs = attrs[4+attrLen+padding:]
	}

	return nil, fmt.Errorf("no mapped address in response")
}

func parseXorMappedAddress(data []byte) (*Candidate, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("xor-mapped-address too short")
	}

	family := data[1]
	if family != 0x01 { // IPv4
		return nil, fmt.Errorf("unsupported address family: %d", family)
	}

	// XOR with magic cookie bytes
	magicBytes := []byte{0x21, 0x12, 0xA4, 0x42}
	port := binary.BigEndian.Uint16(data[2:4]) ^ binary.BigEndian.Uint16(magicBytes[0:2])
	ip := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		ip[i] = data[4+i] ^ magicBytes[i]
	}

	return &Candidate{
		Type:     "srflx",
		IP:       ip.String(),
		Port:     int(port),
		Priority: 100,
	}, nil
}

func parseMappedAddress(data []byte) (*Candidate, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("mapped-address too short")
	}

	family := data[1]
	if family != 0x01 { // IPv4
		return nil, fmt.Errorf("unsupported address family: %d", family)
	}

	port := binary.BigEndian.Uint16(data[2:4])
	ip := net.IP(data[4:8])

	return &Candidate{
		Type:     "srflx",
		IP:       ip.String(),
		Port:     int(port),
		Priority: 100,
	}, nil
}

// GetLocalCandidates returns host candidates (local IPs)
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
					Type:     "host",
					IP:       ipnet.IP.String(),
					Port:     port,
					Priority: 50,
				})
			}
		}
	}

	return candidates
}
