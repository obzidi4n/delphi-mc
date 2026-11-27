package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"delphi.dev/delphi/internal/auth"
)

// Identity errors.
var (
	ErrEmailInUse = errors.New("email already registered in this org")
	ErrNoUser     = errors.New("user not found")
	ErrNoSession  = errors.New("session not found or expired")
	ErrNoAPIKey   = errors.New("api key not found or revoked")
	ErrGroupInUse = errors.New("group name already used")
	ErrNoGroup    = errors.New("group not found")
)

// User is a panel account.
type User struct {
	ID           string
	OrgID        string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// APIKey is a bearer credential for the CLI.
type APIKey struct {
	ID         string
	OrgID      string
	UserID     string
	Name       string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// AuditEntry is one recorded mutation.
type AuditEntry struct {
	Seq       int64           `json:"seq"`
	OrgID     string          `json:"org_id"`
	Actor     string          `json:"actor"`
	Via       string          `json:"via"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Diff      json.RawMessage `json:"diff"`
	CreatedAt time.Time       `json:"created_at"`
}

// Group is a targeting set of servers.
type Group struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateUser inserts an account, reporting duplicate emails.
func (d *DB) CreateUser(ctx context.Context, orgID, email, passwordHash string) (User, error) {
	var u User
	err := d.pool.QueryRow(ctx, `
		insert into users (id, org_id, email, password_hash)
		values ($1, $2, $3, $4)
		returning id, org_id, email, password_hash, created_at`,
		newID("u"), orgID, email, passwordHash).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrEmailInUse
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// UserByEmail looks up an account inside an org.
func (d *DB) UserByEmail(ctx context.Context, orgID, email string) (User, error) {
	var u User
	err := d.pool.QueryRow(ctx, `
		select id, org_id, email, password_hash, created_at
		from users where org_id = $1 and email = $2`, orgID, email).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoUser
	}
	return u, err
}

// CreateSession returns the bearer cookie value and its expiry.
func (d *DB) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, time.Time, error) {
	token := auth.NewSecret()
	expires := time.Now().Add(ttl).Truncate(time.Second)
	if _, err := d.pool.Exec(ctx, `
		insert into sessions (id, user_id, expires_at) values ($1, $2, $3)`,
		hex.EncodeToString(auth.HashSecret(token)), userID, expires); err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expires, nil
}

// UserBySession resolves a session token.
func (d *DB) UserBySession(ctx context.Context, token string) (User, error) {
	var u User
	err := d.pool.QueryRow(ctx, `
		select u.id, u.org_id, u.email, u.password_hash, u.created_at
		from sessions s join users u on u.id = s.user_id
		where s.id = $1 and s.expires_at > now()`,
		hex.EncodeToString(auth.HashSecret(token))).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoSession
	}
	return u, err
}

// DeleteSession removes a session token.
func (d *DB) DeleteSession(ctx context.Context, token string) error {
	_, err := d.pool.Exec(ctx, `delete from sessions where id = $1`,
		hex.EncodeToString(auth.HashSecret(token)))
	return err
}

// CreateAPIKey mints a bearer token and stores only its hash.
func (d *DB) CreateAPIKey(ctx context.Context, orgID, userID, name string, scopes []string) (string, APIKey, error) {
	token := auth.NewSecret()
	key := APIKey{
		ID:        newID("k"),
		OrgID:     orgID,
		UserID:    userID,
		Name:      name,
		Scopes:    scopes,
		CreatedAt: time.Now(),
	}
	if _, err := d.pool.Exec(ctx, `
		insert into api_keys (id, org_id, user_id, name, key_hash, scopes)
		values ($1, $2, nullif($3, ''), $4, $5, $6)`,
		key.ID, orgID, userID, name, auth.HashSecret(token), scopes); err != nil {
		return "", APIKey{}, fmt.Errorf("create api key: %w", err)
	}
	return token, key, nil
}

// PrincipalByAPIKey resolves a bearer token to its owner and scopes.
func (d *DB) PrincipalByAPIKey(ctx context.Context, token string) (userID, orgID string, scopes []string, err error) {
	var keyID string
	err = d.pool.QueryRow(ctx, `
		select id, coalesce(user_id, ''), org_id, scopes
		from api_keys
		where key_hash = $1 and revoked_at is null`,
		auth.HashSecret(token)).Scan(&keyID, &userID, &orgID, &scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, ErrNoAPIKey
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("lookup api key: %w", err)
	}
	_, _ = d.pool.Exec(ctx, `update api_keys set last_used_at = now() where id = $1`, keyID)
	return userID, orgID, scopes, nil
}

// SetOrgRole upserts a user's role binding for the org.
func (d *DB) SetOrgRole(ctx context.Context, userID, orgID, role string) error {
	_, err := d.pool.Exec(ctx, `
		insert into role_bindings (id, user_id, org_id, scope_kind, scope_id, role)
		values ($1, $2, $3, 'org', '', $4)
		on conflict (user_id, scope_kind, scope_id)
		do update set role = excluded.role`,
		newID("rb"), userID, orgID, role)
	if err != nil {
		return fmt.Errorf("set role: %w", err)
	}
	return nil
}

// UserWithRole is a user plus their org-level role ("" when none is bound).
type UserWithRole struct {
	User
	Role string
}

// ListUsers returns the org's accounts ordered by email, one keyset page at
// a time.
func (d *DB) ListUsers(ctx context.Context, orgID string, limit int, after string) ([]UserWithRole, string, error) {
	parts, err := decodeCursor(after, 2)
	if err != nil {
		return nil, "", err
	}
	var afterEmail, afterID string
	if parts != nil {
		afterEmail, afterID = parts[0], parts[1]
	}
	rows, err := d.pool.Query(ctx, `
		select u.id, u.org_id, u.email, u.password_hash, u.created_at, coalesce(rb.role, '')
		from users u
		left join role_bindings rb
		  on rb.user_id = u.id and rb.org_id = u.org_id
		 and rb.scope_kind = 'org' and rb.scope_id = ''
		where u.org_id = $1 and ($2 = '' or (u.email, u.id) > ($2, $3))
		order by u.email, u.id limit $4`, orgID, afterEmail, afterID, pageQuerySize(limit))
	if err != nil {
		return nil, "", fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []UserWithRole
	for rows.Next() {
		var u UserWithRole
		if err := rows.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.Role); err != nil {
			return nil, "", err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(users) <= pageLimit(limit) {
		return users, "", nil
	}
	kept := users[:pageLimit(limit)]
	last := kept[len(kept)-1]
	return kept, encodeCursor(last.Email, last.ID), nil
}

// UserByID returns one account in the org with its org-level role.
func (d *DB) UserByID(ctx context.Context, orgID, id string) (UserWithRole, error) {
	var u UserWithRole
	err := d.pool.QueryRow(ctx, `
		select u.id, u.org_id, u.email, u.password_hash, u.created_at, coalesce(rb.role, '')
		from users u
		left join role_bindings rb
		  on rb.user_id = u.id and rb.org_id = u.org_id
		 and rb.scope_kind = 'org' and rb.scope_id = ''
		where u.org_id = $1 and u.id = $2`, orgID, id).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserWithRole{}, ErrNoUser
	}
	return u, err
}

// CountOwners counts the org's owner bindings.
func (d *DB) CountOwners(ctx context.Context, orgID string) (int, error) {
	var n int
	err := d.pool.QueryRow(ctx, `
		select count(*) from role_bindings
		where org_id = $1 and scope_kind = 'org' and role = 'owner'`, orgID).Scan(&n)
	return n, err
}

// ScopesForUser unions the scopes of every role binding. Org bindings grant
// wildcard scopes; server bindings grant that server only; group bindings
// expand to their member servers.
func (d *DB) ScopesForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := d.pool.Query(ctx, `
		select org_id, scope_kind, scope_id, role
		from role_bindings where user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("read roles: %w", err)
	}
	type binding struct{ orgID, kind, scopeID, role string }
	var bindings []binding
	for rows.Next() {
		var b binding
		if err := rows.Scan(&b.orgID, &b.kind, &b.scopeID, &b.role); err != nil {
			rows.Close()
			return nil, err
		}
		bindings = append(bindings, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var scopes []string
	add := func(values []string) {
		for _, s := range values {
			if !seen[s] {
				seen[s] = true
				scopes = append(scopes, s)
			}
		}
	}
	for _, b := range bindings {
		switch b.kind {
		case "org":
			add(auth.ScopesForRole(b.role, b.orgID))
		case "server":
			add(auth.ScopesForServer(b.role, b.scopeID))
		case "group":
			ids, err := d.groupServerIDs(ctx, b.scopeID)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				add(auth.ScopesForServer(b.role, id))
			}
		}
	}
	return scopes, nil
}

func (d *DB) groupServerIDs(ctx context.Context, groupID string) ([]string, error) {
	rows, err := d.pool.Query(ctx, `select server_id from server_group_members where group_id = $1`, groupID)
	if err != nil {
		return nil, fmt.Errorf("read group members: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Audit appends a mutation record.
func (d *DB) Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error {
	payload := []byte(`{}`)
	if diff != nil {
		if data, err := json.Marshal(diff); err == nil {
			payload = data
		}
	}
	_, err := d.pool.Exec(ctx, `
		insert into audit_log (org_id, actor, via, action, target, diff)
		values ($1, $2, $3, $4, $5, $6)`,
		orgID, actor, via, action, target, payload)
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}

// ListAudit returns the newest entries first, before seq when non-zero.
func (d *DB) ListAudit(ctx context.Context, orgID string, limit int, after string) ([]AuditEntry, string, error) {
	afterSeq, err := decodeSeqCursor(after)
	if err != nil {
		return nil, "", err
	}
	rows, err := d.pool.Query(ctx, `
		select seq, org_id, actor, via, action, target, diff, created_at
		from audit_log
		where org_id = $1 and ($2 = 0 or seq < $2)
		order by seq desc limit $3`, orgID, afterSeq, pageQuerySize(limit))
	if err != nil {
		return nil, "", fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.Seq, &e.OrgID, &e.Actor, &e.Via, &e.Action, &e.Target, &e.Diff, &e.CreatedAt); err != nil {
			return nil, "", err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(out) <= pageLimit(limit) {
		return out, "", nil
	}
	kept := out[:pageLimit(limit)]
	return kept, encodeSeqCursor(kept[len(kept)-1].Seq), nil
}

// CreateGroup inserts a targeting group.
func (d *DB) CreateGroup(ctx context.Context, orgID, name, description string) (Group, error) {
	var g Group
	err := d.pool.QueryRow(ctx, `
		insert into server_groups (id, org_id, name, description)
		values ($1, $2, $3, $4)
		returning id, org_id, name, description, created_at`,
		newID("g"), orgID, name, description).
		Scan(&g.ID, &g.OrgID, &g.Name, &g.Description, &g.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Group{}, ErrGroupInUse
		}
		return Group{}, fmt.Errorf("create group: %w", err)
	}
	return g, nil
}

// ListGroups returns the org's groups.
func (d *DB) ListGroups(ctx context.Context, orgID string, limit int, after string) ([]Group, string, error) {
	parts, err := decodeCursor(after, 2)
	if err != nil {
		return nil, "", err
	}
	var afterName, afterID string
	if parts != nil {
		afterName, afterID = parts[0], parts[1]
	}
	rows, err := d.pool.Query(ctx, `
		select id, org_id, name, description, created_at
		from server_groups
		where org_id = $1 and ($2 = '' or (name, id) > ($2, $3))
		order by name, id limit $4`, orgID, afterName, afterID, pageQuerySize(limit))
	if err != nil {
		return nil, "", fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.OrgID, &g.Name, &g.Description, &g.CreatedAt); err != nil {
			return nil, "", err
		}
		out = append(out, g)
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

// AddGroupMember is idempotent and scoped to the org.
func (d *DB) AddGroupMember(ctx context.Context, orgID, groupID, serverID string) error {
	tag, err := d.pool.Exec(ctx, `
		insert into server_group_members (group_id, server_id)
		select g.id, s.id from server_groups g, servers s
		where g.id = $1 and g.org_id = $2 and s.id = $3 and s.org_id = $2
		on conflict do nothing`, groupID, orgID, serverID)
	if err != nil {
		return fmt.Errorf("add group member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either already a member or one of the ids does not belong to the org.
		var exists int
		err := d.pool.QueryRow(ctx, `select 1 from server_groups where id = $1 and org_id = $2`, groupID, orgID).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoGroup
		}
	}
	return nil
}

// RemoveGroupMember detaches a server from a group.
func (d *DB) RemoveGroupMember(ctx context.Context, orgID, groupID, serverID string) error {
	_, err := d.pool.Exec(ctx, `
		delete from server_group_members m
		using server_groups g
		where m.group_id = g.id and g.org_id = $1 and g.id = $2 and m.server_id = $3`,
		orgID, groupID, serverID)
	return err
}

// ListGroupMembers returns server ids in a group.
func (d *DB) ListGroupMembers(ctx context.Context, orgID, groupID string, limit int, after string) ([]string, string, error) {
	parts, err := decodeCursor(after, 1)
	if err != nil {
		return nil, "", err
	}
	var afterID string
	if parts != nil {
		afterID = parts[0]
	}
	rows, err := d.pool.Query(ctx, `
		select m.server_id
		from server_group_members m join server_groups g on g.id = m.group_id
		where g.org_id = $1 and g.id = $2 and ($3 = '' or m.server_id > $3)
		order by m.server_id limit $4`, orgID, groupID, afterID, pageQuerySize(limit))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, "", err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(out) <= pageLimit(limit) {
		return out, "", nil
	}
	kept := out[:pageLimit(limit)]
	return kept, encodeCursor(kept[len(kept)-1]), nil
}

func newID(prefix string) string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic("store: crypto/rand failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(raw)
}
