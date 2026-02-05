package entrypoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"github.com/mevdschee/underground-node-network/internal/ui/form"
	"golang.org/x/crypto/ssh"
)

func (s *Server) calculatePubKeyHash(key ssh.PublicKey) string {
	return protocol.CalculatePubKeyHash(key)
}

func (s *Server) loadUsers() {
	path := filepath.Join(s.usersDir, "users")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// format: hash unn_username platform_username@platform [lastSeenDate]
		parts := strings.SplitN(line, " ", 4)
		if len(parts) >= 3 {
			hash := parts[0]
			unnName := parts[1]
			platformId := parts[2]
			lastSeen := ""
			if len(parts) == 4 {
				lastSeen = parts[3]
			}
			s.identities[hash] = fmt.Sprintf("%s %s %s", unnName, platformId, lastSeen)
			s.usernames[unnName] = hash
		}
	}
}

func (s *Server) saveUsers() error {
	if err := os.MkdirAll(s.usersDir, 0700); err != nil {
		log.Printf("Error creating users directory: %v", err)
		return err
	}
	var buf bytes.Buffer
	for hash, info := range s.identities {
		// info is "unnUsername platform_username@platform lastSeenDate"
		// Ensure we don't have multiple spaces
		fields := strings.Fields(info)
		if len(fields) >= 2 {
			unnUsername := fields[0]
			platformInfo := fields[1]
			lastSeen := ""
			if len(fields) >= 3 {
				lastSeen = fields[2]
			}
			buf.WriteString(fmt.Sprintf("%s %s %s %s\n", hash, unnUsername, platformInfo, lastSeen))
		}
	}
	err := os.WriteFile(filepath.Join(s.usersDir, "users"), buf.Bytes(), 0600)
	if err != nil {
		log.Printf("Error saving users file: %v", err)
	}
	return err
}

func (s *Server) loadRooms() {
	path := filepath.Join(s.usersDir, "rooms")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// format: hostKeyHash roomName owner lastSeenDate
		parts := strings.Split(line, " ")
		if len(parts) == 4 {
			hostHash := parts[0]
			roomName := parts[1]
			owner := parts[2]
			date := parts[3]
			s.registeredRooms[roomName] = fmt.Sprintf("%s %s %s", hostHash, owner, date)
		}
	}
}

func (s *Server) saveRooms() error {
	if err := os.MkdirAll(s.usersDir, 0700); err != nil {
		return err
	}
	var buf bytes.Buffer
	for name, info := range s.registeredRooms {
		// info is "hostKeyHash owner date"
		parts := strings.Split(info, " ")
		if len(parts) == 3 {
			hostHash := parts[0]
			owner := parts[1]
			date := parts[2]
			buf.WriteString(fmt.Sprintf("%s %s %s %s\n", hostHash, name, owner, date))
		}
	}
	err := os.WriteFile(filepath.Join(s.usersDir, "rooms"), buf.Bytes(), 0600)
	if err != nil {
		log.Printf("Error saving rooms file: %v", err)
	}
	return err
}

func (s *Server) VerifyIdentity(platform, username string, offeredKey ssh.PublicKey) (bool, error) {
	url := ""
	switch platform {
	case "github":
		url = fmt.Sprintf("https://github.com/%s.keys", username)
	case "gitlab":
		url = fmt.Sprintf("https://gitlab.com/%s.keys", username)
	case "sourcehut":
		url = fmt.Sprintf("https://meta.sr.ht/~%s.keys", username)
	case "codeberg":
		url = fmt.Sprintf("https://codeberg.org/%s.keys", username)
	default:
		return false, fmt.Errorf("unsupported platform: %s", platform)
	}

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("platform returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}
		if bytes.Equal(pubKey.Marshal(), offeredKey.Marshal()) {
			return true, nil
		}
	}

	return false, nil
}

