package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestChatUINavigation(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	ui := NewChatUI(screen)
	ui.input = "hello world"
	ui.cursorIdx = 11

	// Test Left Arrow
	ui.handleKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if ui.cursorIdx != 10 {
		t.Errorf("Expected cursorIdx 10, got %d", ui.cursorIdx)
	}

	// Test Home
	ui.handleKey(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	if ui.cursorIdx != 0 {
		t.Errorf("Expected cursorIdx 0, got %d", ui.cursorIdx)
	}

	// Test Right Arrow
	ui.handleKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if ui.cursorIdx != 1 {
		t.Errorf("Expected cursorIdx 1, got %d", ui.cursorIdx)
	}

	// Test End
	ui.handleKey(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone))
	if ui.cursorIdx != 11 {
		t.Errorf("Expected cursorIdx 11, got %d", ui.cursorIdx)
	}

	// Test Backspace in middle
	ui.cursorIdx = 6 // "hello |world"
	ui.handleKey(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone))
	if ui.input != "helloworld" || ui.cursorIdx != 5 {
		t.Errorf("Expected 'helloworld' and cursorIdx 5, got '%s' and %d", ui.input, ui.cursorIdx)
	}

	// Test Rune insertion
	ui.handleKey(tcell.NewEventKey(tcell.KeyRune, '-', tcell.ModNone))
	if ui.input != "hello-world" || ui.cursorIdx != 6 {
		t.Errorf("Expected 'hello-world' and cursorIdx 6, got '%s' and %d", ui.input, ui.cursorIdx)
	}

	// Test History (Up/Down)
	ui.history = []string{"cmd1", "cmd2"}
	ui.hIndex = 2
	ui.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if ui.input != "cmd2" || ui.hIndex != 1 || ui.cursorIdx != 4 {
		t.Errorf("Expected 'cmd2', hIndex 1, cursorIdx 4. Got '%s', %d, %d", ui.input, ui.hIndex, ui.cursorIdx)
	}
	ui.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if ui.input != "cmd1" || ui.hIndex != 0 {
		t.Errorf("Expected 'cmd1', hIndex 0. Got '%s', %d", ui.input, ui.hIndex)
	}
	ui.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if ui.input != "cmd2" || ui.hIndex != 1 {
		t.Errorf("Expected 'cmd2', hIndex 1. Got '%s', %d", ui.input, ui.hIndex)
	}
}

func TestEntryUINavigation(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	ui := NewEntryUI(screen, "alice", "localhost")
	ui.input = "room1"
	ui.cursorIdx = 5

	// Test Left Arrow
	ui.handleKeyResult(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if ui.cursorIdx != 4 {
		t.Errorf("Expected cursorIdx 4, got %d", ui.cursorIdx)
	}

	// Test Home
	ui.handleKeyResult(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	if ui.cursorIdx != 0 {
		t.Errorf("Expected cursorIdx 0, got %d", ui.cursorIdx)
	}

	// Test End
	ui.handleKeyResult(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone))
	if ui.cursorIdx != 5 {
		t.Errorf("Expected cursorIdx 5, got %d", ui.cursorIdx)
	}

	// Test History
	ui.history = []string{"join main", "list"}
	ui.hIndex = 2
	ui.handleKeyResult(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if ui.input != "list" || ui.hIndex != 1 {
		t.Errorf("Expected 'list', hIndex 1. Got '%s', %d", ui.input, ui.hIndex)
	}
}
