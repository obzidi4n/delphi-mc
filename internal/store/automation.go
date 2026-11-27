package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoSchedule is a missing schedule.
var ErrNoSchedule = errors.New("no such schedule")

// Schedule is one cron-triggered action.
type Schedule struct {
	ID         string
	OrgID      string
	Name       string
	Cron       string
	Timezone   string
	TargetKind string
	TargetID   string
	Action     string
	Enabled    bool
	NextRun    *time.Time
	LastRun    *time.Time
	CreatedAt  time.Time
}

const scheduleColumns = `id, org_id, name, cron, timezone, target_kind, target_id, action,
	enabled, next_run, last_run, created_at`

func scanSchedule(row interface{ Scan(dest ...any) error }) (Schedule, error) {
	var s Schedule
	err := row.Scan(&s.ID, &s.OrgID, &s.Name, &s.Cron, &s.Timezone, &s.TargetKind, &s.TargetID,
		&s.Action, &s.Enabled, &s.NextRun, &s.LastRun, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, ErrNoSchedule
	}
	return s, err
}

// CreateSchedule inserts a schedule.
func (d *DB) CreateSchedule(ctx context.Context, s Schedule) (Schedule, error) {
	err := d.pool.QueryRow(ctx, `
		insert into schedules (id, org_id, name, cron, timezone, target_kind, target_id, action, next_run)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		returning `+scheduleColumns,
		s.ID, s.OrgID, s.Name, s.Cron, s.Timezone, s.TargetKind, s.TargetID, s.Action, s.NextRun).Scan(
		&s.ID, &s.OrgID, &s.Name, &s.Cron, &s.Timezone, &s.TargetKind, &s.TargetID,
		&s.Action, &s.Enabled, &s.NextRun, &s.LastRun, &s.CreatedAt)
	if err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return Schedule{}, ErrNameInUse
		}
		return Schedule{}, fmt.Errorf("create schedule: %w", err)
	}
	return s, nil
}

