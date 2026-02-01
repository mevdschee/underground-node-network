package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mevdschee/underground-node-network/internal/protocol"
)

type oscTransferState struct {
	partsPath string
	filename  string
	received  int
	total     int
	checksum  string
	indices   map[int]bool
}

var (
	activeTransfers = make(map[string]*oscTransferState)
	transfersMu     sync.Mutex
)

func handleOSCBlockTransfer(p protocol.FileBlockPayload, verbose bool) {
	transfersMu.Lock()
	state, ok := activeTransfers[p.ID]
	if !ok {
		// New transfer
		if _, err := os.Stat(globalDownloadsDir); os.IsNotExist(err) {
			os.MkdirAll(globalDownloadsDir, 0755)
		}

		partsPath := filepath.Join(globalDownloadsDir, fmt.Sprintf("%s.%s.parts", p.Filename, p.ID))
		state = &oscTransferState{
			partsPath: partsPath,
			filename:  p.Filename,
			total:     p.Count,
			checksum:  p.Checksum,
			indices:   make(map[int]bool),
		}
		activeTransfers[p.ID] = state
	}
	transfersMu.Unlock()

	// Append as NDJSON
	f, err := os.OpenFile(state.partsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open parts file: %v", err)
		return
	}
	defer f.Close()

	jsonData, _ := json.Marshal(p)
	if _, err := f.Write(append(jsonData, '\n')); err != nil {
		log.Printf("Failed to write part: %v", err)
		return
	}

	if !state.indices[p.Index] {
		state.indices[p.Index] = true
		state.received++
	}

	// Progress reporting
	if verbose {
		log.Printf("Downloading %s: %d/%d blocks (%.1f%%)", state.filename, state.received, state.total, float64(state.received)/float64(state.total)*100)
	}

	if state.received >= state.total {
		if verbose {
			log.Printf("Assembling %s...", state.filename)
		}
		assembleFile(state, p.ID, verbose)
	}
}

func assembleFile(state *oscTransferState, transferID string, verbose bool) {
	// 1. Read all blocks from NDJSON
	data, err := os.ReadFile(state.partsPath)
	if err != nil {
		log.Printf("Failed to read parts: %v", err)
		return
	}

	lines := strings.Split(string(data), "\n")
	blocks := make([][]byte, state.total)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var p protocol.FileBlockPayload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			continue
		}
		if p.Index < len(blocks) {
			blocks[p.Index] = decoded
		}
	}

	// 2. Determine unique final path
	finalPath := getUniquePath(filepath.Join(globalDownloadsDir, state.filename))

	// 3. Write and hash
	hasher := sha256.New()
	out, err := os.Create(finalPath)
	if err != nil {
		log.Printf("Failed to create final file: %v", err)
		return
	}
	defer out.Close()

	for _, b := range blocks {
		if b == nil {
			log.Printf("Missing block in reassembly!")
			return
		}
		out.Write(b)
		hasher.Write(b)
	}

	// 4. Verify checksum
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if state.checksum != "" && actualChecksum != state.checksum {
		log.Printf("Checksum mismatch for %s!", state.filename)
		log.Printf("Expected: %s", state.checksum)
		log.Printf("Actual:   %s", actualChecksum)
	} else {
		if verbose {
			log.Printf("Saved %s to %s", state.filename, finalPath)
		}
		os.Remove(state.partsPath)
	}

	transfersMu.Lock()
	delete(activeTransfers, transferID)
	transfersMu.Unlock()
}
