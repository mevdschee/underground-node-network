package fileserver

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/sftp"
)

// Handler only allows reading one specific file by its transferID
type Handler struct {
	BaseDir    string
	Filename   string // real filename relative to BaseDir
	TransferID string // identifier used in SFTP request
}

func (h *Handler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	// Only allow reading if the requested path matches the transferID (base)
	if filepath.Base(r.Filepath) != h.TransferID {
		return nil, fmt.Errorf("permission denied")
	}
	return os.Open(filepath.Join(h.BaseDir, h.Filename))
}

func (h *Handler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return nil, fmt.Errorf("permission denied")
}

func (h *Handler) Filecmd(r *sftp.Request) error {
	return fmt.Errorf("permission denied")
}

func (h *Handler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	if r.Method == "Stat" || r.Method == "Lstat" {
		if filepath.Base(r.Filepath) == h.TransferID {
			info, err := os.Stat(filepath.Join(h.BaseDir, h.Filename))
			if err != nil {
				return nil, err
			}
			// Use renamedFileInfo to hide the real name in Stat response
			return &lister{[]os.FileInfo{&renamedFileInfo{info, h.TransferID}}}, nil
		}
	}
	return nil, fmt.Errorf("permission denied")
}

type renamedFileInfo struct {
	os.FileInfo
	newName string
}

func (f *renamedFileInfo) Name() string { return f.newName }

type lister struct {
	infos []os.FileInfo
}

func (l *lister) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l.infos)) {
		return 0, io.EOF
	}
	n := copy(ls, l.infos[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}
