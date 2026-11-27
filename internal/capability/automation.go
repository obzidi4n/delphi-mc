package capability

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"delphi.dev/delphi/internal/secret"
	"delphi.dev/delphi/internal/store"
)

// ScheduleStore is schedule persistence.
type ScheduleStore interface {
	CreateSchedule(ctx context.Context, s store.Schedule) (store.Schedule, error)
	ListSchedules(ctx context.Context, orgID string) ([]store.Schedule, error)
	DeleteSchedule(ctx context.Context, orgID, id string) error
	DueSchedules(ctx context.Context, now time.Time) ([]store.Schedule, error)
	MarkScheduleRan(ctx context.Context, id string, next, last time.Time) error
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// Schedules creates and runs cron actions.
type Schedules struct {
	Store   ScheduleStore
	Servers *Servers
	Backups *Backups
	Updates *Updates
	Log     *slog.Logger
}

// ScheduleRequest is a create-schedule request.
type ScheduleRequest struct {
	Name     string `json:"name"`
	ServerID string `json:"server_id"`
	Action   string `json:"action"` // power.restart | power.start | power.stop | backup.create | updates.apply
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
}

var scheduleParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Create validates and stores a schedule.
func (s *Schedules) Create(ctx context.Context, p Principal, req ScheduleRequest) (store.Schedule, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return store.Schedule{}, forbidden(orgScope(p, "admin"))
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 {
		return store.Schedule{}, invalid("name must be 1-64 characters")
	}
	req.Timezone = strings.TrimSpace(req.Timezone)
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	loc, err := time.LoadLocation(req.Timezone)
	if err != nil {
		return store.Schedule{}, invalid("timezone is not valid")
	}
	spec, err := scheduleParser.Parse(strings.TrimSpace(req.Cron))
	if err != nil {
		return store.Schedule{}, invalid("cron: %v", err)
	}
	switch req.Action {
	case "power.restart", "power.start", "power.stop", "backup.create", "updates.apply":
	default:
		return store.Schedule{}, invalid("action must be power.restart, power.start, power.stop, backup.create, or updates.apply")
	}
	if _, err := s.Servers.Get(ctx, p, req.ServerID); err != nil {
		return store.Schedule{}, err
	}
	id, err := newID("sch_")
	if err != nil {
		return store.Schedule{}, err
	}
	next := spec.Next(time.Now().In(loc))
	row, err := s.Store.CreateSchedule(ctx, store.Schedule{
		ID: id, OrgID: p.OrgID, Name: req.Name, Cron: req.Cron, Timezone: req.Timezone,
		TargetKind: "server", TargetID: req.ServerID, Action: req.Action, NextRun: &next,
	})
	if err != nil {
		return store.Schedule{}, err
	}
	_ = s.Store.Audit(ctx, p.OrgID, p.UserID, p.Via, "schedule.create", id, map[string]any{
		"server_id": req.ServerID, "action": req.Action, "cron": req.Cron, "timezone": req.Timezone,
	})
	return row, nil
}

// List returns an org's schedules.
func (s *Schedules) List(ctx context.Context, p Principal) ([]store.Schedule, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, forbidden(orgScope(p, "read"))
	}
	return s.Store.ListSchedules(ctx, p.OrgID)
}

// Delete removes an org's schedule.
func (s *Schedules) Delete(ctx context.Context, p Principal, id string) error {
	if !p.Allows(orgScope(p, "admin")) {
		return forbidden(orgScope(p, "admin"))
	}
	if err := s.Store.DeleteSchedule(ctx, p.OrgID, id); err != nil {
		return err
	}
	_ = s.Store.Audit(ctx, p.OrgID, p.UserID, p.Via, "schedule.delete", id, nil)
	return nil
}

// RunDue executes due schedules and advances their next_run. It is called by a
// panel loop; a single panel owns schedules in v1.
func (s *Schedules) RunDue(ctx context.Context, now time.Time) int {
	due, err := s.Store.DueSchedules(ctx, now)
	if err != nil {
		s.log().Warn("schedule scan", "err", err)
		return 0
	}
	ran := 0
	for _, row := range due {
		loc := time.UTC
		if l, err := time.LoadLocation(row.Timezone); err == nil {
			loc = l
		}
		spec, err := scheduleParser.Parse(row.Cron)
		if err != nil {
			s.log().Warn("bad schedule cron", "schedule", row.ID, "err", err)
			continue
		}
		next := spec.Next(now.In(loc))
		principal := Principal{
			UserID: "schedule:" + row.Name, OrgID: row.OrgID, Via: "schedule",
			Scopes: []string{
				orgScope(Principal{OrgID: row.OrgID}, "read"),
				"server:*:read", "server:*:update", "server:*:power",
			},
		}
		err = s.run(ctx, principal, row)
		if markErr := s.Store.MarkScheduleRan(ctx, row.ID, next, now); markErr != nil {
			s.log().Warn("schedule advance", "schedule", row.ID, "err", markErr)
		}
		_ = s.Store.Audit(ctx, row.OrgID, "schedule:"+row.Name, "schedule", "schedule.run", row.ID,
			map[string]any{"action": row.Action, "server_id": row.TargetID, "error": errString(err)})
		ran++
	}
	return ran
}

