// Package config loads the panel and agent configuration files. A missing file
// yields the documented defaults; nothing is configured by environment magic.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Panel is the control-plane configuration.
type Panel struct {
	Listen      string `toml:"listen"`
	BasePath    string `toml:"base_path"`
	DatabaseURL string `toml:"database_url"`
	LogLevel    string `toml:"log_level"`
	DataDir     string `toml:"data_dir"`
	AdminToken  string `toml:"admin_token"`
	TLS         bool   `toml:"tls"`
}

// DefaultPanel returns the defaults used when no file exists.
func DefaultPanel() Panel {
	return Panel{
		Listen:   "127.0.0.1:8080",
		BasePath: "/",
		LogLevel: "info",
		DataDir:  "/var/lib/delphi",
		TLS:      true,
	}
}

// LoadPanel reads file. A missing file is not an error.
func LoadPanel(file string) (Panel, error) {
	cfg := DefaultPanel()
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(file, &cfg); err != nil {
		return Panel{}, fmt.Errorf("read %s: %w", file, err)
	}
	cfg.BasePath = NormalizeBasePath(cfg.BasePath)
	if err := cfg.validate(); err != nil {
		return Panel{}, fmt.Errorf("%s: %w", file, err)
	}
	return cfg, nil
}

func (p Panel) validate() error {
	if strings.TrimSpace(p.Listen) == "" {
		return errors.New("listen must not be empty")
	}
	if strings.TrimSpace(p.DataDir) == "" {
		return errors.New("data_dir must not be empty")
	}
	if _, err := ParseLevel(p.LogLevel); err != nil {
		return err
	}
	return nil
}

// SlogLevel returns the configured level, defaulting to info.
func (p Panel) SlogLevel() slog.Level {
	l, err := ParseLevel(p.LogLevel)
	if err != nil {
		return slog.LevelInfo
	}
	return l
}

// ParseLevel maps a config string to a slog level.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want debug, info, warn, error)", s)
	}
}

// NormalizeBasePath makes a path usable as a router prefix: leading slash,
// trailing slash, "/" for root.
func NormalizeBasePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/") + "/"
}

// Agent is the node-daemon configuration.
type Agent struct {
	PanelURL string   `toml:"panel_url"`
	Interval Duration `toml:"interval"`
	StateDir string   `toml:"state_dir"`
}

// DefaultAgent returns the defaults used when no file exists.
func DefaultAgent() Agent {
	return Agent{
		PanelURL: "https://127.0.0.1:8080",
		Interval: Duration(30 * time.Second),
		StateDir: "/etc/delphi",
	}
}

// LoadAgent reads file. A missing file is not an error.
func LoadAgent(file string) (Agent, error) {
	cfg := DefaultAgent()
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(file, &cfg); err != nil {
		return Agent{}, fmt.Errorf("read %s: %w", file, err)
	}
	if err := cfg.Validate(); err != nil {
		return Agent{}, fmt.Errorf("%s: %w", file, err)
	}
	return cfg, nil
}

// Validate reports whether the agent configuration can be used as-is.
func (a Agent) Validate() error {
	u, err := url.Parse(strings.TrimSpace(a.PanelURL))
	if err != nil || u.Host == "" {
		return errors.New("panel_url must be an absolute https URL")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("panel_url scheme %q must be https: the agent channel requires mutual TLS", u.Scheme)
	}
	if a.Interval <= 0 {
		return errors.New("interval must be positive")
	}
	if strings.TrimSpace(a.StateDir) == "" {
		return errors.New("state_dir must not be empty")
	}
	return nil
}

// Duration is a TOML-friendly time.Duration ("30s", "2m").
type Duration time.Duration

// Std returns the underlying duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}
