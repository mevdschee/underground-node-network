package sshserver

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/mevdschee/underground-node-network/internal/fileserver"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"golang.org/x/crypto/ssh"
)

// Broadcast sends a message to all connected people and stores it in their histories
func (s *Server) Broadcast(sender, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	chatMsg := fmt.Sprintf("<%s> %s", sender, message)

	for _, p := range s.people {
		msgType := ui.MsgChat
		if p.Username == sender {
			msgType = ui.MsgSelf
		}

		// Add to UI if available
		if p.ChatUI != nil {
			p.ChatUI.AddMessage(chatMsg, msgType)
		}

		// Add to history (Security: only because they are connected now)
		pubHash := s.getPubKeyHash(p.PubKey)
		s.addMessageToHistory(pubHash, ui.Message{Text: chatMsg, Type: msgType})
	}
}

func (s *Server) broadcastWithHistory(senderPubKey ssh.PublicKey, chatMsg string, msgType ui.MessageType) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.people {
		actualType := msgType
		if msgType == ui.MsgChat && p.PubKey != nil && senderPubKey != nil && string(p.PubKey.Marshal()) == string(senderPubKey.Marshal()) {
			actualType = ui.MsgSelf
		}

		if p.ChatUI != nil {
			p.ChatUI.AddMessage(chatMsg, actualType)
		}
		pubHash := s.getPubKeyHash(p.PubKey)
		s.addMessageToHistory(pubHash, ui.Message{Text: chatMsg, Type: actualType})
	}
}

func (s *Server) isOperator(pubKey ssh.PublicKey) bool {
	if pubKey == nil || s.operatorPubKey == nil {
		return false
	}
	return string(pubKey.Marshal()) == string(s.operatorPubKey.Marshal())
}

func (s *Server) getPubKeyHash(pubKey ssh.PublicKey) string {
	if pubKey == nil {
		return "anonymous"
	}
	hash := sha256.Sum256(pubKey.Marshal())
	return fmt.Sprintf("%x", hash)
}

func (s *Server) addMessageToHistory(pubHash string, msg ui.Message) {
	history := s.histories[pubHash]
	history = append(history, msg)
	if len(history) > 200 {
		history = history[1:]
	}
	s.histories[pubHash] = history
}

func (s *Server) handleRoomSubsystem(channel ssh.Channel, sessionID string) {
	// Not implemented, but reserved for future Room-to-Room signaling
	defer channel.Close()
}

func (s *Server) SendOSC(p *Person, action string, params map[string]interface{}) {
	if !p.UNNAware {
		return
	}
	common.SendOSC(p.Bus, action, params)
}

func (s *Server) HandleOSC(p *Person, action string, params map[string]interface{}) {
	log.Printf("Received OSC from %s: %s %v", p.Username, action, params)
	// Handle OSC messages from client or doors if needed
}

func (s *Server) showFiles(chatUI *ui.ChatUI) {
	s.mu.RLock()
	// Identify current person to log to history
	var pubHash string
	for _, p := range s.people {
		if p.ChatUI == chatUI {
			pubHash = s.getPubKeyHash(p.PubKey)
			break
		}
	}
	s.mu.RUnlock()

	addMessage := func(text string, msgType ui.MessageType) {
		chatUI.AddMessage(text, msgType)
		if pubHash != "" {
			s.mu.Lock()
			s.addMessageToHistory(pubHash, ui.Message{Text: text, Type: msgType})
			s.mu.Unlock()
		}
	}

	addMessage("--- Available Files ---", ui.MsgServer)
	found := false

	err := filepath.WalkDir(s.filesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		var rel string
		rel, err = filepath.Rel(s.filesDir, path)
		if err != nil {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		found = true
		size := formatSize(info.Size())
		modTime := info.ModTime().Format("2006-01-02 15:04")
		addMessage(fmt.Sprintf(" %-24s %10s  %s", rel, size, modTime), ui.MsgServer)
		return nil
	})

	if err != nil {
		addMessage(fmt.Sprintf("\033[1;31mError listing files: %v\033[0m", err), ui.MsgServer)
		return
	}

	if !found {
		addMessage("No files available.", ui.MsgServer)
	}
	addMessage("-----------------------", ui.MsgServer)
}

