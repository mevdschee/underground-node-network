package nat

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// PunchResult contains the result of a hole-punch attempt
type PunchResult struct {
	Success    bool
	Conn       net.Conn
	LocalAddr  string
	RemoteAddr string
	Error      error
}

// TCPHolePunch attempts TCP hole-punching using simultaneous open
// Both sides should call this at approximately the same time
func TCPHolePunch(localPort int, remoteCandidates []Candidate, timeout time.Duration) (*PunchResult, error) {
	// Bind to local port with SO_REUSEADDR
	laddr := &net.TCPAddr{Port: localPort}

	var wg sync.WaitGroup
	resultChan := make(chan *PunchResult, len(remoteCandidates)*2)

	// Start a listener for incoming connections
	wg.Add(1)
	go func() {
		defer wg.Done()
		listener, err := net.ListenTCP("tcp4", laddr)
		if err != nil {
			log.Printf("Failed to listen: %v", err)
			return
		}
		defer listener.Close()

		listener.SetDeadline(time.Now().Add(timeout))

		for {
			conn, err := listener.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					return
				}
				return
			}

			resultChan <- &PunchResult{
				Success:    true,
				Conn:       conn,
				LocalAddr:  conn.LocalAddr().String(),
				RemoteAddr: conn.RemoteAddr().String(),
			}
			return
		}
	}()

	// Try connecting to each remote candidate
	for _, candidate := range remoteCandidates {
		wg.Add(1)
		go func(c Candidate) {
			defer wg.Done()

			raddr := fmt.Sprintf("%s:%d", c.IP, c.Port)
			dialer := &net.Dialer{
				LocalAddr: laddr,
				Timeout:   timeout,
				Control:   setSocketOptions,
			}

			// Retry connecting multiple times (simultaneous open needs timing)
			for i := 0; i < 5; i++ {
				conn, err := dialer.Dial("tcp4", raddr)
				if err == nil {
					resultChan <- &PunchResult{
						Success:    true,
						Conn:       conn,
						LocalAddr:  conn.LocalAddr().String(),
						RemoteAddr: conn.RemoteAddr().String(),
					}
					return
				}

				// Brief pause between attempts
				time.Sleep(200 * time.Millisecond)
			}
		}(candidate)
	}

	// Wait for either a successful connection or timeout
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Return first successful result
	select {
	case result := <-resultChan:
		if result != nil && result.Success {
			return result, nil
		}
	case <-time.After(timeout):
	}

	return nil, fmt.Errorf("hole-punch failed: no connection established")
}

// Puncher manages hole-punching for a connection
type Puncher struct {
	LocalPort      int
	STUNCandidate  *Candidate
	HostCandidates []Candidate
}

// NewPuncher creates a new hole-puncher
func NewPuncher(localPort int) (*Puncher, error) {
	p := &Puncher{
		LocalPort: localPort,
	}

	// Discover STUN candidate
	stunCand, err := DiscoverPublicAddress(localPort)
	if err != nil {
		log.Printf("STUN discovery failed: %v", err)
	} else {
		p.STUNCandidate = stunCand
		log.Printf("STUN discovered: %s:%d", stunCand.IP, stunCand.Port)
	}

	// Get host candidates
	p.HostCandidates = GetLocalCandidates(localPort)

	return p, nil
}

// GetAllCandidates returns all candidates (STUN + host)
func (p *Puncher) GetAllCandidates() []Candidate {
	candidates := make([]Candidate, 0)

	if p.STUNCandidate != nil {
		candidates = append(candidates, *p.STUNCandidate)
	}

	candidates = append(candidates, p.HostCandidates...)

	return candidates
}

// Punch attempts to establish a connection to the peer
func (p *Puncher) Punch(remoteCandidates []Candidate, timeout time.Duration) (net.Conn, error) {
	result, err := TCPHolePunch(p.LocalPort, remoteCandidates, timeout)
	if err != nil {
		return nil, err
	}
	return result.Conn, nil
}

// CandidatesToStrings converts candidates to string format for protocol
func CandidatesToStrings(candidates []Candidate) []string {
	strs := make([]string, len(candidates))
	for i, c := range candidates {
		strs[i] = fmt.Sprintf("%s:%s:%d", c.Type, c.IP, c.Port)
	}
	return strs
}

// StringsToCandidates parses candidate strings from protocol
func StringsToCandidates(strs []string) []Candidate {
	candidates := make([]Candidate, 0, len(strs))
	for _, s := range strs {
		var c Candidate
		_, err := fmt.Sscanf(s, "%[^:]:%[^:]:%d", &c.Type, &c.IP, &c.Port)
		if err == nil {
			candidates = append(candidates, c)
		}
	}
	return candidates
}
