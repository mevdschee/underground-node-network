package entrypoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/protocol"
	"github.com/mevdschee/underground-node-network/internal/ui"
)

func TestEntrypointCommands(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "unn-ep-test-*")
	defer os.RemoveAll(tmpDir)
	usersDir := filepath.Join(tmpDir, "users")
	os.MkdirAll(usersDir, 0755)

	hostKeyPath := filepath.Join(tmpDir, "host_key")
	s, err := NewServer("127.0.0.1:0", hostKeyPath, usersDir)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	screen := tcell.NewSimulationScreen("")
	screen.Init()
	v := &Visitor{
		Username: "bob",
		UI:       ui.NewEntryUI(screen, "bob", "127.0.0.1"),
	}

	t.Run("help", func(t *testing.T) {
		s.handleVisitorCommand(v, nil, "/help")
		logs := v.UI.GetLogs()
		found := false
		for _, l := range logs {
			if strings.Contains(l.Text, "/rooms") && strings.Contains(l.Text, "List all active rooms") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Help command didn't show available commands")
		}
	})

	t.Run("rooms empty", func(t *testing.T) {
		s.handleVisitorCommand(v, nil, "/rooms")
		logs := v.UI.GetLogs()
		found := false
		for _, l := range logs {
			if strings.Contains(l.Text, "No active rooms.") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Rooms command didn't show empty state")
		}
	})

	t.Run("rooms with data", func(t *testing.T) {
		s.mu.Lock()
		s.rooms["myroom"] = &Room{
			Info: protocol.RoomInfo{Name: "myroom", Owner: "alice"},
		}
		s.mu.Unlock()

		s.handleVisitorCommand(v, nil, "/rooms")
		logs := v.UI.GetLogs()
		found := false
		for _, l := range logs {
			if strings.Contains(l.Text, "myroom") && strings.Contains(l.Text, "alice") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Rooms command didn't show myroom")
		}
	})

	t.Run("join non-existent", func(t *testing.T) {
		s.handleVisitorCommand(v, nil, "nonexistent")
		logs := v.UI.GetLogs()
		found := false
		for _, l := range logs {
			if strings.Contains(l.Text, "Room not found: nonexistent") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Joining non-existent room didn't show error")
		}
	})

	t.Run("register usage", func(t *testing.T) {
		s.handleVisitorCommand(v, nil, "/register")
		logs := v.UI.GetLogs()
		found := false
		for _, l := range logs {
			if strings.Contains(l.Text, "Usage: /register <public_key>") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Register without args didn't show usage")
		}
	})
}
