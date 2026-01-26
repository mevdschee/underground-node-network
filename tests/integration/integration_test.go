package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntegration_BasicRegistration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "unn_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	epBin, clientBin := buildBinaries(t)
	// Build binaries is recursive, so clean it up if needed, but normally it's in a temp dir.

	clientName := "testroom1"
	clientPort := 22223
	clientHostKey := filepath.Join(tempDir, "client_host_key")
	clientIdentity := "../../tests/integration/test_room_key"
	filesDir := filepath.Join(tempDir, "files")
	os.MkdirAll(filesDir, 0700)
	os.WriteFile(filepath.Join(filesDir, "testfile.txt"), []byte("hello world"), 0644)

	// Pre-register room name (as user) and test user
	usersDir := filepath.Join(tempDir, "users") // This matches how cmd/unn-entrypoint calculates usersDir
	os.MkdirAll(usersDir, 0700)

	roomKey, _ := os.ReadFile("../../tests/integration/test_room_key.pub")
	userKey, _ := os.ReadFile("../../tests/integration/test_user_key.pub")

	os.WriteFile(filepath.Join(usersDir, clientName), roomKey, 0600)
	os.WriteFile(filepath.Join(usersDir, "maurits"), userKey, 0600)

	epPort := 44323
	epHostKey := filepath.Join(tempDir, "ep_host_key")
	epProcess := startEntryPoint(t, epBin, epPort, epHostKey, usersDir)
	defer epProcess.Stop()

	clientProcess := startClient(t, clientBin, clientName, clientPort, "localhost:44323", clientHostKey, clientIdentity, filesDir)
	defer clientProcess.Stop()

	// Wait for registration to complete
	time.Sleep(2 * time.Second)

	// Connect to entrypoint via SSH
	fmt.Println("Connecting to entrypoint via SSH...")
	sshClient, session := getSSHClient(t, "localhost:44323", "maurits", "../../tests/integration/test_user_key")
	defer sshClient.Close()
	defer session.Close()

	// List rooms
	fmt.Println("Running /rooms command...")
	output := runSSHCommand(t, session, "/rooms")
	if !strings.Contains(output, clientName) {
		t.Errorf("Expected room %s to be in output, but got:\n%s", clientName, output)
	}

	// Join room to trigger P2P authorization
	fmt.Printf("Joining room %s via entrypoint...\n", clientName)
	sshClientJoin, sessionJoin := getSSHClient(t, "localhost:44323", "maurits", "../../tests/integration/test_user_key")
	defer sshClientJoin.Close()
	defer sessionJoin.Close()
	runSSHCommand(t, sessionJoin, clientName)

	time.Sleep(2 * time.Second)

	// Connect to the room node directly (since entrypoint doesn't proxy)
	fmt.Printf("Connecting directly to room at localhost:%d...\n", clientPort)
	sshClient2, session2 := getSSHClient(t, fmt.Sprintf("localhost:%d", clientPort), "maurits", "../../tests/integration/test_user_key")
	defer sshClient2.Close()
	defer session2.Close()

	outputFiles := runSSHCommand(t, session2, "/files")
	if !strings.Contains(outputFiles, "testfile.txt") {
		t.Errorf("Expected testfile.txt to be in output, but got:\n%s", outputFiles)
	}
}
