package sshserver

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"strings"

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
	common.SendOSC(p.Bus, action, params)
}

func (s *Server) HandleOSC(p *Person, action string, params map[string]interface{}) {
	if action == "transfer_block" {
		return
	}
	log.Printf("Received OSC from %s: %s %v", p.Username, action, params)
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
