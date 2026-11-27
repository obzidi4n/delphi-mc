package wire

const (
	TypeBackupCreate  = "backup.create"
	TypeBackupRestore = "backup.restore"
	TypeBackupResult  = "backup.result"
)

// BackupRequest asks a node to snapshot or restore one server.
type BackupRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	BackupID  string `json:"backup_id"`
	ServerID  string `json:"server_id"`
}

// BackupResult reports the outcome of a snapshot or restore.
type BackupResult struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	OK        bool   `json:"ok"`
	State     string `json:"state,omitempty"`
	Error     string `json:"error,omitempty"`
	Size      int64  `json:"size,omitempty"`
}
