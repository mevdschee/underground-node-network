package main

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/mevdschee/underground-node-network/internal/protocol"
)

func TestAssembleFile(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "unn-test-asm-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	globalDownloadsDir = tmpDir
	transferID := "test-id"
	filename := "hello.txt"
	partsPath := filepath.Join(tmpDir, filename+"."+transferID+".parts")

	// Create mock NDJSON parts
	payloads := []protocol.FileBlockPayload{
		{
			Action:   "transfer_block",
			Filename: filename,
			ID:       transferID,
			Count:    2,
			Index:    0,
			Data:     base64.StdEncoding.EncodeToString([]byte("Hello ")),
		},
		{
			Action:   "transfer_block",
			Filename: filename,
			ID:       transferID,
			Count:    2,
			Index:    1,
			Data:     base64.StdEncoding.EncodeToString([]byte("World!")),
		},
	}

	f, err := os.Create(partsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range payloads {
		data, _ := json.Marshal(p)
		f.Write(append(data, '\n'))
	}
	f.Close()

	state := &oscTransferState{
		partsPath: partsPath,
		filename:  filename,
		total:     2,
		indices:   map[int]bool{0: true, 1: true},
	}

	// Mock active transfers
	activeTransfers[transferID] = state

	assembleFile(state, transferID, true)

	// Check final file
	finalPath := filepath.Join(tmpDir, filename)
	data, err := ioutil.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "Hello World!" {
		t.Errorf("expected 'Hello World!', got '%s'", string(data))
	}

	// Check that parts file is removed
	if _, err := os.Stat(partsPath); !os.IsNotExist(err) {
		t.Error("expected parts file to be removed")
	}

	// Check that state is removed
	if _, ok := activeTransfers[transferID]; ok {
		t.Error("expected state to be removed from activeTransfers")
	}
}
