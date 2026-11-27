package capability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"delphi.dev/delphi/internal/backup"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

// BackupKeep is the per-server retention count.
const BackupKeep = 7

// BackupStore is backup persistence plus the server lookups the capability
// needs.
type BackupStore interface {
	GetServer(ctx context.Context, id string) (store.Server, error)
	CreateBackup(ctx context.Context, b store.Backup) (store.Backup, error)
	BackupByID(ctx context.Context, id string) (store.Backup, error)
	ListBackups(ctx context.Context, orgID, serverID string, limit int) ([]store.Backup, error)
	AllBackupsForServer(ctx context.Context, serverID string) ([]store.Backup, error)
	MarkBackupReady(ctx context.Context, id string, size int64, sha string) error
	FailBackup(ctx context.Context, id, message string) error
	DeleteBackup(ctx context.Context, id string) error
	SetDesiredState(ctx context.Context, id, desired string, kill bool) (store.Server, error)
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// NodeCaller sends a request/response frame to a node channel.
type NodeCaller interface {
	Call(ctx context.Context, nodeID string, build func(requestID string) any, timeout time.Duration) (json.RawMessage, error)
}

// Backups creates, lists, restores, and prunes encrypted snapshots.
type Backups struct {
	Store  BackupStore
	Files  *backup.Store
	Hub    NodeCaller
	Notify Notifier
	Log    *slog.Logger
}

// Create snapshots a server and waits for the upload to land.
func (b *Backups) Create(ctx context.Context, p Principal, serverID string) (store.Backup, error) {
	if !p.Allows(serverScope(serverID, "update")) {
		return store.Backup{}, forbidden(serverScope(serverID, "update"))
	}
	srv, err := b.Store.GetServer(ctx, serverID)
	if err != nil {
		return store.Backup{}, err
	}
	if b.Hub == nil || b.Files == nil {
		return store.Backup{}, errors.New("backup service is not configured")
	}
	id, err := newBackupID()
	if err != nil {
		return store.Backup{}, err
	}
	row, err := b.Store.CreateBackup(ctx, store.Backup{ID: id, ServerID: serverID})
	if err != nil {
		return store.Backup{}, err
	}
	raw, err := b.Hub.Call(ctx, srv.NodeID, func(requestID string) any {
		return wire.BackupRequest{
			Type: wire.TypeBackupCreate, RequestID: requestID,
			BackupID: id, ServerID: serverID,
		}
	}, 30*time.Minute)
	if err != nil {
		_ = b.Store.FailBackup(ctx, id, err.Error())
		return store.Backup{}, err
	}
	var result wire.BackupResult
	if err := json.Unmarshal(raw, &result); err != nil {
		_ = b.Store.FailBackup(ctx, id, err.Error())
		return store.Backup{}, err
	}
	if !result.OK {
		_ = b.Store.FailBackup(ctx, id, result.Error)
		return store.Backup{}, fmt.Errorf("backup failed: %s", result.Error)
	}
	row, err = b.Store.BackupByID(ctx, id)
	if err != nil {
		return store.Backup{}, err
	}
	if row.State != "ready" {
		_ = b.Store.FailBackup(ctx, id, "the node uploaded no backup")
		return store.Backup{}, errors.New("the node uploaded no backup")
	}
	_ = b.Store.Audit(ctx, srv.OrgID, p.UserID, p.Via, "backup.create", id, map[string]any{
		"server_id": serverID, "size_bytes": row.Size, "sha256": row.SHA256,
	})
	b.prune(ctx, serverID)
	return row, nil
}

// List returns a server's snapshots.
func (b *Backups) List(ctx context.Context, p Principal, serverID string) ([]store.Backup, error) {
	if !p.Allows(serverScope(serverID, "read")) {
		return nil, forbidden(serverScope(serverID, "read"))
	}
	srv, err := b.Store.GetServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return b.Store.ListBackups(ctx, srv.OrgID, serverID, 100)
}

// Restore replaces a server directory with a snapshot and bumps the desired
// revision so the node reconciles (and restarts a running server).
func (b *Backups) Restore(ctx context.Context, p Principal, backupID string) (store.Backup, error) {
	row, err := b.Store.BackupByID(ctx, backupID)
	if err != nil {
		return store.Backup{}, err
	}
	if !p.Allows(serverScope(row.ServerID, "update")) {
		return store.Backup{}, forbidden(serverScope(row.ServerID, "update"))
	}
	if row.State != "ready" {
		return store.Backup{}, invalid("backup is not ready")
	}
	srv, err := b.Store.GetServer(ctx, row.ServerID)
	if err != nil {
		return store.Backup{}, err
	}
	if srv.OrgID != p.OrgID {
		return store.Backup{}, store.ErrNoBackup
	}
	if b.Hub == nil {
		return store.Backup{}, errors.New("backup service is not configured")
	}
	raw, err := b.Hub.Call(ctx, srv.NodeID, func(requestID string) any {
		return wire.BackupRequest{
			Type: wire.TypeBackupRestore, RequestID: requestID,
			BackupID: backupID, ServerID: srv.ID,
		}
	}, 30*time.Minute)
	if err != nil {
		return store.Backup{}, err
	}
	var result wire.BackupResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return store.Backup{}, err
	}
	if !result.OK {
		return store.Backup{}, fmt.Errorf("restore failed: %s", result.Error)
	}
	updated, err := b.Store.SetDesiredState(ctx, srv.ID, srv.DesiredState, false)
	if err != nil {
		return store.Backup{}, err
	}
	if b.Notify != nil {
		_ = b.Notify.Sync(ctx, updated.NodeID)
	}
	_ = b.Store.Audit(ctx, srv.OrgID, p.UserID, p.Via, "backup.restore", backupID, map[string]any{
		"server_id": srv.ID, "revision": updated.DesiredRevision,
	})
	return row, nil
}

