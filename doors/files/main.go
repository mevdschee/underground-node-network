package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// protocol types copied from internal/protocol
type FileBlockPayload struct {
	Action   string `json:"action,omitempty"`
	Filename string `json:"filename"`
	ID       string `json:"id"`
	Count    int    `json:"count"`
	Index    int    `json:"index"`
	Checksum string `json:"checksum"`
	Data     string `json:"data"` // Base64 encoded data
}

func main() {
	// Try a few likely locations for the files subfolder
	filesDir := "./room_files"
	if _, err := os.Stat(filesDir); os.IsNotExist(err) {
		fmt.Printf("Error listing files: %v\n", err)
		return
	}

	for {
		files, err := listFiles(filesDir)
		if err != nil {
			fmt.Printf("Error listing files: %v\n", err)
			return
		}

		currentDir, _ := filepath.Abs(filesDir)
		fmt.Printf("\033[H\033[2J") // Clear screen
		fmt.Println("--- UNN File Manager ---")
		fmt.Printf("Location: %s\n\n", currentDir)

		if len(files) == 0 {
			fmt.Println("No files available.")
			fmt.Printf("\nPress Enter to exit...")
			fmt.Scanln()
			return
		}

		for i, f := range files {
			fmt.Printf(" [\033[1;32m%d\033[0m] %-30s %10s\n", i+1, f.name, formatSize(f.size))
		}
		fmt.Printf(" [\033[1;31mQ\033[0m] Quit\n\n")

		fmt.Printf("Selection: ")
		var input string
		fmt.Scanln(&input)

		if strings.ToLower(input) == "q" {
			return
		}

		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(files) {
			continue
		}

		selected := files[idx-1]
		downloadFile(filepath.Join(filesDir, selected.name), selected.name)
	}
}

type fileInfo struct {
	name string
	size int64
}

func listFiles(dir string) ([]fileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []fileInfo
	for _, e := range entries {
		if !e.IsDir() {
			info, _ := e.Info()
			files = append(files, fileInfo{name: e.Name(), size: info.Size()})
		}
	}
	return files, nil
}

func downloadFile(path, filename string) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()

	info, _ := file.Stat()
	const blockSize = 8192
	count := int((info.Size() + blockSize - 1) / blockSize)

	// Create a unique ID for this transfer
	h := sha256.New()
	h.Write([]byte(time.Now().String() + filename))
	transferID := hex.EncodeToString(h.Sum(nil))[:16]

	fmt.Printf("\nCalculating checksum for %s...\n", filename)
	checksum := calculateSHA256(path)

	fmt.Printf("Starting transfer of %s (%d blocks)...\n", filename, count)

	buf := make([]byte, blockSize)
	for i := 0; i < count; i++ {
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			fmt.Printf("Error reading file: %v\n", err)
			return
		}

		payload := FileBlockPayload{
			Action:   "transfer_block",
			Filename: filename,
			ID:       transferID,
			Count:    count,
			Index:    i,
			Checksum: checksum,
			Data:     base64.StdEncoding.EncodeToString(buf[:n]),
		}

		sendOSC("transfer_block", payload)

		// Progress bar
		printProgress(i+1, count, filename)

		// Small delay to simulate rate limiting and let client process
		// 8KB per 10ms is ~800KB/s
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Printf("\n\n\033[1;32mTransfer of %s complete!\033[0m\n", filename)
	time.Sleep(1 * time.Second)
}

func sendOSC(action string, payload interface{}) {
	jsonData, _ := json.Marshal(payload)
	// We print directly to stdout as it will be captured by the client
	// and NOT printed to the terminal if it's a valid OSC 9 sequence.
	fmt.Printf("\x1b]9;%s\x07", string(jsonData))
}

func printProgress(current, total int, filename string) {
	width := 30
	pos := int(float64(current) / float64(total) * float64(width))
	fmt.Printf("\r\033[K[\033[1;32m%s%s\033[0m] %d/%d blocks (%s)",
		strings.Repeat("=", pos),
		strings.Repeat(" ", width-pos),
		current, total,
		filename)
}

func calculateSHA256(path string) string {
	f, _ := os.Open(path)
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
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
