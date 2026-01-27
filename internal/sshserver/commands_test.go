package sshserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mevdschee/underground-node-network/internal/doors"
	"github.com/mevdschee/underground-node-network/internal/ui"
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

	v := &Visitor{
		Username: "alice",
		ChatUI:   ui.NewChatUI(nil),
	}
	v.ChatUI.SetUsername("alice")

	t.Run("help", func(t *testing.T) {
		s.handleInternalCommand(v, "/help")
		msgs := v.ChatUI.GetMessages()
		found := false
		for _, m := range msgs {
			if strings.Contains(m, "--- Available Commands ---") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Help command didn't show available commands")
		}
	})

	t.Run("who", func(t *testing.T) {
		s.mu.Lock()
		s.visitors["alice"] = v
		s.mu.Unlock()

		s.handleInternalCommand(v, "/who")
		msgs := v.ChatUI.GetMessages()
		found := false
		for _, m := range msgs {
			// Format is "<alice> /who" followed by "--- Visitors in room ---" then "• alice"
			if strings.Contains(m, "• alice") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Who command didn't show alice in %v", msgs)
		}
	})

	t.Run("files", func(t *testing.T) {
		s.handleInternalCommand(v, "/files")
		msgs := v.ChatUI.GetMessages()
		found := false
		for _, m := range msgs {
			if strings.Contains(m, "test.txt") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Files command didn't show test.txt")
		}
	})

	t.Run("download usage", func(t *testing.T) {
		s.handleInternalCommand(v, "/get")
		msgs := v.ChatUI.GetMessages()
		found := false
		for _, m := range msgs {
			if strings.Contains(m, "Usage: /get <filename>") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Download without args didn't show usage")
		}
	})

	t.Run("download success", func(t *testing.T) {
		s.handleInternalCommand(v, "/get test.txt")
		if v.PendingDownload != "test.txt" {
			t.Errorf("Expected PendingDownload to be 'test.txt', got '%s'", v.PendingDownload)
		}
	})
}
