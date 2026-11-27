package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoNetwork is a missing network.
var ErrNoNetwork = errors.New("no such network")

// Network is a proxy plus its backends on one container network.
type Network struct {
	ID            string
	OrgID         string
	Name          string
	ProxyServerID string
	Mode          string
	CreatedAt     time.Time
}

const networkColumns = `id, org_id, name, proxy_server_id, mode, created_at`

func scanNetwork(row interface{ Scan(dest ...any) error }) (Network, error) {
	var n Network
	err := row.Scan(&n.ID, &n.OrgID, &n.Name, &n.ProxyServerID, &n.Mode, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Network{}, ErrNoNetwork
	}
	return n, err
}

// CreateNetwork inserts a network row.
func (d *DB) CreateNetwork(ctx context.Context, n Network) (Network, error) {
	if n.Mode == "" {
		n.Mode = "legacy"
	}
	err := d.pool.QueryRow(ctx, `
		insert into networks (id, org_id, name, mode)
		values ($1, $2, $3, $4) returning created_at`,
		n.ID, n.OrgID, n.Name, n.Mode).Scan(&n.CreatedAt)
	if err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return Network{}, ErrNameInUse
		}
		return Network{}, fmt.Errorf("create network: %w", err)
	}
	return n, nil
}

// SetNetworkProxy records the proxy server.
func (d *DB) SetNetworkProxy(ctx context.Context, id, proxyServerID string) error {
	_, err := d.pool.Exec(ctx, `update networks set proxy_server_id = $2 where id = $1`, id, proxyServerID)
	return err
}

// ListNetworks returns an org's networks.
func (d *DB) ListNetworks(ctx context.Context, orgID string) ([]Network, error) {
	rows, err := d.pool.Query(ctx,
		`select `+networkColumns+` from networks where org_id = $1 order by name, id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Network
	for rows.Next() {
		n, err := scanNetwork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// NetworkByID returns a network.
func (d *DB) NetworkByID(ctx context.Context, orgID, id string) (Network, error) {
	return scanNetwork(d.pool.QueryRow(ctx,
		`select `+networkColumns+` from networks where org_id = $1 and id = $2`, orgID, id))
}

// NetworkServers returns the servers attached to a network.
func (d *DB) NetworkServers(ctx context.Context, networkID string) ([]Server, error) {
	return d.queryServers(ctx,
		`select `+serverColumns+` from servers s
		 where s.network_id = $1 and s.deleted_at is null
		 order by s.network_role desc, s.name, s.id`, networkID)
}

// SetNetworkConfig records a server's network role and rendered files.
func (d *DB) SetNetworkConfig(ctx context.Context, id, networkID, role string, files map[string]string) (Server, error) {
	if files == nil {
		files = map[string]string{}
	}
	tag, err := d.pool.Exec(ctx, `
		update servers set network_id = $2, network_role = $3, files = $4,
			desired_revision = desired_revision + 1
		where id = $1 and deleted_at is null`, id, networkID, role, files)
	if err != nil {
		return Server{}, fmt.Errorf("set network config: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Server{}, ErrNoServer
	}
	return d.GetServer(ctx, id)
}
