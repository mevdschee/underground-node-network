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

	"github.com/mevdschee/underground-node-network/internal/protocol"
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
			s.usernames[unnName] = platformId
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
