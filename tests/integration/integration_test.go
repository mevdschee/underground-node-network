package integration

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestIntegration_DownloadVerification(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "unn_test_download_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	epBin, clientBin := buildBinaries(t)

	clientName := "downloadroom"
	clientPort := 22224
	clientHostKey := filepath.Join(tempDir, "client_host_key")
	clientIdentity := "../../tests/integration/test_room_key"
	filesDir := filepath.Join(tempDir, "files")
	os.MkdirAll(filesDir, 0700)

	// Create a test file with predictable content
	testContent := "integrity test content " + time.Now().String()
	fileName := "test_integrity_test.txt"
	filePath := filepath.Join(filesDir, fileName)
	os.WriteFile(filePath, []byte(testContent), 0644)

	// Calculate expected SHA256
	h := sha256.New()
	h.Write([]byte(testContent))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	// Pre-register room name (as user) and test user
	usersDir := filepath.Join(tempDir, "users")
	os.MkdirAll(usersDir, 0700)
	roomKey, _ := os.ReadFile("../../tests/integration/test_room_key.pub")
	userKey, _ := os.ReadFile("../../tests/integration/test_user_key.pub")
	os.WriteFile(filepath.Join(usersDir, clientName), roomKey, 0600)
	os.WriteFile(filepath.Join(usersDir, "maurits"), userKey, 0600)

	epPort := 44324
	epHostKey := filepath.Join(tempDir, "ep_host_key")
	epProcess := startEntryPoint(t, epBin, epPort, epHostKey, usersDir)
	defer epProcess.Stop()

	clientProcess := startClient(t, clientBin, clientName, clientPort, "localhost:44324", clientHostKey, clientIdentity, filesDir)
	defer clientProcess.Stop()

	time.Sleep(2 * time.Second)

	// Connect to entrypoint and join room
	sshClient, session := getSSHClient(t, "localhost:44324", "maurits", "../../tests/integration/test_user_key")
	defer sshClient.Close()
	defer session.Close()

	// Trigger download
	fmt.Printf("Triggering download of %s...\n", fileName)
	runSSHCommand(t, session, clientName)
	// We need another session to send the /download command because the first one might be in TUI mode
	// Wait, runSSHCommand sends a command and closes. But for TUI interaction it might be tricky.

	sshClient2, session2 := getSSHClient(t, "localhost:44324", "maurits", "../../tests/integration/test_user_key")
	defer sshClient2.Close()
	defer session2.Close()

	// Join room
	runSSHCommand(t, session2, clientName)
	time.Sleep(1 * time.Second)

	// In a real TUI we'd send /download, but these tests use a simplified interaction.
	// Let's see if we can just connect to the room node directly and trigger it.
	sshClientRoom, sessionRoom := getSSHClient(t, fmt.Sprintf("localhost:%d", clientPort), "maurits", "../../tests/integration/test_user_key")
	defer sshClientRoom.Close()
	defer sessionRoom.Close()

	fmt.Printf("Sending /download %s to room server...\n", fileName)
	outputDownload := runSSHCommand(t, sessionRoom, "/download "+fileName)

	// Verify [DOWNLOAD FILE] block contains correct SHA256
	if !strings.Contains(outputDownload, "[DOWNLOAD FILE]") {
		t.Errorf("Expected [DOWNLOAD FILE] block in output, but got:\n%s", outputDownload)
	}

	if !strings.Contains(outputDownload, expectedSig) {
		t.Errorf("Expected SHA256 %s to be in download block, but it was missing.\nOutput:\n%s", expectedSig, outputDownload)
	}

	fmt.Printf("Verified: SHA256 %s found in download block.\n", expectedSig)
}
