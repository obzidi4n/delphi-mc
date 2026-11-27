package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// FileRevision is one stored version of a server file.
type FileRevision struct {
	ID        string
	OrgID     string
	ServerID  string
	Path      string
	Revision  int
	Content   string
	SHA256    string
	Author    string
	CreatedAt time.Time
}

// ErrNoRevision reports a missing file revision.
var ErrNoRevision = errors.New("file revision not found")

// AddFileRevision stores content as the next revision and prunes old ones.
func (d *DB) AddFileRevision(ctx context.Context, orgID, serverID, path, content, sha, author string) (FileRevision, error) {
	var result FileRevision
	for attempt := 0; attempt < 3; attempt++ {
		err := d.pool.QueryRow(ctx, `
			insert into file_revisions (id, org_id, server_id, path, revision, content, sha256, author)
			select $1, $2, $3, $4, coalesce(max(revision), 0) + 1, $5, $6, $7
			from file_revisions where server_id = $3 and path = $4
			returning id, org_id, server_id, path, revision, content, sha256, author, created_at`,
			newID("fr"), orgID, serverID, path, content, sha, author).
			Scan(&result.ID, &result.OrgID, &result.ServerID, &result.Path, &result.Revision,
				&result.Content, &result.SHA256, &result.Author, &result.CreatedAt)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				continue // concurrent write; retry
			}
			return FileRevision{}, fmt.Errorf("add file revision: %w", err)
		}
		_, _ = d.pool.Exec(ctx, `
			delete from file_revisions
			where server_id = $1 and path = $2 and revision <= $3 - 50`,
			serverID, path, result.Revision)
		return result, nil
	}
	return FileRevision{}, errors.New("could not allocate a file revision")
}

// ListFileRevisions returns metadata, newest first (content omitted).
func (d *DB) ListFileRevisions(ctx context.Context, orgID, serverID, path string, limit int) ([]FileRevision, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := d.pool.Query(ctx, `
		select id, org_id, server_id, path, revision, '', sha256, author, created_at
		from file_revisions
		where org_id = $1 and server_id = $2 and path = $3
		order by revision desc limit $4`, orgID, serverID, path, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileRevision
	for rows.Next() {
		var r FileRevision
		if err := rows.Scan(&r.ID, &r.OrgID, &r.ServerID, &r.Path, &r.Revision, &r.Content,
			&r.SHA256, &r.Author, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FileRevisionAt loads one revision with its content.
func (d *DB) FileRevisionAt(ctx context.Context, orgID, serverID, path string, revision int) (FileRevision, error) {
	var r FileRevision
	err := d.pool.QueryRow(ctx, `
		select id, org_id, server_id, path, revision, content, sha256, author, created_at
		from file_revisions
		where org_id = $1 and server_id = $2 and path = $3 and revision = $4`,
		orgID, serverID, path, revision).
		Scan(&r.ID, &r.OrgID, &r.ServerID, &r.Path, &r.Revision, &r.Content, &r.SHA256, &r.Author, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FileRevision{}, ErrNoRevision
	}
	return r, err
}
