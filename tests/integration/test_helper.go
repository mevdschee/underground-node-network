package integration

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type UNNProcess struct {
	cmd    *exec.Cmd
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func (p *UNNProcess) Stop() {
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Signal(os.Interrupt)
		p.cmd.Wait()
	}
}

func startEntryPoint(t *testing.T, binPath string, port int, hostKeyPath string, usersPath string) *UNNProcess {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := exec.Command(binPath, "-port", fmt.Sprintf("%d", port), "-hostkey", hostKeyPath, "-users", usersPath, "-headless")
	cmd.Stdout = io.MultiWriter(stdout, os.Stdout)
	cmd.Stderr = io.MultiWriter(stderr, os.Stderr)

	fmt.Printf("Starting entrypoint with command: %s %s\n", binPath, strings.Join(cmd.Args[1:], " "))

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start entry point: %v", err)
	}

	// Wait for it to be ready
	waitForPort(t, "localhost", port, 5*time.Second)

	return &UNNProcess{cmd: cmd, stdout: stdout, stderr: stderr}
}

func startRoom(t *testing.T, binPath string, name string, port int, epAddr string, hostKeyPath string, identityPath string, filesDir string) *UNNProcess {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	args := []string{
		"-room", name,
		"-port", fmt.Sprintf("%d", port),
		"-entrypoint", epAddr,
		"-hostkey", hostKeyPath,
		"-identity", identityPath,
		"-files", filesDir,
		"-headless",
	}
	cmd := exec.Command(binPath, args...)
	cmd.Stdout = io.MultiWriter(stdout, os.Stdout)
	cmd.Stderr = io.MultiWriter(stderr, os.Stderr)

	fmt.Printf("Starting room node with command: %s %s\n", binPath, strings.Join(args, " "))

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start room %s: %v", name, err)
	}

	// Wait for it to be ready (it starts an SSH server)
	waitForPort(t, "localhost", port, 5*time.Second)

	return &UNNProcess{cmd: cmd, stdout: stdout, stderr: stderr}
}

func waitForPort(t *testing.T, host string, port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for port %d", port)
}

func buildBinaries(t *testing.T) (string, string) {
	tempDir, err := os.MkdirTemp("", "unn_test_binaries_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir for binaries: %v", err)
	}

	epBin := filepath.Join(tempDir, "unn-entrypoint")
	roomBin := filepath.Join(tempDir, "unn-room")

	cmd := exec.Command("go", "build", "-o", epBin, "../../cmd/unn-entrypoint")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build entrypoint: %v\nOutput: %s", err, string(out))
	}

	cmd = exec.Command("go", "build", "-o", roomBin, "../../cmd/unn-room")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build room node: %v\nOutput: %s", err, string(out))
	}

	return epBin, roomBin
}

func getSSHClient(t *testing.T, addr string, user string, keyPath string) (*ssh.Client, *ssh.Session) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("unable to read private key %s: %v", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		t.Fatalf("unable to parse private key %s: %v", keyPath, err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
		ClientVersion:   "SSH-2.0-UNN-Test-Client",
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("unable to connect to %s: %v", addr, err)
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		t.Fatalf("unable to create session for %s: %v", addr, err)
	}

	return client, session
}

func runSSHCommand(t *testing.T, session *ssh.Session, command string) string {
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	in, _ := session.StdinPipe()
	if err := session.RequestPty("xterm", 80, 40, ssh.TerminalModes{}); err != nil {
		t.Fatalf("failed to request pty: %v", err)
	}
	if err := session.Shell(); err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	fmt.Fprintln(in, command)
	time.Sleep(1 * time.Second) // Give it more time to process
	in.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Log("Warning: session.Wait() timed out")
	}

	out := stdout.String()
	fmt.Printf("Command '%s' output:\n%s\n", command, out)
	return out
}
