package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Server errors.
var (
	ErrNoServer  = errors.New("server not found")
	ErrNoPort    = errors.New("no free port on the node")
	ErrPortInUse = errors.New("port already allocated on the node")
	ErrNameInUse = errors.New("server name already used on the node")
)

// Server is a managed Minecraft server. The panel owns desired state; the node
// reports observed state and the revision it applied.
type Server struct {
	ID              string
	OrgID           string
	NodeID          string
	Name            string
	DesiredState    string
	DesiredRevision int64
	ObservedState   string
	AppliedRevision int64
	KillRequested   bool
	AcceptEULA      bool
	Image           string
	JavaVersion     int
	MemoryMB        int
	CPUCores        float64
	Port            int
	JarURL          string
	JarSHA256       string
	LastError       string
	CreatedAt       time.Time
	DeletedAt       *time.Time
	PurgeAfter      *time.Time
	SummaryState    string
}

const serverColumns = `s.id, s.org_id, s.node_id, s.name, s.desired_state, s.desired_revision,
	s.observed_state, s.applied_revision, s.kill_requested, s.accept_eula, s.image, s.java_version,
	s.memory_mb, s.cpu_cores, s.port, s.jar_url, s.jar_sha256, s.last_error, s.created_at,
	s.deleted_at, s.purge_after,
	coalesce((select state from server_summary where server_id = s.id), s.observed_state)`

func scanServer(row pgx.Row) (Server, error) {
	var s Server
	err := row.Scan(&s.ID, &s.OrgID, &s.NodeID, &s.Name, &s.DesiredState, &s.DesiredRevision,
		&s.ObservedState, &s.AppliedRevision, &s.KillRequested, &s.AcceptEULA, &s.Image,
		&s.JavaVersion, &s.MemoryMB, &s.CPUCores, &s.Port, &s.JarURL, &s.JarSHA256,
		&s.LastError, &s.CreatedAt, &s.DeletedAt, &s.PurgeAfter, &s.SummaryState)
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{}, ErrNoServer
	}
	return s, err
}

// CreateServer inserts a server, taking org_id from its node. Port 0 asks the
// panel to allocate the lowest free port on that node.
func (d *DB) CreateServer(ctx context.Context, s Server) (Server, error) {
	if s.Port == 0 {
		err := d.pool.QueryRow(ctx, `
			select p from generate_series(25565, 25600) p
			where not exists (select 1 from servers where node_id = $1 and port = p)
			order by p limit 1`, s.NodeID).Scan(&s.Port)
		if errors.Is(err, pgx.ErrNoRows) {
			return Server{}, ErrNoPort
		}
		if err != nil {
			return Server{}, fmt.Errorf("allocate port: %w", err)
		}
	}

	if s.DesiredState == "" {
		s.DesiredState = "stopped"
	}
	tag, err := d.pool.Exec(ctx, `
		insert into servers (id, org_id, node_id, name, desired_state, desired_revision,
			observed_state, applied_revision, kill_requested, accept_eula, image, java_version,
			memory_mb, cpu_cores, port, jar_url, jar_sha256)
		select $1, n.org_id, n.id, $3, $4, 1, 'unknown', 0, false, $5, $6, $7, $8, $9, $10, $11, $12
		from nodes n where n.id = $2`,
		s.ID, s.NodeID, s.Name, s.DesiredState, s.AcceptEULA, s.Image, s.JavaVersion,
		s.MemoryMB, s.CPUCores, s.Port, s.JarURL, s.JarSHA256)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "servers_node_id_port_key":
				return Server{}, ErrPortInUse
			case "servers_node_id_name_key":
				return Server{}, ErrNameInUse
			}
		}
		return Server{}, fmt.Errorf("insert server: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Server{}, ErrNoNode
	}

	if _, err := d.pool.Exec(ctx,
		`insert into server_summary (server_id, state) values ($1, 'unknown') on conflict do nothing`,
		s.ID); err != nil {
		return Server{}, fmt.Errorf("insert summary: %w", err)
	}
	return d.GetServer(ctx, s.ID)
}

