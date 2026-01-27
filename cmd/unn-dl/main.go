package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type DownloadState struct {
	mu              sync.RWMutex
	filename        string
	destPath        string
	totalSize       int64
	downloaded      int64
	startTime       time.Time
	speed           float64 // bytes per second
	eta             time.Duration
	percentage      float64
	complete        bool
	failed          error
	originalName    string
	expectedSig     string
	checksumMatched bool
}

func main() {
	port := flag.Int("port", 0, "Tunnel port")
	transferID := flag.String("id", "", "Transfer UUID")
	filename := flag.String("file", "", "Original filename")
	expectedSig := flag.String("sig", "", "Expected SHA256 signature")
	identity := flag.String("identity", "", "Identity key path")
	batch := flag.Bool("batch", false, "Non-interactive batch mode")
	flag.Parse()

	if *port == 0 || *transferID == "" || *filename == "" {
		fmt.Println("Usage: unn-dl -port <port> -id <uuid> -file <filename> [-sig <sha256>] [-identity <key>]")
		os.Exit(1)
	}

	home, _ := os.UserHomeDir()
	defaultDest := filepath.Join(home, "Downloads", *filename)

	state := &DownloadState{
		originalName: *filename,
		filename:     *filename,
		destPath:     defaultDest,
		startTime:    time.Now(),
		expectedSig:  *expectedSig,
	}

	if *batch {
		tempFile, err := os.CreateTemp("", "unn-dl-*")
		if err != nil {
			log.Fatal(err)
		}
		tempPath := tempFile.Name()
		defer os.Remove(tempPath)
		fmt.Printf("Batch mode: Downloading %s to %s\n", *filename, defaultDest)
		startDownload(nil, state, *port, *transferID, *identity, tempFile)
		if state.failed != nil {
			fmt.Printf("Download failed: %v\n", state.failed)
			os.Exit(1)
		}
		if state.expectedSig != "" && !state.checksumMatched {
			fmt.Println("Error: SHA256 signature mismatch!")
			os.Exit(1)
		}
		os.MkdirAll(filepath.Dir(defaultDest), 0755)
		if err := moveFile(tempPath, defaultDest); err != nil {
			fmt.Printf("Error saving file: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Download complete.")
		return
	}

	// 1. Setup Logging for debugging
	logFile, _ := os.OpenFile("unn-dl.log", os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	if logFile != nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	// Initialize UI
	screen, err := tcell.NewScreen()
	if err != nil {
		log.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		log.Fatal(err)
	}
	defer screen.Fini()

	// 2. Start Download in background
	tempFile, err := os.CreateTemp("", "unn-dl-*")
	if err != nil {
		log.Fatal(err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	go startDownload(screen, state, *port, *transferID, *identity, tempFile)

	// 3. UI Loop
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(100 * time.Millisecond):
				drawUI(screen, state)
			}
		}
	}()

	// 4. Input Handling
	handleInput(screen, state, done)

	// 5. Finalize
	screen.Fini()

	state.mu.RLock()
	err = state.failed
	finalDest := state.destPath
	isComplete := state.complete
	state.mu.RUnlock()

	if err != nil {
		fmt.Printf("\nError: %v\n", err)
		os.Exit(1)
	}

	if isComplete {
		if state.expectedSig != "" && !state.checksumMatched {
			fmt.Println("\nError: SHA256 signature mismatch!")
			os.Exit(1)
		}

		// Ensure directory exists
		os.MkdirAll(filepath.Dir(finalDest), 0755)

		// Move and Rename
		if err := moveFile(tempPath, finalDest); err != nil {
			fmt.Printf("Error saving file to %s: %v\n", finalDest, err)
			os.Exit(1)
		}
		fmt.Printf("\nDownloaded %s to %s\n", *filename, finalDest)
	}
}

func startDownload(s tcell.Screen, state *DownloadState, port int, transferID string, identity string, dest io.WriteCloser) {
	defer func() {
		if dest != nil {
			dest.Close()
		}
		if s != nil {
			s.PostEvent(tcell.NewEventInterrupt(nil))
		}
	}()

	// Connect to local tunnel
	if s != nil {
		s.PostEvent(tcell.NewEventInterrupt(nil)) // Trigger redraw once connected (optional, but good for responsiveness)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		state.mu.Lock()
		state.failed = fmt.Errorf("failed to connect to tunnel: %w", err)
		state.mu.Unlock()
		return
	}
	defer conn.Close()

	// Handshake
	config := &ssh.ClientConfig{
		User:            "visitor",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	if identity != "" {
		signer, err := loadKey(identity)
		if err == nil {
			config.Auth = append(config.Auth, ssh.PublicKeys(signer))
		}
	} else {
		// Try standard keys as fallback
		home, _ := os.UserHomeDir()
		for _, k := range []string{"id_ed25519", "id_rsa"} {
			signer, err := loadKey(filepath.Join(home, ".ssh", k))
			if err == nil {
				config.Auth = append(config.Auth, ssh.PublicKeys(signer))
				break
			}
		}
	}

	ncc, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		state.mu.Lock()
		state.failed = fmt.Errorf("SSH handshake failed: %w", err)
		state.mu.Unlock()
		return
	}
	sftpSshClient := ssh.NewClient(ncc, chans, reqs)
	defer sftpSshClient.Close()

	sftpClient, err := sftp.NewClient(sftpSshClient)
	if err != nil {
		state.mu.Lock()
		state.failed = fmt.Errorf("SFTP client failed: %w", err)
		state.mu.Unlock()
		return
	}
	defer sftpClient.Close()

	remoteFile, err := sftpClient.Open(transferID)
	if err != nil {
		state.mu.Lock()
		state.failed = fmt.Errorf("could not open remote file: %w", err)
		state.mu.Unlock()
		return
	}
	defer remoteFile.Close()

	stat, _ := remoteFile.Stat()
	totalSize := stat.Size()

	state.mu.Lock()
	state.totalSize = totalSize
	state.mu.Unlock()

	// Copy with progress tracking and on-the-fly hashing
	hash := sha256.New()
	buf := make([]byte, 32*1024)
	var downloaded int64
	for {
		n, err := remoteFile.Read(buf)
		if n > 0 {
			if _, werr := dest.Write(buf[:n]); werr != nil {
				state.mu.Lock()
				state.failed = werr
				state.mu.Unlock()
				return
			}
			hash.Write(buf[:n])
			downloaded += int64(n)

			// Update stats
			now := time.Now()
			elapsed := now.Sub(state.startTime).Seconds()

			state.mu.Lock()
			state.downloaded = downloaded
			if elapsed > 0 {
				state.speed = float64(downloaded) / elapsed
				if state.speed > 0 {
					remaining := float64(totalSize - downloaded)
					state.eta = time.Duration(remaining/state.speed) * time.Second
				}
			}
			if totalSize > 0 {
				state.percentage = (float64(downloaded) / float64(totalSize)) * 100
			}
			state.mu.Unlock()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			state.mu.Lock()
			state.failed = err
			state.mu.Unlock()
			return
		}
	}

	state.mu.Lock()
	state.complete = true
	if state.expectedSig != "" {
		actualSig := hex.EncodeToString(hash.Sum(nil))
		state.checksumMatched = (actualSig == state.expectedSig)
	}
	state.mu.Unlock()
}

func drawUI(s tcell.Screen, state *DownloadState) {
	w, h := s.Size()

	state.mu.RLock()
	defer state.mu.RUnlock()

	// 1. Draw Background Dither Animation
	// Fill from bottom-left, row by row upwards
	totalChars := w * h
	filledChars := int(float64(totalChars) * (state.percentage / 100))

	// Dark Green theme
	bgStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.NewRGBColor(0, 60, 0)) // Dim dither
	filledBgStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorDarkGreen)  // Brighter dither

	for i := 0; i < totalChars; i++ {
		row := (h - 1) - (i / w)
		col := i % w

		style := bgStyle
		if i < filledChars {
			style = filledBgStyle
		}
		s.SetContent(col, row, '░', nil, style)
	}

	// 2. Draw Single Centered UI Box
	boxStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite)
	labelStyle := boxStyle.Foreground(tcell.ColorTeal)
	valueStyle := boxStyle.Foreground(tcell.ColorWhite)
	progressStyle := boxStyle.Foreground(tcell.ColorWhite).Background(tcell.ColorBlue)
	inputStyle := boxStyle.Foreground(tcell.ColorYellow).Background(tcell.ColorBlack) // No underlining

	// Box dimensions
	boxW := 60
	if boxW > w-4 {
		boxW = w - 4
	}
	boxH := 16 // Slightly smaller without title
	boxX := (w - boxW) / 2
	boxY := (h - boxH) / 2
	if boxY < 0 {
		boxY = 0
	}
	if boxX < 0 {
		boxX = 0
	}

	// Clear the UI box area
	for ry := 0; ry < boxH; ry++ {
		if boxY+ry >= h {
			break
		}
		for rx := 0; rx < boxW; rx++ {
			if boxX+rx >= w {
				break
			}
			s.SetContent(boxX+rx, boxY+ry, ' ', nil, boxStyle)
		}
	}

	// Draw Box Border (optional but looks nice)
	drawText(s, boxX, boxY, "┌"+strings.Repeat("─", boxW-2)+"┐", labelStyle)
	for ry := 1; ry < boxH-1; ry++ {
		s.SetContent(boxX, boxY+ry, '│', nil, labelStyle)
		s.SetContent(boxX+boxW-1, boxY+ry, '│', nil, labelStyle)
	}
	drawText(s, boxX, boxY+boxH-1, "└"+strings.Repeat("─", boxW-2)+"┘", labelStyle)

	// Content
	y := boxY + 2
	drawText(s, boxX+4, y, "FILE:     ", labelStyle)
	drawText(s, boxX+14, y, state.originalName, valueStyle)

	y++
	drawText(s, boxX+4, y, "SIZE:     ", labelStyle)
	drawText(s, boxX+14, y, formatSize(state.totalSize), valueStyle)

	y++
	drawText(s, boxX+4, y, "SPEED:    ", labelStyle)
	drawText(s, boxX+14, y, fmt.Sprintf("%.2f MB/s", state.speed/(1024*1024)), valueStyle)

	y++
	drawText(s, boxX+4, y, "ETA:      ", labelStyle)
	drawText(s, boxX+14, y, state.eta.Round(time.Second).String(), valueStyle)

	y++
	drawText(s, boxX+4, y, "CHECKSUM: ", labelStyle)
	csMsg := "PENDING"
	csStyle := valueStyle
	if state.complete {
		if state.expectedSig == "" {
			csMsg = "N/A"
		} else if state.checksumMatched {
			csMsg = "MATCHED ✓"
			csStyle = boxStyle.Foreground(tcell.ColorGreen)
		} else {
			csMsg = "MISMATCH ✗"
			csStyle = boxStyle.Foreground(tcell.ColorRed)
		}
	}
	drawText(s, boxX+14, y, csMsg, csStyle)

	// Progress Bar
	y = boxY + 8
	barW := boxW - 18
	filledW := int(float64(barW) * (state.percentage / 100))

	drawText(s, boxX+4, y, "PROGRESS: ", labelStyle)
	y++
	drawChar(s, boxX+4, y, '[', labelStyle)
	for i := 0; i < barW; i++ {
		char := ' '
		style := tcell.StyleDefault.Background(tcell.ColorGrey)
		if i < filledW {
			char = '█'
			style = progressStyle
		}
		s.SetContent(boxX+5+i, y, char, nil, style)
	}
	drawChar(s, boxX+5+barW, y, ']', labelStyle)
	drawText(s, boxX+boxW-12, y, fmt.Sprintf("%6.1f%%", state.percentage), valueStyle)

	// Destination Input
	y = boxY + 11
	drawText(s, boxX+4, y, "SAVE TO:", labelStyle)
	drawText(s, boxX+4, y+1, state.destPath, inputStyle)
	s.ShowCursor(boxX+4+len(state.destPath), y+1)

	// Footer (centered at bottom of box)
	msg := "[ESC] Cancel"
	if state.complete {
		msg = "[ENTER] Save and Return [ESC] Cancel"
	}
	if state.failed != nil {
		msg = fmt.Sprintf("ERROR: %v", state.failed)
	}
	footerX := boxX + (boxW-len(msg))/2
	drawText(s, footerX, boxY+boxH-2, msg, tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite))

	s.Show()
}

