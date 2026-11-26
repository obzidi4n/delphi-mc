package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPanelDefaults(t *testing.T) {
	got := DefaultPanel()
	if got.Listen != "127.0.0.1:8080" || got.BasePath != "/" || got.LogLevel != "info" {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestLoadPanelMissingFileUsesDefaults(t *testing.T) {
	got, err := LoadPanel(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultPanel() {
		t.Fatalf("got %+v, want defaults", got)
	}
}

func TestLoadPanelOverridesDefaults(t *testing.T) {
	file := filepath.Join(t.TempDir(), "panel.toml")
	body := "listen = \"127.0.0.1:9999\"\nbase_path = \"delphi\"\ndatabase_url = \"postgres://x\"\nlog_level = \"debug\"\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadPanel(file)
	if err != nil {
		t.Fatal(err)
	}
	if got.Listen != "127.0.0.1:9999" || got.DatabaseURL != "postgres://x" {
		t.Fatalf("overrides not applied: %+v", got)
	}
	if got.BasePath != "/delphi/" {
		t.Fatalf("base path = %q, want /delphi/", got.BasePath)
	}
	if got.SlogLevel() != -4 { // slog.LevelDebug
		t.Fatalf("log level = %v, want debug", got.SlogLevel())
	}
}

func TestLoadPanelRejectsUnknownLogLevel(t *testing.T) {
	file := filepath.Join(t.TempDir(), "panel.toml")
	if err := os.WriteFile(file, []byte("log_level = \"loud\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPanel(file); err == nil {
		t.Fatal("expected an error for an unknown log level")
	}
}

func TestNormalizeBasePath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "/"},
		{"/", "/"},
		{"delphi", "/delphi/"},
		{"/delphi", "/delphi/"},
		{"/delphi/", "/delphi/"},
		{"  /a/b/  ", "/a/b/"},
	} {
		if got := NormalizeBasePath(tc.in); got != tc.want {
			t.Errorf("NormalizeBasePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAgentDefaultsAndDuration(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.toml")
	body := "panel_url = \"https://panel.example\"\ninterval = \"5s\"\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadAgent(file)
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval.Std() != 5*time.Second {
		t.Fatalf("interval = %v, want 5s", got.Interval.Std())
	}
	if got.PanelURL != "https://panel.example" {
		t.Fatalf("panel url = %q", got.PanelURL)
	}
}

func TestAgentValidate(t *testing.T) {
	base := DefaultAgent()

	cases := []struct {
		name  string
		cfg   Agent
		valid bool
	}{
		{"default", base, true},
		{"no scheme", Agent{PanelURL: "panel.example", Interval: base.Interval}, false},
		{"relative", Agent{PanelURL: "/panel", Interval: base.Interval}, false},
		{"bad scheme", Agent{PanelURL: "ftp://panel.example", Interval: base.Interval}, false},
		{"zero interval", Agent{PanelURL: base.PanelURL, Interval: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.valid && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
