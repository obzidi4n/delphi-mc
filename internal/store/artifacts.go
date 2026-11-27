package store

import (
	"context"
	"fmt"
	"time"
)

// Artifact is one sha256-addressed file in the panel's content store.
type Artifact struct {
	SHA256    string
	Filename  string
	Size      int64
	Kind      string
	Source    string
	CreatedAt time.Time
}

// RecordArtifact stores artifact metadata; the first writer wins because the
// content address is the identity.
func (d *DB) RecordArtifact(ctx context.Context, a Artifact) error {
	_, err := d.pool.Exec(ctx, `
		insert into artifacts (sha256, filename, size, kind, source)
		values ($1, $2, $3, $4, $5)
		on conflict (sha256) do nothing`,
		a.SHA256, a.Filename, a.Size, a.Kind, a.Source)
	if err != nil {
		return fmt.Errorf("record artifact: %w", err)
	}
	return nil
}

// ListArtifacts returns every artifact ordered oldest first.
func (d *DB) ListArtifacts(ctx context.Context) ([]Artifact, error) {
	rows, err := d.pool.Query(ctx, `
		select sha256, filename, size, kind, source, created_at
		from artifacts order by created_at, sha256`)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.SHA256, &a.Filename, &a.Size, &a.Kind, &a.Source, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteArtifact removes artifact metadata.
func (d *DB) DeleteArtifact(ctx context.Context, sha string) error {
	_, err := d.pool.Exec(ctx, `delete from artifacts where sha256 = $1`, sha)
	if err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return nil
}

// ArtifactRefs returns the sha256 values still referenced by live servers'
// managed plugins.
func (d *DB) ArtifactRefs(ctx context.Context) (map[string]struct{}, error) {
	rows, err := d.pool.Query(ctx, `
		select distinct p->>'sha256'
		from servers s, jsonb_array_elements(s.plugins) p
		where s.deleted_at is null and p->>'sha256' <> ''`)
	if err != nil {
		return nil, fmt.Errorf("artifact refs: %w", err)
	}
	defer rows.Close()
	refs := map[string]struct{}{}
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		refs[sha] = struct{}{}
	}
	return refs, rows.Err()
}
