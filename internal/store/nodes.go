package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Enrollment and node lookup failures.
var (
	ErrInvalidToken = errors.New("invalid enrollment token")
	ErrTokenExpired = errors.New("enrollment token expired")
	ErrTokenUsed    = errors.New("enrollment token already used")
	ErrNoNode       = errors.New("node not found")
	ErrNodeRevoked  = errors.New("node revoked")
)

// Node is a registered agent, identified by the certificate it presented at
// enrollment.
type Node struct {
	ID            string
	OrgID         string
	Name          string
	Status        string
	AgentVersion  string
	FSCapability  string
	Capacity      []byte
	CertSerial    string
	CertExpiresAt time.Time
	RevokedAt     *time.Time
	LastSeen      *time.Time
	CreatedAt     time.Time
}

// CreateEnrollmentToken issues a single-use token for orgID.
func (d *DB) CreateEnrollmentToken(ctx context.Context, orgID string, ttl time.Duration) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	expires := time.Now().Add(ttl).Truncate(time.Second)

	if _, err := d.pool.Exec(ctx,
		`insert into enrollment_tokens (token_hash, org_id, expires_at) values ($1, $2, $3)`,
		hash[:], orgID, expires); err != nil {
		return "", time.Time{}, fmt.Errorf("store token: %w", err)
	}
	return token, expires, nil
}

// EnrollNode consumes token and registers node in one transaction. The node's
// org comes from the token, and its status starts at pending.
func (d *DB) EnrollNode(ctx context.Context, token string, node Node) (Node, error) {
	hash := sha256.Sum256([]byte(token))
	now := time.Now()

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return Node{}, fmt.Errorf("begin enroll: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var orgID string
	var expires time.Time
	var used *time.Time
	err = tx.QueryRow(ctx,
		`select org_id, expires_at, used_at from enrollment_tokens where token_hash = $1 for update`,
		hash[:]).Scan(&orgID, &expires, &used)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Node{}, ErrInvalidToken
	case err != nil:
		return Node{}, fmt.Errorf("read token: %w", err)
	case used != nil:
		return Node{}, ErrTokenUsed
	case now.After(expires):
		return Node{}, ErrTokenExpired
	}

	if _, err := tx.Exec(ctx, `update enrollment_tokens set used_at = now() where token_hash = $1`, hash[:]); err != nil {
		return Node{}, fmt.Errorf("consume token: %w", err)
	}

	node.OrgID = orgID
	node.Status = "pending"
	if len(node.Capacity) == 0 {
		node.Capacity = []byte(`{}`)
	}
	err = tx.QueryRow(ctx, `
		insert into nodes (id, org_id, name, status, fs_capability, capacity, cert_serial, cert_expires_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		returning created_at`,
		node.ID, node.OrgID, node.Name, node.Status, node.FSCapability,
		node.Capacity, node.CertSerial, node.CertExpiresAt).Scan(&node.CreatedAt)
	if err != nil {
		return Node{}, fmt.Errorf("insert node: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Node{}, fmt.Errorf("commit enroll: %w", err)
	}
	return node, nil
}

const nodeColumns = `id, org_id, name, status, agent_version, fs_capability, capacity,
	cert_serial, cert_expires_at, revoked_at, last_seen, created_at`

func scanNode(row pgx.Row) (Node, error) {
	var n Node
	err := row.Scan(&n.ID, &n.OrgID, &n.Name, &n.Status, &n.AgentVersion, &n.FSCapability,
		&n.Capacity, &n.CertSerial, &n.CertExpiresAt, &n.RevokedAt, &n.LastSeen, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, ErrNoNode
	}
	return n, err
}

// NodeBySerial finds the node that owns a certificate serial.
func (d *DB) NodeBySerial(ctx context.Context, serial string) (Node, error) {
	return scanNode(d.pool.QueryRow(ctx, `select `+nodeColumns+` from nodes where cert_serial = $1`, serial))
}

// NodeByID finds a node by id.
func (d *DB) NodeByID(ctx context.Context, id string) (Node, error) {
	return scanNode(d.pool.QueryRow(ctx, `select `+nodeColumns+` from nodes where id = $1`, id))
}

// TouchNode records a heartbeat, marking the node healthy. A revoked node is
// reported as revoked so the channel can be closed.
func (d *DB) TouchNode(ctx context.Context, id string, at time.Time) error {
	tag, err := d.pool.Exec(ctx,
		`update nodes set last_seen = $2, status = 'healthy' where id = $1 and revoked_at is null`,
		id, at)
	if err != nil {
		return fmt.Errorf("touch node: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNodeRevoked
	}
	return nil
}

// UpdateNodeHello records the agent's advertised version, filesystem
// capability, and capacity.
func (d *DB) UpdateNodeHello(ctx context.Context, id, agentVersion, fsCapability string, capacity []byte) error {
	if len(capacity) == 0 {
		capacity = []byte(`{}`)
	}
	tag, err := d.pool.Exec(ctx,
		`update nodes set agent_version = $2, fs_capability = $3, capacity = $4 where id = $1`,
		id, agentVersion, fsCapability, capacity)
	if err != nil {
		return fmt.Errorf("update node hello: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoNode
	}
	return nil
}

// SetNodeStatus writes a lifecycle status (pending, healthy, unhealthy,
// disconnected, revoked).
func (d *DB) SetNodeStatus(ctx context.Context, id, status string) error {
	_, err := d.pool.Exec(ctx, `update nodes set status = $2 where id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("set node status: %w", err)
	}
	return nil
}

// ListNodes returns the fleet ordered by name.
func (d *DB) ListNodes(ctx context.Context, limit int, after string) ([]Node, string, error) {
	parts, err := decodeCursor(after, 2)
	if err != nil {
		return nil, "", err
	}
	var afterName, afterID string
	if parts != nil {
		afterName, afterID = parts[0], parts[1]
	}
	rows, err := d.pool.Query(ctx, `
		select `+nodeColumns+` from nodes
		where ($1 = '' or (name, id) > ($1, $2))
		order by name, id limit $3`, afterName, afterID, pageQuerySize(limit))
	if err != nil {
		return nil, "", fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, n)
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

// RevokeNode permanently revokes a node's certificate.
func (d *DB) RevokeNode(ctx context.Context, id string) error {
	tag, err := d.pool.Exec(ctx,
		`update nodes set revoked_at = now(), status = 'revoked' where id = $1 and revoked_at is null`, id)
	if err != nil {
		return fmt.Errorf("revoke node: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoNode
	}
	return nil
}

// SweepStaleNodes marks healthy nodes unhealthy when their last heartbeat is
// older than cutoff. It returns how many transitioned.
func (d *DB) SweepStaleNodes(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := d.pool.Exec(ctx,
		`update nodes set status = 'unhealthy'
		 where status = 'healthy' and revoked_at is null
		   and (last_seen is null or last_seen < $1)`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("sweep nodes: %w", err)
	}
	return tag.RowsAffected(), nil
}
