package sshserver

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
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

func (s *Server) addCommandToHistory(pubHash string, cmd string) {
	history := s.cmdHistories[pubHash]
	// Avoid duplicate consecutive commands
	if len(history) > 0 && history[len(history)-1] == cmd {
		return
	}
	history = append(history, cmd)
	if len(history) > 100 {
		history = history[1:]
	}
	s.cmdHistories[pubHash] = history
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
	if action == "transfer_block" {
		return
	}
	log.Printf("Received OSC from %s: %s %v", p.Username, action, params)
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
}

func (s *Server) sendOSCFileBlocks(p *Person, filePath string, filename string) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(p.Bus, "\033[1;31mError opening file: %v\033[0m\r\n", err)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		fmt.Fprintf(p.Bus, "\033[1;31mError stating file: %v\033[0m\r\n", err)
		return
	}

	const blockSize = 8192
	count := int((info.Size() + blockSize - 1) / blockSize)
	buf := make([]byte, blockSize)

	transferID := uuid.New().String()
	checksum := s.calculateFileSHA256(filePath)

	s.mu.RLock()
	limit := s.uploadLimit
	s.mu.RUnlock()

	var blockDelay time.Duration
	if limit > 0 {
		blockDelay = time.Duration(float64(blockSize) / float64(limit) * float64(time.Second))
	}

	for i := 0; i < count; i++ {
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			fmt.Fprintf(p.Bus, "\033[1;31rError reading file: %v\033[0m\r\n", err)
			return
		}

		payload := protocol.FileBlockPayload{
			Action:   "transfer_block",
			Filename: filename,
			ID:       transferID,
			Count:    count,
			Index:    i,
			Checksum: checksum,
			Data:     base64.StdEncoding.EncodeToString(buf[:n]),
		}

		s.SendOSC(p, "transfer_block", map[string]interface{}{
			"filename": payload.Filename,
			"id":       payload.ID,
			"count":    payload.Count,
			"index":    payload.Index,
			"checksum": payload.Checksum,
			"data":     payload.Data,
		})

		if blockDelay > 0 {
			time.Sleep(blockDelay)
		}
	}
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

	filename = cleanTarget

	// Always clear screen before showing download info
	// First reset colors to avoid the black background from sticking around
	fmt.Fprint(p.Bus, "\033[m\033[2J\033[H")

	fmt.Fprintf(p.Bus, "\033[1;32mUNN DOWNLOAD STARTING (OSC)\033[0m\r\n\r\n")
	fmt.Fprintf(p.Bus, "The client is downloading '%s' via OSC blocks.\r\n", filename)

	s.sendOSCFileBlocks(p, path, filename)

	fmt.Fprintf(p.Bus, "\r\n\033[1;32mDOWNLOAD COMPLETE\033[0m\r\n")
	time.Sleep(1 * time.Second)

	// Consume the data
	p.PendingDownload = ""
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
