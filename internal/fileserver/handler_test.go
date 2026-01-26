package fileserver

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

func TestHandler(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "unn-fileserver-test-*")
	defer os.RemoveAll(tmpDir)

	filesDir := filepath.Join(tmpDir, "files")
	os.MkdirAll(filesDir, 0755)

	testFile := "hello.txt"
	testContent := "hello sftp"
	os.WriteFile(filepath.Join(filesDir, testFile), []byte(testContent), 0644)
	testTransferID := "550e8400-e29b-41d4-a716-446655440000"

	handler := &Handler{
		BaseDir:    filesDir,
		Filename:   testFile,
		TransferID: testTransferID,
	}

	t.Run("Fileread - valid path (UUID)", func(t *testing.T) {
		req := &sftp.Request{
			Method:   "Get",
			Filepath: "/" + testTransferID,
		}
		reader, err := handler.Fileread(req)
		if err != nil {
			t.Fatalf("Failed to open file: %v", err)
		}
		defer reader.(io.Closer).Close()

		buf := make([]byte, len(testContent))
		n, err := reader.ReadAt(buf, 0)
		if err != nil && err != io.EOF {
			t.Fatalf("Failed to read file: %v", err)
		}
		if string(buf[:n]) != testContent {
			t.Errorf("Expected content %q, got %q", testContent, string(buf[:n]))
		}
	})

	t.Run("Fileread - original filename denied", func(t *testing.T) {
		req := &sftp.Request{
			Method:   "Get",
			Filepath: "/hello.txt",
		}
		_, err := handler.Fileread(req)
		if err == nil {
			t.Errorf("Expected error for original filename, but opened something")
		}
	})

	t.Run("Filewrite - permission denied", func(t *testing.T) {
		req := &sftp.Request{
			Method:   "Put",
			Filepath: "/" + testTransferID,
		}
		_, err := handler.Filewrite(req)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("Expected permission denied error, got %v", err)
		}
	})

	t.Run("Filelist - Stat success (UUID)", func(t *testing.T) {
		req := &sftp.Request{
			Method:   "Stat",
			Filepath: "/" + testTransferID,
		}
		lister, err := handler.Filelist(req)
		if err != nil {
			t.Fatalf("Failed to stat file: %v", err)
		}

		buf := make([]os.FileInfo, 1)
		n, err := lister.ListAt(buf, 0)
		if n != 1 {
			t.Errorf("Expected 1 entry, got %d", n)
		}
		if buf[0].Name() != testTransferID {
			t.Errorf("Expected %s, got %s", testTransferID, buf[0].Name())
		}
	})
}
