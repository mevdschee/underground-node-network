package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestChatUINavigation(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	ui := NewChatUI(screen)
	ui.cmdInput.Value = "hello world"
	ui.cmdInput.CursorIdx = 11

	// Test Left Arrow
	ui.cmdInput.HandleKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if ui.cmdInput.CursorIdx != 10 {
		t.Errorf("Expected CursorIdx 10, got %d", ui.cmdInput.CursorIdx)
	}

	// Test Home
	ui.cmdInput.HandleKey(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	if ui.cmdInput.CursorIdx != 0 {
		t.Errorf("Expected CursorIdx 0, got %d", ui.cmdInput.CursorIdx)
	}

	// Test Right Arrow
	ui.cmdInput.HandleKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if ui.cmdInput.CursorIdx != 1 {
		t.Errorf("Expected CursorIdx 1, got %d", ui.cmdInput.CursorIdx)
	}

	// Test End
	ui.cmdInput.HandleKey(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone))
	if ui.cmdInput.CursorIdx != 11 {
		t.Errorf("Expected CursorIdx 11, got %d", ui.cmdInput.CursorIdx)
	}

	// Test Backspace in middle
	ui.cmdInput.CursorIdx = 6 // "hello |world"
	ui.cmdInput.HandleKey(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone))
	if ui.cmdInput.Value != "helloworld" || ui.cmdInput.CursorIdx != 5 {
		t.Errorf("Expected 'helloworld' and CursorIdx 5, got '%s' and %d", ui.cmdInput.Value, ui.cmdInput.CursorIdx)
	}

	// Test Rune insertion
	ui.cmdInput.HandleKey(tcell.NewEventKey(tcell.KeyRune, '-', tcell.ModNone))
	if ui.cmdInput.Value != "hello-world" || ui.cmdInput.CursorIdx != 6 {
		t.Errorf("Expected 'hello-world' and CursorIdx 6, got '%s' and %d", ui.cmdInput.Value, ui.cmdInput.CursorIdx)
	}

	// Test History (Up/Down)
	ui.cmdInput.Value = "my draft"
	ui.cmdInput.History = []string{"cmd1", "cmd2"}
	ui.cmdInput.HIndex = 2
	ui.cmdInput.HandleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)) // "cmd2"
	if ui.cmdInput.Value != "cmd2" || ui.cmdInput.HIndex != 1 {
		t.Errorf("Expected 'cmd2'. Got '%s'", ui.cmdInput.Value)
	}
	ui.cmdInput.HandleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))   // "cmd1"
	ui.cmdInput.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)) // "cmd2"
}

func TestEntryUINavigation(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	ui := NewEntryUI(screen, "alice", "localhost")
	ui.cmdInput.Value = "room1"
	ui.cmdInput.CursorIdx = 5

	// Test Left Arrow
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if ui.cmdInput.CursorIdx != 4 {
		t.Errorf("Expected CursorIdx 4, got %d", ui.cmdInput.CursorIdx)
	}

	// Test Home
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	if ui.cmdInput.CursorIdx != 0 {
		t.Errorf("Expected CursorIdx 0, got %d", ui.cmdInput.CursorIdx)
	}

	// Test End
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone))
	if ui.cmdInput.CursorIdx != 5 {
		t.Errorf("Expected CursorIdx 5, got %d", ui.cmdInput.CursorIdx)
	}

	// Test History
	ui.cmdInput.History = []string{"join main", "list"}
	ui.cmdInput.HIndex = 2
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if ui.cmdInput.Value != "list" || ui.cmdInput.HIndex != 1 {
		t.Errorf("Expected 'list', HIndex 1. Got '%s', %d", ui.cmdInput.Value, ui.cmdInput.HIndex)
	}
}