// GetServer returns one server.
func (d *DB) GetServer(ctx context.Context, id string) (Server, error) {
	return scanServer(d.pool.QueryRow(ctx, `select `+serverColumns+` from servers s where s.id = $1 and s.deleted_at is null`, id))
}

// ListServers returns live servers ordered by name, one keyset page at a time.
func (d *DB) ListServers(ctx context.Context, limit int, after string) ([]Server, string, error) {
	parts, err := decodeCursor(after, 2)
	if err != nil {
		return nil, "", err
	}
	var afterName, afterID string
	if parts != nil {
		afterName, afterID = parts[0], parts[1]
	}
	servers, err := d.queryServers(ctx, `
		select `+serverColumns+` from servers s
		where s.deleted_at is null and ($1 = '' or (s.name, s.id) > ($1, $2))
		order by s.name, s.id limit $3`, afterName, afterID, pageQuerySize(limit))
	if err != nil {
		return nil, "", err
	}
	kept, next := pageServers(servers, limit)
	return kept, next, nil
}

// ListServersByNode returns the desired set for one node, including tombstones
// the node has not acknowledged yet.
func (d *DB) ListServersByNode(ctx context.Context, nodeID string) ([]Server, error) {
	return d.queryServers(ctx,
		`select `+serverColumns+` from servers s
		 where s.node_id = $1 and s.observed_state <> 'deleted'
		 order by s.name, s.id`, nodeID)
}

func (d *DB) queryServers(ctx context.Context, query string, args ...any) ([]Server, error) {
	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	defer rows.Close()

	var out []Server
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetDesiredState changes the desired lifecycle and bumps the revision.
// Restart is expressed as desired running with a new revision.
func (d *DB) SetDesiredState(ctx context.Context, id, desired string, kill bool) (Server, error) {
	tag, err := d.pool.Exec(ctx, `
		update servers
		set desired_state = $2, kill_requested = $3, desired_revision = desired_revision + 1
		where id = $1 and deleted_at is null`, id, desired, kill)
	if err != nil {
		return Server{}, fmt.Errorf("set desired state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Server{}, ErrNoServer
	}
	return d.GetServer(ctx, id)
}

// RenameServer changes a server's display name.
func (d *DB) RenameServer(ctx context.Context, id, name string) (Server, error) {
	tag, err := d.pool.Exec(ctx,
		`update servers set name = $2 where id = $1 and deleted_at is null`, id, name)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Server{}, ErrNameInUse
		}
		return Server{}, fmt.Errorf("rename server: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Server{}, ErrNoServer
	}
	return d.GetServer(ctx, id)
}

// SoftDeleteServer stops the server, marks it deleted, and schedules its data
// for purge after seven days. The node removes the container on next sync.
func (d *DB) SoftDeleteServer(ctx context.Context, id string) (Server, error) {
	tag, err := d.pool.Exec(ctx, `
		update servers
		set desired_state = 'stopped', kill_requested = false,
		    desired_revision = desired_revision + 1,
		    deleted_at = now(), purge_after = now() + interval '7 days'
		where id = $1 and deleted_at is null`, id)
	if err != nil {
		return Server{}, fmt.Errorf("delete server: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Server{}, ErrNoServer
	}
	return d.GetServer(ctx, id)
}

// ReportServerState records what the node observed. The report is scoped to the
// reporting node, so an agent cannot speak for another node's servers.
func (d *DB) ReportServerState(ctx context.Context, nodeID, id, state string, revision int64, detail string) error {
	tag, err := d.pool.Exec(ctx, `
		update servers
		set observed_state = $3,
		    applied_revision = greatest(applied_revision, $4),
		    last_error = $5,
		    kill_requested = case when $3 in ('stopped', 'exited', 'error') then false else kill_requested end
		where id = $2 and node_id = $1`, nodeID, id, state, revision, detail)
	if err != nil {
		return fmt.Errorf("report server state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoServer
	}

	if _, err := d.pool.Exec(ctx, `
		insert into server_summary (server_id, state, updated_at)
		values ($1, $2, now())
		on conflict (server_id) do update set state = excluded.state, updated_at = now()`,
		id, state); err != nil {
		return fmt.Errorf("update summary: %w", err)
	}
	return nil
}
