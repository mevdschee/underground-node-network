package nat

import (
	"net"
	"time"

	"github.com/quic-go/quic-go"
)

// QUICStreamConn wraps a QUIC stream to implement net.Conn interface
// This allows QUIC streams to be used with SSH which expects net.Conn
type QUICStreamConn struct {
	stream *quic.Stream
	conn   *quic.Conn
}

// NewQUICStreamConn creates a new net.Conn wrapper around a QUIC stream
func NewQUICStreamConn(stream *quic.Stream, conn *quic.Conn) *QUICStreamConn {
	return &QUICStreamConn{
		stream: stream,
		conn:   conn,
	}
}

// Read reads data from the stream
func (qsc *QUICStreamConn) Read(b []byte) (int, error) {
	return qsc.stream.Read(b)
}

// Write writes data to the stream
func (qsc *QUICStreamConn) Write(b []byte) (int, error) {
	return qsc.stream.Write(b)
}

// Close closes the stream
func (qsc *QUICStreamConn) Close() error {
	return qsc.stream.Close()
}

// LocalAddr returns the local network address
func (qsc *QUICStreamConn) LocalAddr() net.Addr {
	return qsc.conn.LocalAddr()
}

// RemoteAddr returns the remote network address
func (qsc *QUICStreamConn) RemoteAddr() net.Addr {
	return qsc.conn.RemoteAddr()
}

// SetDeadline sets the read and write deadlines
func (qsc *QUICStreamConn) SetDeadline(t time.Time) error {
	if err := qsc.stream.SetReadDeadline(t); err != nil {
		return err
	}
	return qsc.stream.SetWriteDeadline(t)
}

// SetReadDeadline sets the read deadline
func (qsc *QUICStreamConn) SetReadDeadline(t time.Time) error {
	return qsc.stream.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline
func (qsc *QUICStreamConn) SetWriteDeadline(t time.Time) error {
	return qsc.stream.SetWriteDeadline(t)
}
