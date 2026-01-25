package doors

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// crlfWriter wraps an io.Writer and replaces \n with \r\n
type crlfWriter struct {
	w             io.Writer
	lastCharWasCR bool
}

func (cw *crlfWriter) Write(p []byte) (n int, err error) {
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '\n' {
			// Write everything up to this \n
			if i > start {
				if _, err := cw.w.Write(p[start:i]); err != nil {
					return n, err
				}
			}
			// If not preceded by \r, write \r first
			if !cw.lastCharWasCR {
				if _, err := cw.w.Write([]byte{'\r'}); err != nil {
					return n, err
				}
			}
			if _, err := cw.w.Write([]byte{'\n'}); err != nil {
				return n, err
			}
			start = i + 1
			cw.lastCharWasCR = false
		} else {
			cw.lastCharWasCR = (p[i] == '\r')
		}
	}
	if start < len(p) {
		if _, err := cw.w.Write(p[start:]); err != nil {
			return n, err
		}
	}
	return len(p), nil
}

// Execute runs a door program with I/O connected to the provided streams
func (m *Manager) Execute(name string, stdin io.Reader, stdout, stderr io.Writer) error {
	door, ok := m.doors[name]
	if !ok {
		return fmt.Errorf("door not found: %s", name)
	}

	cmd := exec.Command(door.Path)
	cmd.Stdin = stdin
	cmd.Stdout = &crlfWriter{w: stdout}
	cmd.Stderr = &crlfWriter{w: stderr}

	return cmd.Run()
}
