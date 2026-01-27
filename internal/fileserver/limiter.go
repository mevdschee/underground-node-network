package fileserver

import (
	"io"
	"time"
)

// RateLimitedWriter wraps an io.Writer and limits its write speed.
type RateLimitedWriter struct {
	w           io.Writer
	bytesPerSec int64
	startTime   time.Time
	written     int64
}

// NewRateLimitedWriter creates a new RateLimitedWriter.
// If bytesPerSec is 0 or less, it returns the original writer.
func NewRateLimitedWriter(w io.Writer, bytesPerSec int64) io.Writer {
	if bytesPerSec <= 0 {
		return w
	}
	return &RateLimitedWriter{
		w:           w,
		bytesPerSec: bytesPerSec,
		startTime:   time.Now(),
	}
}

func (r *RateLimitedWriter) Write(p []byte) (int, error) {
	n, err := r.w.Write(p)
	if n > 0 {
		r.written += int64(n)
		r.throttle()
	}
	return n, err
}

func (r *RateLimitedWriter) throttle() {
	if r.bytesPerSec <= 0 {
		return
	}

	// Calculate how long we should have taken to write this much data
	expectedTime := time.Duration(r.written) * time.Second / time.Duration(r.bytesPerSec)
	actualTime := time.Since(r.startTime)

	if actualTime < expectedTime {
		time.Sleep(expectedTime - actualTime)
	}
}

// RateLimitedReadWriteCloser wraps an io.ReadWriteCloser and limits its write speed.
type RateLimitedReadWriteCloser struct {
	io.ReadCloser
	io.Writer
}

func (r *RateLimitedReadWriteCloser) Close() error {
	return r.ReadCloser.Close()
}

// NewRateLimitedReadWriteCloser wraps an io.ReadWriteCloser with a rate-limited writer.
func NewRateLimitedReadWriteCloser(rwc io.ReadWriteCloser, bytesPerSec int64) io.ReadWriteCloser {
	if bytesPerSec <= 0 {
		return rwc
	}
	return &RateLimitedReadWriteCloser{
		ReadCloser: rwc,
		Writer:     NewRateLimitedWriter(rwc, bytesPerSec),
	}
}