// Delete removes a snapshot.
func (b *Backups) Delete(ctx context.Context, p Principal, backupID string) error {
	row, err := b.Store.BackupByID(ctx, backupID)
	if err != nil {
		return err
	}
	if !p.Allows(serverScope(row.ServerID, "update")) {
		return forbidden(serverScope(row.ServerID, "update"))
	}
	if row.OrgID != p.OrgID {
		return store.ErrNoBackup
	}
	if b.Files != nil {
		_ = b.Files.Remove(row.ID)
	}
	if err := b.Store.DeleteBackup(ctx, row.ID); err != nil {
		return err
	}
	_ = b.Store.Audit(ctx, row.OrgID, p.UserID, p.Via, "backup.delete", row.ID, map[string]any{"server_id": row.ServerID})
	return nil
}

// Upload encrypts and stores a node-uploaded archive. It is called by the
// agent HTTP surface after certificate and ownership checks.
func (b *Backups) Upload(ctx context.Context, nodeID, backupID string, src io.Reader, wantSHA string) (store.Backup, error) {
	if b.Files == nil {
		return store.Backup{}, errors.New("backup storage is not configured")
	}
	row, err := b.Store.BackupByID(ctx, backupID)
	if err != nil {
		return store.Backup{}, err
	}
	if row.NodeID != nodeID {
		return store.Backup{}, store.ErrNoBackup
	}
	if row.State != "creating" {
		return store.Backup{}, invalid("backup is not accepting an upload")
	}
	sha, size, err := b.Files.Encrypt(backupID, src)
	if err != nil {
		_ = b.Store.FailBackup(ctx, backupID, err.Error())
		return store.Backup{}, err
	}
	if wantSHA != "" && wantSHA != sha {
		_ = b.Files.Remove(backupID)
		_ = b.Store.FailBackup(ctx, backupID, "sha256 mismatch")
		return store.Backup{}, errors.New("backup sha256 mismatch")
	}
	if err := b.Store.MarkBackupReady(ctx, backupID, size, sha); err != nil {
		return store.Backup{}, err
	}
	return b.Store.BackupByID(ctx, backupID)
}

// Download streams a decrypted backup to an authorized node.
func (b *Backups) Download(nodeID, backupID string, dst io.Writer) error {
	if b.Files == nil {
		return errors.New("backup storage is not configured")
	}
	row, err := b.Store.BackupByID(context.Background(), backupID)
	if err != nil {
		return err
	}
	if row.NodeID != nodeID || row.State != "ready" {
		return store.ErrNoBackup
	}
	return b.Files.Decrypt(backupID, dst)
}

func (b *Backups) prune(ctx context.Context, serverID string) {
	rows, err := b.Store.AllBackupsForServer(ctx, serverID)
	if err != nil {
		b.log().Warn("backup prune list", "server", serverID, "err", err)
		return
	}
	for i, row := range rows {
		if i < BackupKeep {
			continue
		}
		if b.Files != nil {
			_ = b.Files.Remove(row.ID)
		}
		_ = b.Store.DeleteBackup(ctx, row.ID)
	}
}

func (b *Backups) log() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

func newBackupID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "b_" + hex.EncodeToString(buf), nil
}
