package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AI errors.
var ErrNoConversation = errors.New("conversation not found")

// Conversation is one AI thread owned by a user.
type Conversation struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StoredMessage is one persisted conversation message.
type StoredMessage struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	ToolCalls      json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID     string          `json:"tool_call_id,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// DocChunk is one indexed documentation section.
type DocChunk struct {
	Path    string  `json:"path"`
	Ordinal int     `json:"ordinal"`
	Heading string  `json:"heading"`
	Content string  `json:"content"`
	Rank    float64 `json:"rank,omitempty"`
}

const conversationColumns = `id, org_id, user_id, title, created_at, updated_at`

func scanConversation(row pgx.Row) (Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.OrgID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNoConversation
	}
	return c, err
}

// CreateConversation starts a thread.
func (d *DB) CreateConversation(ctx context.Context, orgID, userID, title string) (Conversation, error) {
	return scanConversation(d.pool.QueryRow(ctx, `
		insert into conversations (id, org_id, user_id, title)
		values ($1, $2, $3, $4)
		returning `+conversationColumns, newID("conv"), orgID, userID, title))
}

// ListConversations returns the user's threads, newest first.
func (d *DB) ListConversations(ctx context.Context, orgID, userID string, limit int) ([]Conversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := d.pool.Query(ctx, `
		select `+conversationColumns+` from conversations
		where org_id = $1 and user_id = $2
		order by updated_at desc limit $3`, orgID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Conversation loads one thread owned by the user.
func (d *DB) Conversation(ctx context.Context, orgID, userID, id string) (Conversation, error) {
	return scanConversation(d.pool.QueryRow(ctx, `
		select `+conversationColumns+` from conversations
		where id = $1 and org_id = $2 and user_id = $3`, id, orgID, userID))
}

// Messages returns a thread's messages oldest first.
func (d *DB) Messages(ctx context.Context, conversationID string) ([]StoredMessage, error) {
	rows, err := d.pool.Query(ctx, `
		select id, conversation_id, role, content, coalesce(tool_calls, 'null'::jsonb), coalesce(tool_call_id, ''), created_at
		from messages where conversation_id = $1 order by created_at, id`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	var out []StoredMessage
	for rows.Next() {
		var m StoredMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.ToolCalls, &m.ToolCallID, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AppendMessage stores one message and touches the thread.
func (d *DB) AppendMessage(ctx context.Context, conversationID, role, content string, toolCalls json.RawMessage, toolCallID string) (StoredMessage, error) {
	if len(toolCalls) == 0 {
		toolCalls = nil
	}
	var m StoredMessage
	err := d.pool.QueryRow(ctx, `
		insert into messages (id, conversation_id, role, content, tool_calls, tool_call_id)
		values ($1, $2, $3, $4, $5, nullif($6, ''))
		returning id, conversation_id, role, content, coalesce(tool_calls, 'null'::jsonb), coalesce(tool_call_id, ''), created_at`,
		newID("msg"), conversationID, role, content, toolCalls, toolCallID).
		Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.ToolCalls, &m.ToolCallID, &m.CreatedAt)
	if err != nil {
		return StoredMessage{}, fmt.Errorf("append message: %w", err)
	}
	_, _ = d.pool.Exec(ctx, `update conversations set updated_at = now() where id = $1`, conversationID)
	return m, nil
}

// RenameConversation sets the thread title.
func (d *DB) RenameConversation(ctx context.Context, conversationID, title string) error {
	_, err := d.pool.Exec(ctx, `update conversations set title = $2, updated_at = now() where id = $1`,
		conversationID, title)
	return err
}

// DocsHash returns the indexed corpus hash ("" when nothing is indexed).
func (d *DB) DocsHash(ctx context.Context) (string, error) {
	var hash string
	err := d.pool.QueryRow(ctx, `select hash from docs_meta where id = 1`).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return hash, err
}

// ReplaceDocs swaps the corpus when hash differs from what is indexed.
func (d *DB) ReplaceDocs(ctx context.Context, hash string, chunks []DocChunk) (bool, error) {
	current, err := d.DocsHash(ctx)
	if err != nil {
		return false, err
	}
	if current == hash && len(chunks) > 0 {
		return false, nil
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `delete from docs_chunks`); err != nil {
		return false, err
	}
	for _, c := range chunks {
		if _, err := tx.Exec(ctx, `
			insert into docs_chunks (path, ordinal, heading, content) values ($1, $2, $3, $4)`,
			c.Path, c.Ordinal, c.Heading, c.Content); err != nil {
			return false, fmt.Errorf("insert chunk %s#%d: %w", c.Path, c.Ordinal, err)
		}
	}
	if _, err := tx.Exec(ctx, `
		insert into docs_meta (id, hash) values (1, $1)
		on conflict (id) do update set hash = excluded.hash`, hash); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// SearchDocs ranks documentation chunks with websearch FTS.
func (d *DB) SearchDocs(ctx context.Context, query string, limit int) ([]DocChunk, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	rows, err := d.pool.Query(ctx, `
		select path, ordinal, heading, content, ts_rank(tsv, q) as rank
		from docs_chunks, websearch_to_tsquery('english', $1) q
		where tsv @@ q
		order by rank desc, path, ordinal
		limit $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search docs: %w", err)
	}
	defer rows.Close()
	var out []DocChunk
	for rows.Next() {
		var c DocChunk
		if err := rows.Scan(&c.Path, &c.Ordinal, &c.Heading, &c.Content, &c.Rank); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Approval errors.
var ErrNoApproval = errors.New("approval not found")

// Approval is one pending or decided two-phase plan.
type Approval struct {
	ID             string
	OrgID          string
	UserID         string
	ConversationID string
	Status         string
	Plan           json.RawMessage
	Results        json.RawMessage
	CreatedAt      time.Time
	DecidedAt      *time.Time
	ExecutedAt     *time.Time
}

const approvalColumns = `id, org_id, user_id, conversation_id, status, plan, coalesce(results, 'null'::jsonb), created_at, decided_at, executed_at`

func scanApproval(row pgx.Row) (Approval, error) {
	var a Approval
	err := row.Scan(&a.ID, &a.OrgID, &a.UserID, &a.ConversationID, &a.Status, &a.Plan, &a.Results, &a.CreatedAt, &a.DecidedAt, &a.ExecutedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Approval{}, ErrNoApproval
	}
	return a, err
}

// CreateApproval stores a pending plan.
func (d *DB) CreateApproval(ctx context.Context, orgID, userID, conversationID string, plan []byte) (Approval, error) {
	return scanApproval(d.pool.QueryRow(ctx, `
		insert into ai_approvals (id, org_id, user_id, conversation_id, status, plan)
		values ($1, $2, $3, $4, 'pending', $5)
		returning `+approvalColumns, newID("ap"), orgID, userID, conversationID, plan))
}

// Approval loads one owned approval.
func (d *DB) Approval(ctx context.Context, orgID, userID, id string) (Approval, error) {
	return scanApproval(d.pool.QueryRow(ctx, `
		select `+approvalColumns+` from ai_approvals
		where id = $1 and org_id = $2 and user_id = $3`, id, orgID, userID))
}

// PendingApprovals lists an owned conversation's pending plans, oldest first.
func (d *DB) PendingApprovals(ctx context.Context, orgID, userID, conversationID string) ([]Approval, error) {
	rows, err := d.pool.Query(ctx, `
		select `+approvalColumns+` from ai_approvals
		where conversation_id = $1 and org_id = $2 and user_id = $3 and status = 'pending'
		order by created_at`, conversationID, orgID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetApprovalStatus moves an approval and stamps the matching timestamp.
func (d *DB) SetApprovalStatus(ctx context.Context, id, status string) error {
	_, err := d.pool.Exec(ctx, `
		update ai_approvals
		set status = $2,
		    decided_at = case when $2 in ('approved','rejected') then now() else decided_at end,
		    executed_at = case when $2 in ('executed','failed') then now() else executed_at end
		where id = $1`, id, status)
	return err
}

// SetApprovalResults records execution output.
func (d *DB) SetApprovalResults(ctx context.Context, id string, results []byte) error {
	_, err := d.pool.Exec(ctx, `update ai_approvals set results = $2 where id = $1`, id, results)
	return err
}
