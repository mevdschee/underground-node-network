package doors

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/creack/pty"
)

// Door represents an executable door program
type Door struct {
	Name string
	Path string
}

// Manager handles door discovery and execution
type Manager struct {
	doorsDir string
	doors    map[string]*Door
}

// NewManager creates a new door manager for the given directory
func NewManager(doorsDir string) *Manager {
	return &Manager{
		doorsDir: doorsDir,
		doors:    make(map[string]*Door),
	}
}

// Scan discovers executable doors in the doors directory
func (m *Manager) Scan() error {
	m.doors = make(map[string]*Door)

	entries, err := os.ReadDir(m.doorsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No doors directory is fine
		}
		return fmt.Errorf("failed to read doors directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(m.doorsDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Check if executable
		if info.Mode()&0111 != 0 {
			name := strings.TrimPrefix(entry.Name(), "/")
			m.doors[name] = &Door{
				Name: name,
				Path: path,
			}
		}
	}

	return nil
}

// List returns all available door names
func (m *Manager) List() []string {
	names := make([]string, 0, len(m.doors))
	for name := range m.doors {
		names = append(names, name)
	}
	return names
}

// Get returns a door by name
func (m *Manager) Get(name string) (*Door, bool) {
	door, ok := m.doors[name]
	return door, ok
}

// Execute runs a door program with I/O connected to the provided streams using a PTY
func (m *Manager) Execute(name string, stdin io.Reader, stdout, stderr io.Writer) error {
	door, ok := m.doors[name]
	if !ok {
		return fmt.Errorf("door not found: %s", name)
	}

	cmd := exec.Command(door.Path)

	// Start the command with a pty
	log.Printf("[DEBUG] Starting door program: %s", door.Path)
	f, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer f.Close()

	// Copy stdin to the pty
	stdinDone := make(chan struct{})
	go func() {
		log.Printf("[DEBUG] Starting stdin copier for door %s", name)
		buf := make([]byte, 1024)
		for {
			log.Printf("[DEBUG] Stdin copier for %s: Calling Read", name)
			n, err := stdin.Read(buf)
			log.Printf("[DEBUG] Stdin copier for %s: Read returned n=%d err=%v", name, n, err)
			if n > 0 {
				log.Printf("[DEBUG] Stdin copier for %s: Calling Write", name)
				_, werr := f.Write(buf[:n])
				log.Printf("[DEBUG] Stdin copier for %s: Write returned err=%v", name, werr)
				if werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		log.Printf("[DEBUG] Stdin copier finished for door %s", name)
		close(stdinDone)
	}()

	log.Printf("[DEBUG] Starting stdout copy for door %s", name)
	_, err = io.Copy(stdout, f)
	log.Printf("[DEBUG] Stdout copy finished for door %s (err: %v)", name, err)

	// Ensure the pty is closed to unblock the stdin goroutine if it's waiting on write
	f.Close()

	// If the stdin reader can be signaled to stop, do it now
	if se, ok := stdin.(interface{ SignalExit() }); ok {
		log.Printf("[DEBUG] Signaling stdin reader to exit for door %s", name)
		se.SignalExit()
	}

	log.Printf("[DEBUG] Waiting for stdin copier to finish for door %s", name)
	<-stdinDone // Wait for stdin copier to truly finish
	log.Printf("[DEBUG] Door %s Execute returning", name)

	if err != nil && (errors.Is(err, syscall.EIO) || strings.Contains(err.Error(), "input/output error")) {
		// Suppress EIO error on Linux when PTY slave is closed (process exit)
		return nil
	}
	return err
}
