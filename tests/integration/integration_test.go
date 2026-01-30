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

	"golang.org/x/crypto/ssh"
)

func TestIntegration_BasicRegistration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "unn_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	epBin, roomBin := buildBinaries(t)

	roomName := "testroom1"
	roomPort := 22223
	roomHostKey := filepath.Join(tempDir, "room_host_key")
	clientIdentity := "../../tests/integration/test_room_key"
	filesDir := filepath.Join(tempDir, "files")
	os.MkdirAll(filesDir, 0700)
	os.WriteFile(filepath.Join(filesDir, "testfile.txt"), []byte("hello world"), 0644)

	roomKey, _ := os.ReadFile("../../tests/integration/test_room_key.pub")
	userKey, _ := os.ReadFile("../../tests/integration/test_user_key.pub")

	roomPubKeyRaw, _, _, _, _ := ssh.ParseAuthorizedKey(roomKey)
	userPubKeyRaw, _, _, _, _ := ssh.ParseAuthorizedKey(userKey)

	roomHash := sha256.Sum256(roomPubKeyRaw.Marshal())
	userHash := sha256.Sum256(userPubKeyRaw.Marshal())

	// User storage format: hash unn_username platform_username@platform
	os.WriteFile(filepath.Join(tempDir, "users"), []byte(fmt.Sprintf("%x testroom1 testroom1@github\n%x maurits maurits@github\n", roomHash, userHash)), 0600)

	epPort := 44323
	epHostKey := filepath.Join(tempDir, "ep_host_key")
	epProcess := startEntryPoint(t, epBin, epPort, epHostKey, tempDir)
	defer epProcess.Stop()

	roomProcess := startRoom(t, roomBin, roomName, roomPort, "localhost:44323", roomHostKey, clientIdentity, filesDir)
	defer roomProcess.Stop()

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
	if !strings.Contains(output, roomName) {
		t.Errorf("Expected room %s to be in output, but got:\n%s", roomName, output)
	}

	// Join room to trigger P2P authorization
	fmt.Printf("Joining room %s via entrypoint...\n", roomName)
	sshClientJoin, sessionJoin := getSSHClient(t, "localhost:44323", "maurits", "../../tests/integration/test_user_key")
	defer sshClientJoin.Close()
	defer sessionJoin.Close()
	runSSHCommand(t, sessionJoin, "/join "+roomName)

	time.Sleep(2 * time.Second)

	// Connect to the room node directly (since entrypoint doesn't proxy)
	fmt.Printf("Connecting directly to room at localhost:%d...\n", roomPort)
	sshClient2, session2 := getSSHClient(t, fmt.Sprintf("localhost:%d", roomPort), "maurits", "../../tests/integration/test_user_key")
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

	epBin, roomBin := buildBinaries(t)

	roomName := "downloadroom"
	roomPort := 22224
	roomHostKey := filepath.Join(tempDir, "room_host_key")
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

	roomKey, _ := os.ReadFile("../../tests/integration/test_room_key.pub")
	userKey, _ := os.ReadFile("../../tests/integration/test_user_key.pub")

	roomPubKeyRaw, _, _, _, _ := ssh.ParseAuthorizedKey(roomKey)
	userPubKeyRaw, _, _, _, _ := ssh.ParseAuthorizedKey(userKey)

	roomHash := sha256.Sum256(roomPubKeyRaw.Marshal())
	userHash := sha256.Sum256(userPubKeyRaw.Marshal())

	// User storage format: hash unn_username platform_username@platform
	os.WriteFile(filepath.Join(tempDir, "users"), []byte(fmt.Sprintf("%x downloadroom downloadroom@github\n%x maurits maurits@github\n", roomHash, userHash)), 0600)

	epPort := 44324
	epHostKey := filepath.Join(tempDir, "ep_host_key")
	epProcess := startEntryPoint(t, epBin, epPort, epHostKey, tempDir)
	defer epProcess.Stop()

	roomProcess := startRoom(t, roomBin, roomName, roomPort, "localhost:44324", roomHostKey, clientIdentity, filesDir)
	defer roomProcess.Stop()

	time.Sleep(2 * time.Second)

	// Connect to entrypoint and join room
	sshClient, session := getSSHClient(t, "localhost:44324", "maurits", "../../tests/integration/test_user_key")
	defer sshClient.Close()
	defer session.Close()

	// Join room
	runSSHCommand(t, session, "/join "+roomName)
	time.Sleep(1 * time.Second)

	// Connect to the room node directly and trigger download
	sshClientRoom, sessionRoom := getSSHClient(t, fmt.Sprintf("localhost:%d", roomPort), "maurits", "../../tests/integration/test_user_key")
	defer sshClientRoom.Close()
	defer sessionRoom.Close()

	fmt.Printf("Sending /get %s to room server...\n", fileName)
	outputDownload := runSSHCommand(t, sessionRoom, "/get "+fileName)

	// Verify OSC 9 sequence contains correct SHA256
	if !strings.Contains(outputDownload, "\x1b]9;") {
		t.Errorf("Expected OSC 9 sequence in output, but got:\n%s", outputDownload)
	}

	if !strings.Contains(outputDownload, expectedSig) {
		t.Errorf("Expected SHA256 %s to be in download signaling, but it was missing.\nOutput:\n%s", expectedSig, outputDownload)
	}

	fmt.Printf("Verified: SHA256 %s found in download signaling.\n", expectedSig)
}
