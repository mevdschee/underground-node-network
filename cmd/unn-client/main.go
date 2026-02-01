package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type StdinManager struct {
	mu     sync.Mutex
	writer io.Writer
	active bool
	paused bool
}

func (m *StdinManager) SetWriter(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writer = w
}

func (m *StdinManager) Pause() {
	m.mu.Lock()
	m.paused = true
	m.mu.Unlock()
}

func (m *StdinManager) Resume() {
	m.mu.Lock()
	m.paused = false
	m.mu.Unlock()
}

func (m *StdinManager) Start() {
	m.mu.Lock()
	if m.active {
		m.mu.Unlock()
		return
	}
	m.active = true
	m.mu.Unlock()

	go func() {
		buf := make([]byte, 1024)
		fd := int(os.Stdin.Fd())
		for {
			m.mu.Lock()
			paused := m.paused
			m.mu.Unlock()

			if paused {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Use syscall.Select to wait for input without blocking forever
			// This allows us to check the 'paused' flag frequently.
			readfds := &syscall.FdSet{}
			readfds.Bits[fd/64] |= 1 << (uint(fd) % 64)
			timeout := &syscall.Timeval{Sec: 0, Usec: 100000} // 100ms

			n, err := syscall.Select(fd+1, readfds, nil, nil, timeout)
			if err != nil && err != syscall.EINTR {
				return
			}
			if n > 0 {
				n, err := os.Stdin.Read(buf)
				if err != nil {
					return
				}
				m.mu.Lock()
				if m.writer != nil && !m.paused {
					m.writer.Write(buf[:n])
				}
				m.mu.Unlock()
			}
		}
	}()
}

var globalStdinManager StdinManager

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] unn://entrypoint[:port]/[roomname]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nTeleport to a UNN room via SSH.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s unn://localhost/myroom\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s unn://localhost (interactive mode)\n", os.Args[0])
	}

	verbose := flag.Bool("v", false, "Verbose output")
	identity := flag.String("identity", "", "Path to private key for authentication")
	batch := flag.Bool("batch", false, "Non-interactive batch mode")
	homeDir, _ := os.UserHomeDir()
	defaultDownloads := filepath.Join(homeDir, "Downloads")
	downloads := flag.String("downloads", defaultDownloads, "Directory for file downloads")
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	unnUrl := flag.Arg(0)
	// Ignore SIGINT so it's passed as a byte to the SSH sessions
	signal.Ignore(os.Interrupt)
	if err := teleport(unnUrl, *identity, *verbose, *batch, *downloads); err != nil {
		log.Fatalf("Error: %v", err)
	}
}