// ListSchedules returns an org's schedules.
func (d *DB) ListSchedules(ctx context.Context, orgID string) ([]Schedule, error) {
	rows, err := d.pool.Query(ctx,
		`select `+scheduleColumns+` from schedules where org_id = $1 order by name, id`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteSchedule removes an org's schedule.
func (d *DB) DeleteSchedule(ctx context.Context, orgID, id string) error {
	tag, err := d.pool.Exec(ctx, `delete from schedules where org_id = $1 and id = $2`, orgID, id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoSchedule
	}
	return nil
}

// DueSchedules returns enabled schedules whose next_run has passed. The panel
// is single-replica in v1; MarkScheduleRan advances each row after the run.
func (d *DB) DueSchedules(ctx context.Context, now time.Time) ([]Schedule, error) {
	rows, err := d.pool.Query(ctx, `
		select `+scheduleColumns+` from schedules
		where enabled and (next_run is null or next_run <= $1)
		order by next_run nulls first, id limit 20`, now)
	if err != nil {
		return nil, fmt.Errorf("due schedules: %w", err)
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkScheduleRan advances a schedule after its action was dispatched.
func (d *DB) MarkScheduleRan(ctx context.Context, id string, next, last time.Time) error {
	_, err := d.pool.Exec(ctx, `update schedules set next_run = $2, last_run = $3 where id = $1`, id, next, last)
	if err != nil {
		return fmt.Errorf("mark schedule: %w", err)
	}
	return nil
}

// AlertRule is a crash-loop or downtime rule.
type AlertRule struct {
	ID            string
	OrgID         string
	Kind          string
	Threshold     int
	WindowSeconds int
	Enabled       bool
	CreatedAt     time.Time
}

// CreateAlertRule inserts a rule.
func (d *DB) CreateAlertRule(ctx context.Context, r AlertRule) (AlertRule, error) {
	err := d.pool.QueryRow(ctx, `
		insert into alert_rules (id, org_id, kind, threshold, window_seconds, enabled)
		values ($1, $2, $3, $4, $5, true)
		returning id, org_id, kind, threshold, window_seconds, enabled, created_at`,
		r.ID, r.OrgID, r.Kind, r.Threshold, r.WindowSeconds).Scan(
		&r.ID, &r.OrgID, &r.Kind, &r.Threshold, &r.WindowSeconds, &r.Enabled, &r.CreatedAt)
	if err != nil {
		return AlertRule{}, fmt.Errorf("create alert rule: %w", err)
	}
	return r, nil
}

// ListAlertRules returns an org's rules.
func (d *DB) ListAlertRules(ctx context.Context, orgID string) ([]AlertRule, error) {
	rows, err := d.pool.Query(ctx,
		`select id, org_id, kind, threshold, window_seconds, enabled, created_at
		 from alert_rules where org_id = $1 order by created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Kind, &r.Threshold, &r.WindowSeconds, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EnabledRulesForOrg returns rules that can fire.
func (d *DB) EnabledRulesForOrg(ctx context.Context, orgID string) ([]AlertRule, error) {
	rows, err := d.pool.Query(ctx,
		`select id, org_id, kind, threshold, window_seconds, enabled, created_at
		 from alert_rules where org_id = $1 and enabled`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Kind, &r.Threshold, &r.WindowSeconds, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteAlertRule removes a rule.
func (d *DB) DeleteAlertRule(ctx context.Context, orgID, id string) error {
	_, err := d.pool.Exec(ctx, `delete from alert_rules where org_id = $1 and id = $2`, orgID, id)
	return err
}

// AlertChannel is one delivery target; ConfigEnc is sealed at rest.
type AlertChannel struct {
	ID        string
	OrgID     string
	Name      string
	Kind      string
	ConfigEnc []byte
	CreatedAt time.Time
}

// CreateAlertChannel inserts a channel.
func (d *DB) CreateAlertChannel(ctx context.Context, c AlertChannel) (AlertChannel, error) {
	err := d.pool.QueryRow(ctx, `
		insert into alert_channels (id, org_id, name, kind, config_enc)
		values ($1, $2, $3, $4, $5)
		returning created_at`,
		c.ID, c.OrgID, c.Name, c.Kind, c.ConfigEnc).Scan(&c.CreatedAt)
	if err != nil {
		return AlertChannel{}, fmt.Errorf("create alert channel: %w", err)
	}
	return c, nil
}

// ListAlertChannels returns channels without secrets.
func (d *DB) ListAlertChannels(ctx context.Context, orgID string) ([]AlertChannel, error) {
	rows, err := d.pool.Query(ctx,
		`select id, org_id, name, kind, '{}'::bytea, created_at from alert_channels where org_id = $1 order by name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertChannel
	for rows.Next() {
		var c AlertChannel
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Kind, &c.ConfigEnc, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AlertChannels returns channels with sealed config for delivery.
func (d *DB) AlertChannels(ctx context.Context, orgID string) ([]AlertChannel, error) {
	rows, err := d.pool.Query(ctx,
		`select id, org_id, name, kind, config_enc, created_at from alert_channels where org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertChannel
	for rows.Next() {
		var c AlertChannel
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Kind, &c.ConfigEnc, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteAlertChannel removes a channel.
func (d *DB) DeleteAlertChannel(ctx context.Context, orgID, id string) error {
	_, err := d.pool.Exec(ctx, `delete from alert_channels where org_id = $1 and id = $2`, orgID, id)
	return err
}

// Alert is one firing or resolved incident.
type Alert struct {
	ID         string
	OrgID      string
	RuleID     string
	ServerID   string
	State      string
	Summary    string
	StartedAt  time.Time
	ResolvedAt *time.Time
}

// CreateAlert inserts a firing alert.
func (d *DB) CreateAlert(ctx context.Context, a Alert) error {
	_, err := d.pool.Exec(ctx, `
		insert into alerts (id, org_id, rule_id, server_id, state, summary)
		values ($1, $2, $3, $4, 'firing', $5)`,
		a.ID, a.OrgID, a.RuleID, a.ServerID, a.Summary)
	if err != nil {
		return fmt.Errorf("create alert: %w", err)
	}
	return nil
}

// ResolveAlerts marks firing alerts for a server+rule resolved.
func (d *DB) ResolveAlerts(ctx context.Context, serverID, ruleID string) (int, error) {
	tag, err := d.pool.Exec(ctx, `
		update alerts set state = 'resolved', resolved_at = now()
		where server_id = $1 and rule_id = $2 and state = 'firing'`, serverID, ruleID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// FiringAlerts returns firing alerts for a server.
func (d *DB) FiringAlerts(ctx context.Context, serverID string) ([]Alert, error) {
	rows, err := d.pool.Query(ctx, `
		select id, org_id, rule_id, server_id, state, summary, started_at, resolved_at
		from alerts where server_id = $1 and state = 'firing'`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.OrgID, &a.RuleID, &a.ServerID, &a.State, &a.Summary, &a.StartedAt, &a.ResolvedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAlerts returns recent alerts for an org.
func (d *DB) ListAlerts(ctx context.Context, orgID string, limit int) ([]Alert, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := d.pool.Query(ctx, `
		select id, org_id, rule_id, server_id, state, summary, started_at, resolved_at
		from alerts where org_id = $1 order by started_at desc limit $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.OrgID, &a.RuleID, &a.ServerID, &a.State, &a.Summary, &a.StartedAt, &a.ResolvedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MaintenanceWindow suppresses alerts and can block updates.
type MaintenanceWindow struct {
	ID             string
	OrgID          string
	StartsAt       time.Time
	EndsAt         time.Time
	SuppressAlerts bool
	BlockUpdates   bool
	CreatedAt      time.Time
}

// CreateMaintenanceWindow inserts a window.
func (d *DB) CreateMaintenanceWindow(ctx context.Context, w MaintenanceWindow) (MaintenanceWindow, error) {
	err := d.pool.QueryRow(ctx, `
		insert into maintenance_windows (id, org_id, starts_at, ends_at, suppress_alerts, block_updates)
		values ($1, $2, $3, $4, $5, $6) returning created_at`,
		w.ID, w.OrgID, w.StartsAt, w.EndsAt, w.SuppressAlerts, w.BlockUpdates).Scan(&w.CreatedAt)
	if err != nil {
		return MaintenanceWindow{}, err
	}
	return w, nil
}

// ListMaintenanceWindows returns an org's windows.
func (d *DB) ListMaintenanceWindows(ctx context.Context, orgID string) ([]MaintenanceWindow, error) {
	rows, err := d.pool.Query(ctx, `
		select id, org_id, starts_at, ends_at, suppress_alerts, block_updates, created_at
		from maintenance_windows where org_id = $1 order by starts_at desc`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MaintenanceWindow
	for rows.Next() {
		var w MaintenanceWindow
		if err := rows.Scan(&w.ID, &w.OrgID, &w.StartsAt, &w.EndsAt, &w.SuppressAlerts, &w.BlockUpdates, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteMaintenanceWindow removes a window.
func (d *DB) DeleteMaintenanceWindow(ctx context.Context, orgID, id string) error {
	_, err := d.pool.Exec(ctx, `delete from maintenance_windows where org_id = $1 and id = $2`, orgID, id)
	return err
}

// ActiveMaintenance reports whether an alert-suppressing window covers now.
func (d *DB) ActiveMaintenance(ctx context.Context, orgID string, now time.Time) (bool, error) {
	var n int
	err := d.pool.QueryRow(ctx, `
		select count(*) from maintenance_windows
		where org_id = $1 and suppress_alerts and starts_at <= $2 and ends_at >= $2`,
		orgID, now).Scan(&n)
	return n > 0, err
}
