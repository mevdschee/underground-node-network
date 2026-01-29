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

	// Test History (Up/Down) with Draft
	ui.input = "my draft"
	ui.cursorIdx = 8
	ui.history = []string{"cmd1", "cmd2"}
	ui.hIndex = 2
	ui.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)) // "cmd2"
	if ui.input != "cmd2" || ui.hIndex != 1 || ui.draft != "my draft" {
		t.Errorf("Expected 'cmd2' and draft 'my draft'. Got '%s', %s", ui.input, ui.draft)
	}
	ui.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))   // "cmd1"
	ui.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)) // "cmd2"
	ui.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)) // "my draft"
	if ui.input != "my draft" || ui.hIndex != 2 {
		t.Errorf("Expected 'my draft' restored, got '%s'", ui.input)
	}

	// Test Horizontal Scrolling
	// Simulate small screen (w=10). prompt="> " (len 2). availWidth = 10 - 2 - 2 = 6
	// The availWidth calculation in code is w - len([]rune(prompt)) - 2
	ui.input = ""
	ui.cursorIdx = 0
	ui.inputOffset = 0
	// For testing, we need to mock ui.screen.Size() or use the actual simulation screen
	simScreen := ui.screen.(tcell.SimulationScreen)
	simScreen.SetSize(10, 10)

	// Type 9 chars: "123456789"
	for _, r := range "123456789" {
		ui.handleKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	// cursorIdx = 9.
	// Before typing '9', offset was 0 (fits in availWidth 6-8).
	// With '9', len is 9. availWidth is 6 (since offset was 0).
	// centeredOffset = 9 - 6/2 = 6.
	// maxOffset = 9 - 6 = 3.
	// final clampedOffset = 3.
	if ui.inputOffset != 3 {
		t.Errorf("Expected inputOffset 3 (centered), got %d", ui.inputOffset)
	}

	// Move Left: cursorIdx = 8
	ui.handleKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	// cursorIdx = 8. offset=3 -> promptLen=0 -> availWidth=8.
	// centeredOffset = 8 - 4 = 4.
	// maxOffset = 9 - 8 = 1.
	// clampedOffset = 1.
	if ui.inputOffset != 1 {
		t.Errorf("Expected inputOffset 1 after Left, got %d", ui.inputOffset)
	}
}

func TestEntryUINavigation(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	ui := NewEntryUI(screen, "alice", "localhost")
	ui.input = "room1"
	ui.cursorIdx = 5

	// Test Left Arrow
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if ui.cursorIdx != 4 {
		t.Errorf("Expected cursorIdx 4, got %d", ui.cursorIdx)
	}

	// Test Home
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	if ui.cursorIdx != 0 {
		t.Errorf("Expected cursorIdx 0, got %d", ui.cursorIdx)
	}

	// Test End
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone))
	if ui.cursorIdx != 5 {
		t.Errorf("Expected cursorIdx 5, got %d", ui.cursorIdx)
	}

	// Test History
	ui.history = []string{"join main", "list"}
	ui.hIndex = 2
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if ui.input != "list" || ui.hIndex != 1 {
		t.Errorf("Expected 'list', hIndex 1. Got '%s', %d", ui.input, ui.hIndex)
	}

	// Test History (Up/Down) with Draft
	ui.input = "entry draft"
	ui.history = []string{"join room1", "list"}
	ui.hIndex = 2
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)) // "list"
	if ui.input != "list" || ui.draft != "entry draft" {
		t.Errorf("Expected 'list' and draft 'entry draft'. Got '%s', %s", ui.input, ui.draft)
	}
	ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)) // "entry draft"
	if ui.input != "entry draft" {
		t.Errorf("Expected draft 'entry draft' restored, got '%s'", ui.input)
	}

	// Test Horizontal Scrolling (Centered)
	ui.input = ""
	ui.cursorIdx = 0
	ui.inputOffset = 0
	simScreen := ui.screen.(tcell.SimulationScreen)
	simScreen.SetSize(10, 10)

	// Type 9 chars
	for _, r := range "123456789" {
		ui.HandleKeyResult(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	// Same expectation as ChatUI: offset should be 3
	if ui.inputOffset != 3 {
		t.Errorf("Expected EntryUI inputOffset 3 (centered), got %d", ui.inputOffset)
	}
}
