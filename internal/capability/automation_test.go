package capability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"delphi.dev/delphi/internal/secret"
	"delphi.dev/delphi/internal/store"
)

type fakeAlertStore struct {
	server   store.Server
	rules    []store.AlertRule
	channels []store.AlertChannel
	firing   []store.Alert
	created  []store.Alert
	resolved []string
}

func (f *fakeAlertStore) GetServer(context.Context, string) (store.Server, error) {
	return f.server, nil
}
func (f *fakeAlertStore) EnabledRulesForOrg(context.Context, string) ([]store.AlertRule, error) {
	return f.rules, nil
}
func (f *fakeAlertStore) AlertChannels(context.Context, string) ([]store.AlertChannel, error) {
	return f.channels, nil
}
func (f *fakeAlertStore) ActiveMaintenance(context.Context, string, time.Time) (bool, error) {
	return false, nil
}
func (f *fakeAlertStore) CreateAlert(_ context.Context, a store.Alert) error {
	a.State = "firing"
	f.created = append(f.created, a)
	f.firing = append(f.firing, a)
	return nil
}
func (f *fakeAlertStore) ResolveAlerts(_ context.Context, serverID, ruleID string) (int, error) {
	n := 0
	var keep []store.Alert
	for _, a := range f.firing {
		if a.ServerID == serverID && a.RuleID == ruleID {
			n++
			f.resolved = append(f.resolved, a.ID)
			continue
		}
		keep = append(keep, a)
	}
	f.firing = keep
	return n, nil
}
func (f *fakeAlertStore) FiringAlerts(context.Context, string) ([]store.Alert, error) {
	return f.firing, nil
}
func (f *fakeAlertStore) ListAlerts(context.Context, string, int) ([]store.Alert, error) {
	return f.created, nil
}
func (f *fakeAlertStore) CreateAlertRule(context.Context, store.AlertRule) (store.AlertRule, error) {
	return store.AlertRule{}, nil
}
func (f *fakeAlertStore) ListAlertRules(context.Context, string) ([]store.AlertRule, error) {
	return nil, nil
}
func (f *fakeAlertStore) DeleteAlertRule(context.Context, string, string) error { return nil }
func (f *fakeAlertStore) CreateAlertChannel(context.Context, store.AlertChannel) (store.AlertChannel, error) {
	return store.AlertChannel{}, nil
}
func (f *fakeAlertStore) ListAlertChannels(context.Context, string) ([]store.AlertChannel, error) {
	return nil, nil
}
func (f *fakeAlertStore) DeleteAlertChannel(context.Context, string, string) error { return nil }
func (f *fakeAlertStore) CreateMaintenanceWindow(context.Context, store.MaintenanceWindow) (store.MaintenanceWindow, error) {
	return store.MaintenanceWindow{}, nil
}
func (f *fakeAlertStore) ListMaintenanceWindows(context.Context, string) ([]store.MaintenanceWindow, error) {
	return nil, nil
}
func (f *fakeAlertStore) DeleteMaintenanceWindow(context.Context, string, string) error { return nil }
func (f *fakeAlertStore) Audit(context.Context, string, string, string, string, string, any) error {
	return nil
}

func TestCrashLoopFiresWebhookAndResolves(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hook.Close()

	keyPath := filepath.Join(t.TempDir(), "secrets.key")
	box, err := secret.New(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal([]byte(`{"url":"` + hook.URL + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAlertStore{
		server:   store.Server{ID: "s_1", OrgID: "default", Name: "SMP", DesiredState: "running"},
		rules:    []store.AlertRule{{ID: "rule_1", OrgID: "default", Kind: "crash_loop", Threshold: 1, WindowSeconds: 300, Enabled: true}},
		channels: []store.AlertChannel{{ID: "chan_1", OrgID: "default", Kind: "webhook", ConfigEnc: sealed}},
	}
	alerts := &Alerts{Store: fake, Box: box, HTTP: hook.Client()}
	if err := alerts.evaluate(context.Background(), "s_1", "exited"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := len(bodies)
	var payload map[string]string
	if got > 0 {
		_ = json.Unmarshal([]byte(bodies[0]), &payload)
	}
	mu.Unlock()
	if got != 1 || payload["content"] == "" {
		t.Fatalf("webhook bodies = %v", bodies)
	}
	if len(fake.created) != 1 || fake.created[0].State != "firing" {
		t.Fatalf("created = %+v", fake.created)
	}
	if err := alerts.evaluate(context.Background(), "s_1", "running"); err != nil {
		t.Fatal(err)
	}
	if len(fake.resolved) != 1 {
		t.Fatalf("resolved = %v", fake.resolved)
	}
}