func (s *Server) showDownloadInfo(p *Person, filename string) {
	if filename == "" {
		return
	}

	// Sanitize and prevent path traversal
	cleanTarget := filepath.Clean(filename)
	path := filepath.Join(s.filesDir, cleanTarget)

	// Ensure the path is within s.filesDir
	absBase, _ := filepath.Abs(s.filesDir)
	absPath, _ := filepath.Abs(path)
	if !strings.HasPrefix(absPath, absBase) {
		fmt.Fprintf(p.Bus, "\033[1;31mAccess denied: %s\033[0m\r\n", filename)
		return
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(p.Bus, "\033[1;31mFile not found: %s\033[0m\r\n", filename)
		return
	}

	filename = cleanTarget

	transferID := uuid.New().String()

	// Start one-shot server
	s.mu.RLock()
	dt := s.downloadTimeout
	s.mu.RUnlock()

	filePort, err := fileserver.StartOneShot(fileserver.Options{
		HostKey:     s.hostKey,
		ClientKey:   p.PubKey,
		Filename:    filename,
		BaseDir:     s.filesDir,
		TransferID:  transferID,
		Timeout:     dt,
		UploadLimit: s.uploadLimit,
	})
	if err != nil {
		fmt.Fprintf(p.Bus, "\033[1;31mFailed to start download server: %v\033[0m\r\n", err)
		return
	}

	// Always clear screen before showing download info
	// First reset colors to avoid the black background from sticking around
	fmt.Fprint(p.Bus, "\033[m\033[2J\033[H")

	// Calculate file signature early to include it in the client's download block
	sig := s.calculateFileSHA256(absPath)

	data := protocol.DownloadPayload{
		Filename:   filename,
		Port:       filePort,
		TransferID: transferID,
		Signature:  sig,
	}

	// Emit invisible ANSI OSC 9 sequence with download data
	data.Action = "download"
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(p.Bus, "\033]9;%s\007", string(jsonData))

	fmt.Fprintf(p.Bus, "\033[1;32mUNN DOWNLOAD READY\033[0m\r\n\r\n")
	fmt.Fprintf(p.Bus, "The client is automatically downloading the file to your Downloads folder.\r\n")
	fmt.Fprintf(p.Bus, " \r\n")
	fmt.Fprintf(p.Bus, "\033[1;33mNote: The transfer must start within %d seconds.\033[0m\r\n", int(dt.Seconds()))

	fmt.Fprintf(p.Bus, "If the client fails, you can download manually using:\r\n\r\n")

	// Get actual address for manual instruction
	host, _, _ := net.SplitHostPort(s.address)
	if host == "" || host == "0.0.0.0" || host == "127.0.0.1" || host == "::" {
		host = "localhost"
	}

	fmt.Fprintf(p.Bus, "  \033[1;36mscp -P %d %s:%s ~/Downloads/%s\033[0m\r\n\r\n", filePort, host, transferID, filepath.Base(filename))

	// Display the host key fingerprint so the user can verify it
	fingerprint := s.calculateHostKeyFingerprint()
	fmt.Fprintf(p.Bus, "\033[1mHost Verification Fingerprint (tunnel):\033[0m\r\n")
	fmt.Fprintf(p.Bus, "\033[1;36m%s\033[0m\r\n\r\n", fingerprint)

	// Display the file signature (matching entrypoint style)
	fmt.Fprintf(p.Bus, "\033[1mFile Verification Signature:\033[0m\r\n")
	fmt.Fprintf(p.Bus, "\033[1;36m%s  %s\033[0m\r\n\r\n", sig, filename)

	fmt.Fprintf(p.Bus, "Disconnecting to allow the transfer...\r\n")

	// Consume the data
	p.PendingDownload = ""

	// Give the client documentation and time to start the download.
	// The connection will stay open until the person disconnects.
}

func (s *Server) calculateFileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "error"
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "error"
	}
	hash := h.Sum(nil)
	fingerprint := fmt.Sprintf("%x", hash)
	return fingerprint
}

func (s *Server) calculateHostKeyFingerprint() string {
	pubKey := s.hostKey.PublicKey()
	algo := strings.ToUpper(strings.TrimPrefix(pubKey.Type(), "ssh-"))
	hash := sha256.Sum256(pubKey.Marshal())
	fingerprint := "SHA256:" + base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
	return fmt.Sprintf("%s key fingerprint is %s.", algo, fingerprint)
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