func (s *Server) handleOnboardingForm(p *Person, conn *ssh.ServerConn) bool {
	eui := p.UI
	sshUser := conn.User()

	fields := []form.FormField{
		{Label: "Platform (github, gitlab, sourcehut, codeberg)", Value: "github"},
		{Label: "Platform Username", Value: ""},
		{Label: "UNN Username", Value: sshUser, MaxLength: 20, Alphanumeric: true},
	}

	// Give a moment for any initial automated input to arrive, then flush it.
	time.Sleep(100 * time.Millisecond)
	p.Bridge.Flush()

	for {
		results := eui.PromptForm(fields)
		if len(results) < 3 {
			return false
		}
		platform := strings.ToLower(strings.TrimSpace(results[0]))
		platformUser := strings.TrimSpace(results[1])
		unnUsername := strings.TrimSpace(results[2])

		fields[0].Value = platform
		fields[1].Value = platformUser
		fields[2].Value = unnUsername

		// Clear errors
		for i := range fields {
			fields[i].Error = ""
		}

		platforms := []string{"github", "gitlab", "sourcehut", "codeberg"}
		validPlatform := false
		for _, v := range platforms {
			if platform == v {
				validPlatform = true
				break
			}
		}
		if !validPlatform {
			fields[0].Error = "unsupported platform"
			continue
		}

		if platformUser == "" {
			fields[1].Error = "cannot be empty"
			continue
		}

		// Length check
		if len(unnUsername) < 4 {
			fields[2].Error = "too short"
			continue
		}

		if !common.IsAlphanumeric(unnUsername) {
			fields[2].Error = "must be alphanumeric"
			continue
		}

		pubKeyStr := conn.Permissions.Extensions["pubkey"]
		offeredKey, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(pubKeyStr))

		matched, err := s.VerifyIdentity(platform, platformUser, offeredKey)
		if err != nil {
			if strings.Contains(err.Error(), "status 404") {
				fields[1].Error = "username not found"
			} else {
				s.showMessage(p, fmt.Sprintf("\033[1;31mError verifying identity: %v\033[0m", err), ui.MsgServer)
			}
			continue
		}

		if matched {
			pubKeyHash := s.calculatePubKeyHash(offeredKey)
			s.mu.RLock()
			ownerHash, taken := s.usernames[unnUsername]
			s.mu.RUnlock()

			if taken && ownerHash != pubKeyHash {
				fields[2].Error = "not available"
				continue
			}

			s.mu.Lock()
			currentDate := time.Now().Format("2006-01-02")
			s.usernames[unnUsername] = pubKeyHash
			s.identities[pubKeyHash] = fmt.Sprintf("%s %s@%s %s", unnUsername, platformUser, platform, currentDate)
			s.saveUsers()
			s.mu.Unlock()

			p.Username = unnUsername
			p.UI.SetUsername(unnUsername)
			conn.Permissions.Extensions["verified"] = "true"
			conn.Permissions.Extensions["platform"] = platform
			conn.Permissions.Extensions["username"] = unnUsername
			return true
		} else {
			fields[1].Error = "key not found"
		}
	}
}

func (s *Server) calculateSHA256Fingerprint(keyStr string) string {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
	if err != nil {
		return "invalid key"
	}
	algo := strings.ToUpper(strings.TrimPrefix(pubKey.Type(), "ssh-"))
	hash := sha256.Sum256(pubKey.Marshal())
	fingerprint := "SHA256:" + base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
	return fmt.Sprintf("%s key fingerprint is %s.", algo, fingerprint)
}

func (s *Server) getPubKeyHash(keyStr string) string {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
	if err != nil {
		return "invalid"
	}
	hash := sha256.Sum256(pubKey.Marshal())
	return fmt.Sprintf("%x", hash)
}

func (s *Server) SendOSC(p *Person, action string, params map[string]interface{}) {
	if !p.UNNAware {
		return
	}
	common.SendOSC(p.Bus, action, params)
}

func (s *Server) HandleOSC(p *Person, action string, params map[string]interface{}) {
	log.Printf("Received OSC from %s: %s %v", p.Username, action, params)
	if action == "join" {
		if room, ok := params["room"].(string); ok {
			p.InitialCommand = room
		}
	}
}

func (s *Server) showTeleportInfo(p *Person) {
	data := p.TeleportData
	if data == nil {
		return
	}

	// Always clear screen before showing teleport info
	// First reset colors to avoid the black background from sticking around
	fmt.Fprint(p.Bus, "\033[m\033[2J\033[H")

	// Emit invisible ANSI OSC 31337 sequence with teleport data
	data.Action = "teleport"
	s.SendOSC(p, "teleport", map[string]interface{}{
		"room_name":   data.RoomName,
		"candidates":  data.Candidates,
		"ssh_port":    data.SSHPort,
		"public_keys": data.PublicKeys,
	})

	fmt.Fprintf(p.Bus, "\033[1;32mUNN TELEPORTATION READY\033[0m\r\n\r\n")
	fmt.Fprintf(p.Bus, "The client is automatically teleporting you to the room.\r\n")
	fmt.Fprintf(p.Bus, "If the client fails, you can connect manually using:\r\n\r\n")

	for _, candidate := range data.Candidates {
		fmt.Fprintf(p.Bus, "\033[1;36mssh -p %d %s\033[0m\r\n", data.SSHPort, candidate)
	}

	fmt.Fprintf(p.Bus, "\r\n\033[1mHost Verification Fingerprints:\033[0m\r\n\r\n")
	for _, key := range data.PublicKeys {
		fingerprint := s.calculateSHA256Fingerprint(key)
		fmt.Fprintf(p.Bus, "\033[1;36m%s\033[0m\r\n", fingerprint)
	}
	fmt.Fprintf(p.Bus, "\r\n")

	// Consume the data so it's not shown again if we disconnect for other reasons
	p.TeleportData = nil
}
