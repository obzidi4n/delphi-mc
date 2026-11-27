package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoBackup reports a missing backup row.
var ErrNoBackup = errors.New("no such backup")

// Backup is one snapshot of a server directory.
type Backup struct {
	ID        string
	OrgID     string
	ServerID  string
	NodeID    string
	State     string
	Size      int64
	SHA256    string
	Format    string
	Encrypted bool
	Error     string
	CreatedAt time.Time
	ReadyAt   *time.Time
}

const backupColumns = `id, org_id, server_id, node_id, state, size_bytes, sha256, format,
	encrypted, error, created_at, ready_at`

func scanBackup(row interface {
	Scan(dest ...any) error
}) (Backup, error) {
	var b Backup
	err := row.Scan(&b.ID, &b.OrgID, &b.ServerID, &b.NodeID, &b.State, &b.Size, &b.SHA256,
		&b.Format, &b.Encrypted, &b.Error, &b.CreatedAt, &b.ReadyAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Backup{}, ErrNoBackup
	}
	return b, err
}

// CreateBackup inserts a pending backup.
func (d *DB) CreateBackup(ctx context.Context, b Backup) (Backup, error) {
	if b.State == "" {
		b.State = "creating"
	}
	if b.Format == "" {
		b.Format = "tar.gz"
	}
	tag, err := d.pool.Exec(ctx, `
		insert into backups (id, org_id, server_id, node_id, state, format, encrypted)
		select $1, s.org_id, s.id, s.node_id, $3, $4, true
		from servers s where s.id = $2 and s.deleted_at is null`,
		b.ID, b.ServerID, b.State, b.Format)
	if err != nil {
		return Backup{}, fmt.Errorf("create backup: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Backup{}, ErrNoServer
	}
	return d.BackupByID(ctx, b.ID)
}

// BackupByID returns a backup without an org filter; callers authorize.
func (d *DB) BackupByID(ctx context.Context, id string) (Backup, error) {
	return scanBackup(d.pool.QueryRow(ctx, `select `+backupColumns+` from backups where id = $1`, id))
}

// ListBackups returns one server's backups, newest first.
func (d *DB) ListBackups(ctx context.Context, orgID, serverID string, limit int) ([]Backup, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := d.pool.Query(ctx, `
		select `+backupColumns+` from backups
		where org_id = $1 and server_id = $2
		order by created_at desc, id desc limit $3`, orgID, serverID, limit)
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	defer rows.Close()
	var out []Backup
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AllBackupsForServer returns every backup row for retention.
func (d *DB) AllBackupsForServer(ctx context.Context, serverID string) ([]Backup, error) {
	rows, err := d.pool.Query(ctx, `
		select `+backupColumns+` from backups where server_id = $1
		order by created_at desc, id desc`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	defer rows.Close()
	var out []Backup
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// MarkBackupReady records the uploaded artifact metadata.
func (d *DB) MarkBackupReady(ctx context.Context, id string, size int64, sha string) error {
	tag, err := d.pool.Exec(ctx, `
		update backups set state = 'ready', size_bytes = $2, sha256 = $3, ready_at = now(), error = ''
		where id = $1 and state = 'creating'`, id, size, sha)
	if err != nil {
		return fmt.Errorf("mark backup ready: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoBackup
	}
	return nil
}

// FailBackup records a failed backup.
func (d *DB) FailBackup(ctx context.Context, id, message string) error {
	_, err := d.pool.Exec(ctx,
		`update backups set state = 'failed', error = $2 where id = $1`, id, message)
	if err != nil {
		return fmt.Errorf("fail backup: %w", err)
	}
	return nil
}

// DeleteBackup removes one backup row after its file is removed.
func (d *DB) DeleteBackup(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx, `delete from backups where id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete backup: %w", err)
	}
	return nil
}
