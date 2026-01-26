package ui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestChatUICtrlC(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	// We don't call Fini here because ChatUI.Run() will call it or we handle it.
	// Actually SimulationScreen needs cleanup too.

	ui := NewChatUI(screen)
	closed := false
	ui.OnClose(func() {
		closed = true
	})

	go func() {
		time.Sleep(200 * time.Millisecond)
		screen.PostEvent(tcell.NewEventKey(tcell.KeyCtrlC, 'c', tcell.ModNone))
	}()

	cmd := ui.Run()
	if cmd != "" {
		t.Errorf("Expected empty command on Ctrl+C, got '%s'", cmd)
	}
	if !closed {
		t.Errorf("OnClose callback not triggered on Ctrl+C")
	}
}

func TestEntryUICtrlC(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}

	ui := NewEntryUI(screen, "alice", "localhost")

	go func() {
		time.Sleep(200 * time.Millisecond)
		screen.PostEvent(tcell.NewEventKey(tcell.KeyCtrlC, 'c', tcell.ModNone))
	}()

	success := ui.Run()
	if success {
		t.Errorf("Expected success=false on Ctrl+C")
	}
}
