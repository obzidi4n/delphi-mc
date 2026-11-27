package wire

import (
	"time"

	"delphi.dev/delphi/internal/nbt"
)

// File request/result frames (panel asks the node that owns the server).
const (
	TypeFileRequest = "file.request"
	TypeFileResult  = "file.result"
)

// FileRequest asks one node to list, read, or write under a server directory.
type FileRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	ServerID  string `json:"server_id"`
	Op        string `json:"op"` // list | read | write | nbt | download
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"`
}

// FileEntry is one directory entry.
type FileEntry struct {
	Name    string    `json:"name"`
	Dir     bool      `json:"dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// FileResult is the node's answer.
type FileResult struct {
	Type      string        `json:"type"`
	RequestID string        `json:"request_id"`
	Error     string        `json:"error,omitempty"`
	Entries   []FileEntry   `json:"entries,omitempty"`
	Content   string        `json:"content,omitempty"`
	Data      []byte        `json:"data,omitempty"`
	NBT       *nbt.Document `json:"nbt,omitempty"`
	SHA256    string        `json:"sha256,omitempty"`
	Size      int64         `json:"size,omitempty"`
	ModTime   time.Time     `json:"mod_time,omitempty"`
}
