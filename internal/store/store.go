// Package store owns the Postgres pool and the forward-only migration runner.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB is the panel's database handle. The pool connects lazily, so a panel can
// start (and serve /healthz) while Postgres is down.
type DB struct {
	pool *pgxpool.Pool
}

// Open parses the DSN and creates a lazy pool.
func Open(ctx context.Context, dsn string) (*DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("database_url is empty")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Ping reports whether the database is reachable now.
func (d *DB) Ping(ctx context.Context) error {
	return d.pool.Ping(ctx)
}

// Close releases the pool.
func (d *DB) Close() { d.pool.Close() }

// Migrate applies every pending migration in version order and returns the
// names it applied. It is safe to run on every start.
func (d *DB) Migrate(ctx context.Context) ([]string, error) {
	if _, err := d.pool.Exec(ctx, `
		create table if not exists schema_migrations (
			version    bigint primary key,
			name       text        not null,
			applied_at timestamptz not null default now()
		)`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := d.appliedVersions(ctx)
	if err != nil {
		return nil, err
	}
	pending, err := planMigrations(mustLoad(), applied)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, m := range pending {
		tx, err := d.pool.Begin(ctx)
		if err != nil {
			return names, fmt.Errorf("begin %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			_ = tx.Rollback(ctx)
			return names, fmt.Errorf("apply %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx,
			`insert into schema_migrations (version, name) values ($1, $2)`, m.version, m.name); err != nil {
			_ = tx.Rollback(ctx)
			return names, fmt.Errorf("record %s: %w", m.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return names, fmt.Errorf("commit %s: %w", m.name, err)
		}
		names = append(names, m.name)
	}
	return names, nil
}

func (d *DB) appliedVersions(ctx context.Context) (map[int64]bool, error) {
	rows, err := d.pool.Query(ctx, `select version from schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]bool)
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		out[v] = true
	}
	return out, rows.Err()
}

// migration is one embedded SQL file.
type migration struct {
	version int64
	name    string
	sql     string
}

// loadMigrations reads and validates the embedded set.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	out := make([]migration, 0, len(entries))
	seen := make(map[int64]string, len(entries))
	for _, name := range entries {
		base := name[len("migrations/"):]
		version, err := parseMigrationName(base)
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("duplicate migration version %d: %s and %s", version, prev, base)
		}
		seen[version] = base
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", base, err)
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			return nil, fmt.Errorf("migration %s is empty", base)
		}
		out = append(out, migration{version: version, name: base, sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func mustLoad() []migration {
	all, err := loadMigrations(migrationFS)
	if err != nil {
		panic("store: embedded migrations are invalid: " + err.Error())
	}
	return all
}

// parseMigrationName accepts NNNN_name.sql and returns NNNN.
func parseMigrationName(base string) (int64, error) {
	num, _, ok := strings.Cut(base, "_")
	if !ok || !strings.HasSuffix(base, ".sql") {
		return 0, fmt.Errorf("migration %q must be named NNNN_name.sql", base)
	}
	v, err := strconv.ParseInt(num, 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("migration %q has a non-numeric version", base)
	}
	return v, nil
}

// planMigrations filters out applied versions, preserving order.
func planMigrations(all []migration, applied map[int64]bool) ([]migration, error) {
	pending := make([]migration, 0, len(all))
	for _, m := range all {
		if applied[m.version] {
			continue
		}
		pending = append(pending, m)
	}
	return pending, nil
}