func (s *Schedules) run(ctx context.Context, p Principal, row store.Schedule) error {
	switch row.Action {
	case "power.restart", "power.start", "power.stop":
		if s.Servers == nil {
			return errors.New("servers are not configured")
		}
		action := strings.TrimPrefix(row.Action, "power.")
		_, err := s.Servers.Power(ctx, p, row.TargetID, action)
		return err
	case "backup.create":
		if s.Backups == nil {
			return errors.New("backups are not configured")
		}
		_, err := s.Backups.Create(ctx, p, row.TargetID)
		return err
	case "updates.apply":
		if s.Updates == nil {
			return errors.New("updates are not configured")
		}
		_, err := s.Updates.Apply(ctx, p, row.TargetID)
		if errors.Is(err, ErrNoUpdates) {
			return nil
		}
		return err
	}
	return fmt.Errorf("unknown action %q", row.Action)
}

func (s *Schedules) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// AlertStore is alert persistence.
type AlertStore interface {
	GetServer(ctx context.Context, id string) (store.Server, error)
	EnabledRulesForOrg(ctx context.Context, orgID string) ([]store.AlertRule, error)
	AlertChannels(ctx context.Context, orgID string) ([]store.AlertChannel, error)
	ActiveMaintenance(ctx context.Context, orgID string, now time.Time) (bool, error)
	CreateAlert(ctx context.Context, a store.Alert) error
	ResolveAlerts(ctx context.Context, serverID, ruleID string) (int, error)
	FiringAlerts(ctx context.Context, serverID string) ([]store.Alert, error)
	ListAlerts(ctx context.Context, orgID string, limit int) ([]store.Alert, error)
	CreateAlertRule(ctx context.Context, r store.AlertRule) (store.AlertRule, error)
	ListAlertRules(ctx context.Context, orgID string) ([]store.AlertRule, error)
	DeleteAlertRule(ctx context.Context, orgID, id string) error
	CreateAlertChannel(ctx context.Context, c store.AlertChannel) (store.AlertChannel, error)
	ListAlertChannels(ctx context.Context, orgID string) ([]store.AlertChannel, error)
	DeleteAlertChannel(ctx context.Context, orgID, id string) error
	CreateMaintenanceWindow(ctx context.Context, w store.MaintenanceWindow) (store.MaintenanceWindow, error)
	ListMaintenanceWindows(ctx context.Context, orgID string) ([]store.MaintenanceWindow, error)
	DeleteMaintenanceWindow(ctx context.Context, orgID, id string) error
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// Alerts evaluates crash-loop rules and delivers webhook/SMTP messages.
type Alerts struct {
	Store  AlertStore
	Box    *secret.Box
	HTTP   *http.Client
	Log    *slog.Logger
	mu     sync.Mutex
	events map[string][]time.Time
}

// NotifyState evaluates a state report without blocking the agent channel.
func (a *Alerts) NotifyState(serverID, state string) {
	if a == nil || a.Store == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.evaluate(ctx, serverID, state); err != nil {
			a.log().Warn("alert evaluation", "server", serverID, "err", err)
		}
	}()
}

