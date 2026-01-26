package sshserver

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// handleSCPSend implements the SCP "source" protocol to send a file to the client
func (s *Server) handleSCPSend(channel ssh.Channel, command string) {
	defer channel.Close()

	// Parse filename from command: "scp -f filename"
	args := parseSCPArgs(command)
	if len(args) == 0 {
		return
	}
	filename := args[len(args)-1]

	// Sanitize and prevent path traversal
	cleanTarget := filepath.Clean(filename)
	// If the user requested ".", scp will send "scp -f .", we need to handle this
	// but for simplicity we assume the user provides a specific filename since
	// our /download command provides it.

	path := filepath.Join(s.filesDir, cleanTarget)

	// Ensure the path is within s.filesDir
	absBase, _ := filepath.Abs(s.filesDir)
	absPath, _ := filepath.Abs(path)
	if !strings.HasPrefix(absPath, absBase) {
		fmt.Fprintf(channel.Stderr(), "scp: access denied\n")
		return
	}

	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(channel.Stderr(), "scp: %v\n", err)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return
	}

	// 1. Wait for start signal (null byte)
	buf := make([]byte, 1)
	if _, err := channel.Read(buf); err != nil || buf[0] != 0 {
		return
	}

	// 2. Send file header: C<mode> <size> <base_name>\n
	header := fmt.Sprintf("C%04o %d %s\n", info.Mode()&0777, info.Size(), filepath.Base(cleanTarget))
	if _, err := fmt.Fprint(channel, header); err != nil {
		return
	}

	// 3. Wait for acknowledgment
	if _, err := channel.Read(buf); err != nil || buf[0] != 0 {
		return
	}

	// 4. Send file content
	if _, err := io.Copy(channel, file); err != nil {
		return
	}

	// 5. Send final acknowledgment
	if _, err := channel.Write([]byte{0}); err != nil {
		return
	}

	// 6. Wait for final acknowledgment
	channel.Read(buf)
}

func parseSCPArgs(command string) []string {
	parts := strings.Fields(command)
	for i, part := range parts {
		if part == "-f" && i+1 < len(parts) {
			// In SCP "source" mode (-f), everything after -f is the path(s)
			return []string{strings.Join(parts[i+1:], " ")}
		}
	}
	return parts
}
