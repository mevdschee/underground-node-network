package sshserver

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mevdschee/underground-node-network/internal/doors"
	"github.com/mevdschee/underground-node-network/internal/ui"
	"golang.org/x/crypto/ssh"
)

func TestRoomCommands(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "unn-room-test-*")
	defer os.RemoveAll(tmpDir)

	filesDir := filepath.Join(tmpDir, "files")
	os.MkdirAll(filesDir, 0755)
	os.WriteFile(filepath.Join(filesDir, "test.txt"), []byte("hello"), 0644)

	dm := doors.NewManager(tmpDir)
	hostKeyPath := filepath.Join(tmpDir, "host_key")
	s, err := NewServer("127.0.0.1:0", hostKeyPath, "testroom", filesDir, dm)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	pubAlice, _, _ := ed25519.GenerateKey(rand.Reader)
	sshAlice, _ := ssh.NewPublicKey(pubAlice)

	p := &Person{
		Username: "alice",
		ChatUI:   ui.NewChatUI(nil),
		PubKey:   sshAlice,
	}
	p.ChatUI.SetUsername("alice")

	t.Run("help regular", func(t *testing.T) {
		s.handleInternalCommand(p, "/help")
		msgs := p.ChatUI.GetMessages()
		foundOperator := false
		for _, m := range msgs {
			if strings.Contains(m.Text, "--- Operator Commands ---") {
				foundOperator = true
				break
			}
		}
		if foundOperator {
			t.Errorf("Regular user saw operator commands in help")
		}
	})

	t.Run("help operator", func(t *testing.T) {
		// make alice operator
		s.mu.Lock()
		s.operatorPubKey = p.PubKey
		s.mu.Unlock()

		s.handleInternalCommand(p, "/help")
		msgs := p.ChatUI.GetMessages()
		foundOperator := false
		for _, m := range msgs {
			if strings.Contains(m.Text, "--- Operator Commands ---") {
				foundOperator = true
				break
			}
		}
		if !foundOperator {
			t.Errorf("Operator didn't see operator commands in help")
		}

		// clear operator for other tests if needed
		s.mu.Lock()
		s.operatorPubKey = nil
		s.mu.Unlock()
	})

	t.Run("who", func(t *testing.T) {
		s.mu.Lock()
		s.people["alice"] = p
		s.mu.Unlock()

		s.handleInternalCommand(p, "/who")
		msgs := p.ChatUI.GetMessages()
		found := false
		for _, m := range msgs {
			// Format is "<alice> /who" followed by "--- People in room ---" then "• alice"
			if strings.Contains(m.Text, "• alice") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Who command didn't show alice in %v", msgs)
		}
	})

	t.Run("files", func(t *testing.T) {
		s.handleInternalCommand(p, "/files")
		msgs := p.ChatUI.GetMessages()
		found := false
		for _, m := range msgs {
			if strings.Contains(m.Text, "test.txt") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Files command didn't show test.txt")
		}
	})

	t.Run("download usage", func(t *testing.T) {
		s.handleInternalCommand(p, "/get")
		msgs := p.ChatUI.GetMessages()
		found := false
		for _, m := range msgs {
			if strings.Contains(m.Text, "Usage: /get <filename>") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Download without args didn't show usage")
		}
	})

	t.Run("download success", func(t *testing.T) {
		s.handleInternalCommand(p, "/get test.txt")
		if p.PendingDownload != "test.txt" {
			t.Errorf("Expected PendingDownload to be 'test.txt', got '%s'", p.PendingDownload)
		}
	})

	t.Run("me action", func(t *testing.T) {
		s.handleInternalCommand(p, "/me hacks the Gibson")
		msgs := p.ChatUI.GetMessages()
		found := false
		for _, m := range msgs {
			if strings.Contains(m.Text, "* alice hacks the Gibson") && m.Type == ui.MsgAction {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Action command didn't work as expected")
		}
	})

	t.Run("whisper", func(t *testing.T) {
		pubBob, _, _ := ed25519.GenerateKey(rand.Reader)
		sshBob, _ := ssh.NewPublicKey(pubBob)
		bob := &Person{
			Username: "bob",
			ChatUI:   ui.NewChatUI(nil),
			PubKey:   sshBob,
		}
		s.mu.Lock()
		s.people["bob"] = bob
		s.mu.Unlock()

		s.handleInternalCommand(p, "/whisper bob you are 1337")

		// Check sender saw it
		senderMsgs := p.ChatUI.GetMessages()
		foundSender := false
		for _, m := range senderMsgs {
			if strings.Contains(m.Text, "-> [bob] you are 1337") && m.Type == ui.MsgWhisper {
				foundSender = true
				break
			}
		}
		if !foundSender {
			t.Errorf("Whisper sender didn't see outgoing message")
		}

		// Check receiver saw it
		receiverMsgs := bob.ChatUI.GetMessages()
		foundReceiver := false
		for _, m := range receiverMsgs {
			if strings.Contains(m.Text, "[alice] -> you are 1337") && m.Type == ui.MsgWhisper {
				foundReceiver = true
				break
			}
		}
		if !foundReceiver {
			t.Errorf("Whisper receiver didn't see incoming message")
		}
	})

	t.Run("operator privileges", func(t *testing.T) {
		// alice is NOT operator yet
		s.handleInternalCommand(p, "/lock secret")
		if s.roomLockKey != "" {
			t.Errorf("Room was locked by non-operator")
		}

		// make alice operator
		s.mu.Lock()
		s.operatorPubKey = p.PubKey
		s.mu.Unlock()

		s.handleInternalCommand(p, "/lock secret")
		if s.roomLockKey != "secret" {
			t.Errorf("Room was NOT locked by operator")
		}

		s.handleInternalCommand(p, "/unlock")
		if s.roomLockKey != "" {
			t.Errorf("Room was NOT unlocked by operator")
		}
	})

	t.Run("open door invalid", func(t *testing.T) {
		s.handleInternalCommand(p, "/open non-existent-door")
		msgs := p.ChatUI.GetMessages()
		found := false
		for _, m := range msgs {
			if strings.Contains(m.Text, "Door not found: non-existent-door") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Opening invalid door didn't show error message")
		}
	})

	t.Run("open door valid", func(t *testing.T) {
		// Create a mock executable door
		echoPath := filepath.Join(tmpDir, "echo")
		os.WriteFile(echoPath, []byte("#!/bin/sh\necho hi"), 0755)
		dm.Scan()

		// /open should be rejected by handleInternalCommand (returns false) if door exists
		handled := s.handleInternalCommand(p, "/open echo")
		if handled {
			t.Errorf("handleInternalCommand should return false for valid /open")
		}
	})
}
