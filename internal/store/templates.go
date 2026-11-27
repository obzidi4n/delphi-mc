package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Template errors.
var (
	ErrNoTemplate    = errors.New("template not found")
	ErrTemplateInUse = errors.New("template name already used in this org")
)

// Template is a stored template body plus its metadata.
type Template struct {
	ID        string
	OrgID     string
	Name      string
	Revision  int
	Body      []byte
	Builtin   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

const templateColumns = `id, coalesce(org_id, ''), name, revision, body, builtin, created_at, updated_at`

func scanTemplate(row pgx.Row) (Template, error) {
	var t Template
	var body []byte
	err := row.Scan(&t.ID, &t.OrgID, &t.Name, &t.Revision, &body, &t.Builtin, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Template{}, ErrNoTemplate
	}
	t.Body = body
	return t, err
}

// ListTemplates returns built-ins and the org's templates ordered by name.
func (d *DB) ListTemplates(ctx context.Context, orgID string, limit int, after string) ([]Template, string, error) {
	parts, err := decodeCursor(after, 2)
	if err != nil {
		return nil, "", err
	}
	var afterName, afterID string
	if parts != nil {
		afterName, afterID = parts[0], parts[1]
	}
	rows, err := d.pool.Query(ctx, `
		select `+templateColumns+` from templates
		where (builtin or org_id = $1) and ($2 = '' or (name, id) > ($2, $3))
		order by name, id limit $4`, orgID, afterName, afterID, pageQuerySize(limit))
	if err != nil {
		return nil, "", fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var out []Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(out) <= pageLimit(limit) {
		return out, "", nil
	}
	kept := out[:pageLimit(limit)]
	last := kept[len(kept)-1]
	return kept, encodeCursor(last.Name, last.ID), nil
}

// TemplateByID returns a built-in or an org template.
func (d *DB) TemplateByID(ctx context.Context, orgID, id string) (Template, error) {
	return scanTemplate(d.pool.QueryRow(ctx, `
		select `+templateColumns+` from templates
		where id = $1 and (builtin or org_id = $2)`, id, orgID))
}

// ImportTemplate stores a validated body under the org.
func (d *DB) ImportTemplate(ctx context.Context, orgID, name string, body []byte) (Template, error) {
	t, err := scanTemplate(d.pool.QueryRow(ctx, `
		insert into templates (id, org_id, name, body)
		values ($1, $2, $3, $4)
		returning `+templateColumns,
		newID("t"), orgID, name, body))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Template{}, ErrTemplateInUse
		}
		return Template{}, fmt.Errorf("import template: %w", err)
	}
	return t, nil
}

// UpsertBuiltinTemplate seeds an embedded built-in, bumping its revision when
// the embedded body changed. Org templates are never touched.
func (d *DB) UpsertBuiltinTemplate(ctx context.Context, id, name string, body []byte) error {
	if !json.Valid(body) {
		return errors.New("builtin template body is not JSON")
	}
	_, err := d.pool.Exec(ctx, `
		insert into templates (id, org_id, name, body, builtin)
		values ($1, null, $2, $3, true)
		on conflict (id) do update
		set name = excluded.name, body = excluded.body,
		    revision = templates.revision + 1, updated_at = now()
		where templates.builtin and templates.body <> excluded.body`,
		id, name, body)
	return err
}
