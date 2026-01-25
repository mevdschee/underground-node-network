package sshserver

import (
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ThrottledChannel wraps an ssh.Channel to limit output speed
type ThrottledChannel struct {
	ssh.Channel
	bytesPerSec int
	lastWrite   time.Time
	mu          sync.Mutex
}

// NewThrottledChannel creates a new throttled SSH channel
func NewThrottledChannel(c ssh.Channel, baudRate int) *ThrottledChannel {
	// Assuming 10 bits per byte (1 start, 8 data, 1 stop)
	bytesPerSec := baudRate / 10
	if bytesPerSec <= 0 {
		bytesPerSec = 11520 // Default to 115k baud
	}
	return &ThrottledChannel{
		Channel:     c,
		bytesPerSec: bytesPerSec,
		lastWrite:   time.Now(),
	}
}

// Write implements io.Writer and throttles the output
func (t *ThrottledChannel) Write(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	n, err := t.Channel.Write(data)
	if n > 0 {
		// Calculate how long it SHOULD take to send this data
		duration := time.Duration(n) * time.Second / time.Duration(t.bytesPerSec)

		// If we are writing faster than the target rate, sleep
		now := time.Now()
		targetTime := t.lastWrite.Add(duration)

		if targetTime.After(now) {
			time.Sleep(targetTime.Sub(now))
			t.lastWrite = targetTime
		} else {
			t.lastWrite = now
		}
	}
	return n, err
}

// Stderr returns a throttled stderr read-writer
func (t *ThrottledChannel) Stderr() io.ReadWriter {
	return &throttledReadWriter{
		rw:          t.Channel.Stderr(),
		bytesPerSec: t.bytesPerSec,
		parent:      t,
	}
}

// throttledReadWriter is a simple io.ReadWriter wrapper for stderr
type throttledReadWriter struct {
	rw          io.ReadWriter
	bytesPerSec int
	parent      *ThrottledChannel
}

func (trw *throttledReadWriter) Write(data []byte) (int, error) {
	trw.parent.mu.Lock()
	defer trw.parent.mu.Unlock()

	n, err := trw.rw.Write(data)
	if n > 0 {
		duration := time.Duration(n) * time.Second / time.Duration(trw.bytesPerSec)
		now := time.Now()
		targetTime := trw.parent.lastWrite.Add(duration)

		if targetTime.After(now) {
			time.Sleep(targetTime.Sub(now))
			trw.parent.lastWrite = targetTime
		} else {
			trw.parent.lastWrite = now
		}
	}
	return n, err
}

func (trw *throttledReadWriter) Read(data []byte) (int, error) {
	return trw.rw.Read(data)
}