func drawChar(s tcell.Screen, x, y int, r rune, style tcell.Style) {
	s.SetContent(x, y, r, nil, style)
}

func handleInput(s tcell.Screen, state *DownloadState, done chan struct{}) {
	for {
		ev := s.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventInterrupt:
			state.mu.RLock()
			failed := state.failed
			state.mu.RUnlock()
			if failed != nil {
				return
			}
		case *tcell.EventKey:
			if ev.Key() == tcell.KeyEscape {
				state.mu.Lock()
				state.failed = fmt.Errorf("user cancelled")
				state.mu.Unlock()
				return
			}
			if ev.Key() == tcell.KeyEnter {
				state.mu.Lock()
				if state.complete {
					state.mu.Unlock()
					return
				}
				state.mu.Unlock()
			}
			if ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2 {
				state.mu.Lock()
				if len(state.destPath) > 0 {
					state.destPath = state.destPath[:len(state.destPath)-1]
				}
				state.mu.Unlock()
			} else if ev.Key() == tcell.KeyRune {
				state.mu.Lock()
				state.destPath += string(ev.Rune())
				state.mu.Unlock()
			}
		case *tcell.EventResize:
			s.Sync()
		}

		state.mu.RLock()
		if state.failed != nil {
			state.mu.RUnlock()
			return
		}
		state.mu.RUnlock()
	}
}

func drawText(s tcell.Screen, x, y int, str string, style tcell.Style) {
	col := x
	for _, r := range str {
		s.SetContent(col, y, r, nil, style)
		col++
	}
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

func loadKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(keyBytes)
}

func verifyFile(path, expectedHex string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}

	actualHex := hex.EncodeToString(h.Sum(nil))
	return actualHex == expectedHex
}

func moveFile(src, dst string) error {
	// Try rename first
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Fallback to copy
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()
	_, err = io.Copy(d, s)
	return err
}