func (a *Alerts) evaluate(ctx context.Context, serverID, state string) error {
	srv, err := a.Store.GetServer(ctx, serverID)
	if err != nil {
		return err
	}
	rules, err := a.Store.EnabledRulesForOrg(ctx, srv.OrgID)
	if err != nil {
		return err
	}
	now := time.Now()
	if state == "running" {
		for _, rule := range rules {
			if _, err := a.Store.ResolveAlerts(ctx, serverID, rule.ID); err != nil {
				continue
			}
		}
		return nil
	}
	if state != "exited" && state != "error" {
		return nil
	}

	a.mu.Lock()
	if a.events == nil {
		a.events = map[string][]time.Time{}
	}
	a.events[serverID] = append(a.events[serverID], now)
	events := append([]time.Time(nil), a.events[serverID]...)
	a.mu.Unlock()

	firing, err := a.Store.FiringAlerts(ctx, serverID)
	if err != nil {
		return err
	}
	firingSet := map[string]bool{}
	for _, alert := range firing {
		firingSet[alert.RuleID] = true
	}

	for _, rule := range rules {
		if firingSet[rule.ID] {
			continue
		}
		window := time.Duration(rule.WindowSeconds) * time.Second
		count := 0
		for _, at := range events {
			if now.Sub(at) <= window {
				count++
			}
		}
		if count < rule.Threshold {
			continue
		}
		summary := fmt.Sprintf("%s on %s: %d crash exits in %s", rule.Kind, srv.Name, count, window)
		id, err := newID("alert_")
		if err != nil {
			return err
		}
		if err := a.Store.CreateAlert(ctx, store.Alert{
			ID: id, OrgID: srv.OrgID, RuleID: rule.ID, ServerID: serverID,
			State: "firing", Summary: summary,
		}); err != nil {
			return err
		}
		_ = a.Store.Audit(ctx, srv.OrgID, "alerts", "service", "alert.fire", id,
			map[string]any{"server_id": serverID, "rule": rule.Kind, "summary": summary})
		suppressed, err := a.Store.ActiveMaintenance(ctx, srv.OrgID, now)
		if err == nil && suppressed {
			continue
		}
		a.deliver(ctx, srv.OrgID, summary)
	}
	return nil
}

type channelConfig struct {
	URL      string `json:"url"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"`
}

func (a *Alerts) deliver(ctx context.Context, orgID, message string) {
	channels, err := a.Store.AlertChannels(ctx, orgID)
	if err != nil {
		a.log().Warn("alert channels", "err", err)
		return
	}
	for _, channel := range channels {
		plain, err := a.Box.Open(channel.ConfigEnc)
		if err != nil {
			a.log().Warn("alert channel decrypt", "channel", channel.ID, "err", err)
			continue
		}
		var cfg channelConfig
		if err := json.Unmarshal(plain, &cfg); err != nil {
			continue
		}
		switch channel.Kind {
		case "webhook":
			a.webhook(ctx, cfg.URL, message)
		case "smtp":
			a.mail(cfg, message)
		}
	}
}

func (a *Alerts) webhook(ctx context.Context, url, message string) {
	if url == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"content": message})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := a.HTTP
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		a.log().Warn("alert webhook", "err", err)
		return
	}
	resp.Body.Close()
}

func (a *Alerts) mail(cfg channelConfig, message string) {
	if cfg.Host == "" || cfg.From == "" || cfg.To == "" {
		return
	}
	port := cfg.Port
	if port == 0 {
		port = 587
	}
	addr := cfg.Host + ":" + strconv.Itoa(port)
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	body := "To: " + cfg.To + "\r\nFrom: " + cfg.From + "\r\nSubject: DelphiMC alert\r\n\r\n" + message + "\r\n"
	if err := smtp.SendMail(addr, auth, cfg.From, []string{cfg.To}, []byte(body)); err != nil {
		a.log().Warn("alert smtp", "err", err)
	}
}

// AlertCreateRule creates a rule; callers hold org admin.
func (a *Alerts) CreateRule(ctx context.Context, p Principal, kind string, threshold, window int) (store.AlertRule, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return store.AlertRule{}, forbidden(orgScope(p, "admin"))
	}
	if kind != "crash_loop" && kind != "server_down" {
		return store.AlertRule{}, invalid("kind must be crash_loop or server_down")
	}
	if threshold <= 0 {
		threshold = 1
	}
	if window <= 0 {
		window = 600
	}
	id, err := newID("rule_")
	if err != nil {
		return store.AlertRule{}, err
	}
	rule, err := a.Store.CreateAlertRule(ctx, store.AlertRule{
		ID: id, OrgID: p.OrgID, Kind: kind, Threshold: threshold, WindowSeconds: window, Enabled: true,
	})
	if err != nil {
		return store.AlertRule{}, err
	}
	_ = a.Store.Audit(ctx, p.OrgID, p.UserID, p.Via, "alert_rule.create", id, map[string]any{"kind": kind})
	return rule, nil
}

// Rules lists rules.
func (a *Alerts) Rules(ctx context.Context, p Principal) ([]store.AlertRule, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, forbidden(orgScope(p, "read"))
	}
	return a.Store.ListAlertRules(ctx, p.OrgID)
}

// DeleteRule removes a rule.
func (a *Alerts) DeleteRule(ctx context.Context, p Principal, id string) error {
	if !p.Allows(orgScope(p, "admin")) {
		return forbidden(orgScope(p, "admin"))
	}
	return a.Store.DeleteAlertRule(ctx, p.OrgID, id)
}

// CreateChannel seals and stores a delivery channel.
func (a *Alerts) CreateChannel(ctx context.Context, p Principal, name, kind string, config json.RawMessage) (store.AlertChannel, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return store.AlertChannel{}, forbidden(orgScope(p, "admin"))
	}
	if kind != "webhook" && kind != "smtp" {
		return store.AlertChannel{}, invalid("kind must be webhook or smtp")
	}
	if len(config) == 0 || len(config) > 8192 {
		return store.AlertChannel{}, invalid("config must be 1-8192 bytes")
	}
	var cfg channelConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return store.AlertChannel{}, invalid("config: %v", err)
	}
	if kind == "webhook" && !strings.HasPrefix(cfg.URL, "https://") && !strings.HasPrefix(cfg.URL, "http://") {
		return store.AlertChannel{}, invalid("webhook url must be http(s)")
	}
	if a.Box == nil {
		return store.AlertChannel{}, errors.New("secret store is not configured")
	}
	sealed, err := a.Box.Seal(config)
	if err != nil {
		return store.AlertChannel{}, err
	}
	id, err := newID("chan_")
	if err != nil {
		return store.AlertChannel{}, err
	}
	channel, err := a.Store.CreateAlertChannel(ctx, store.AlertChannel{
		ID: id, OrgID: p.OrgID, Name: strings.TrimSpace(name), Kind: kind, ConfigEnc: sealed,
	})
	if err != nil {
		return store.AlertChannel{}, err
	}
	_ = a.Store.Audit(ctx, p.OrgID, p.UserID, p.Via, "alert_channel.create", id, map[string]any{"kind": kind})
	return channel, nil
}

// Channels lists channels without secrets.
func (a *Alerts) Channels(ctx context.Context, p Principal) ([]store.AlertChannel, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, forbidden(orgScope(p, "read"))
	}
	return a.Store.ListAlertChannels(ctx, p.OrgID)
}

// DeleteChannel removes a channel.
func (a *Alerts) DeleteChannel(ctx context.Context, p Principal, id string) error {
	if !p.Allows(orgScope(p, "admin")) {
		return forbidden(orgScope(p, "admin"))
	}
	return a.Store.DeleteAlertChannel(ctx, p.OrgID, id)
}

// CreateWindow stores a maintenance window.
func (a *Alerts) CreateWindow(ctx context.Context, p Principal, starts, ends time.Time, suppress, block bool) (store.MaintenanceWindow, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return store.MaintenanceWindow{}, forbidden(orgScope(p, "admin"))
	}
	if !ends.After(starts) {
		return store.MaintenanceWindow{}, invalid("ends_at must be after starts_at")
	}
	id, err := newID("mw_")
	if err != nil {
		return store.MaintenanceWindow{}, err
	}
	window, err := a.Store.CreateMaintenanceWindow(ctx, store.MaintenanceWindow{
		ID: id, OrgID: p.OrgID, StartsAt: starts, EndsAt: ends,
		SuppressAlerts: suppress, BlockUpdates: block,
	})
	if err != nil {
		return store.MaintenanceWindow{}, err
	}
	_ = a.Store.Audit(ctx, p.OrgID, p.UserID, p.Via, "maintenance.create", id, nil)
	return window, nil
}

// Windows lists maintenance windows.
func (a *Alerts) Windows(ctx context.Context, p Principal) ([]store.MaintenanceWindow, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, forbidden(orgScope(p, "read"))
	}
	return a.Store.ListMaintenanceWindows(ctx, p.OrgID)
}

// DeleteWindow removes a window.
func (a *Alerts) DeleteWindow(ctx context.Context, p Principal, id string) error {
	if !p.Allows(orgScope(p, "admin")) {
		return forbidden(orgScope(p, "admin"))
	}
	return a.Store.DeleteMaintenanceWindow(ctx, p.OrgID, id)
}

// List returns recent alerts.
func (a *Alerts) List(ctx context.Context, p Principal, limit int) ([]store.Alert, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, forbidden(orgScope(p, "read"))
	}
	return a.Store.ListAlerts(ctx, p.OrgID, limit)
}

func (a *Alerts) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

func newID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf), nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
