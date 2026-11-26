// Command delphi runs the control plane, the node daemon, and first-run setup.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"

	"delphi.dev/delphi/internal/agent"
	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/config"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/version"
)

const (
	defaultPanelConfig = "/etc/delphi/panel.toml"
	defaultAgentConfig = "/etc/delphi/agent.toml"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "panel":
		err = runPanel(ctx, os.Args[2:])
	case "agent":
		err = runAgent(ctx, os.Args[2:])
	case "init":
		err = runInit(ctx, os.Args[2:])
	case "version":
		fmt.Println(version.String())
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		slog.Error("delphi", "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `DelphiMC — Minecraft server control plane

usage: delphi <command> [flags]

commands:
  panel    run the control plane
  agent    run the node daemon
  init     write panel config and apply migrations
  version  print the build version

flags are per command; run "delphi <command> -h" for details.
`)
}

func runPanel(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("panel", flag.ContinueOnError)
	configPath := fs.String("config", defaultPanelConfig, "panel config file")
	listen := fs.String("listen", "", "override the listen address")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg, err := config.LoadPanel(*configPath)
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.Listen = *listen
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	slog.SetDefault(log)

	ready := func(context.Context) error { return errors.New("database not configured") }
	if cfg.DatabaseURL == "" {
		log.Warn("no database configured; /readyz will report unready")
	} else {
		db, err := store.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer db.Close()
		ready = db.Ping
	}

	handler, err := api.New(api.Options{
		BasePath: cfg.BasePath,
		Version:  version.Version,
		Ready:    ready,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("panel listening",
		"addr", cfg.Listen,
		"base_path", cfg.BasePath,
		"version", version.Version,
	)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runAgent(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	configPath := fs.String("config", defaultAgentConfig, "agent config file")
	panelURL := fs.String("panel", "", "override the panel URL")
	once := fs.Bool("once", false, "probe once and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg, err := config.LoadAgent(*configPath)
	if err != nil {
		return err
	}
	if *panelURL != "" {
		cfg.PanelURL = *panelURL
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	return agent.Run(ctx, agent.Config{
		PanelURL: cfg.PanelURL,
		Interval: cfg.Interval.Std(),
	}, log, *once)
}

func runInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := fs.String("config", defaultPanelConfig, "panel config file to create or read")
	databaseURL := fs.String("database-url", "", "database DSN (required when the config has none)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg, err := config.LoadPanel(*configPath)
	if err != nil {
		return err
	}
	if *databaseURL != "" {
		cfg.DatabaseURL = *databaseURL
	}
	if cfg.DatabaseURL == "" {
		return errors.New("init: set database_url in the config or pass --database-url")
	}

	created := false
	if _, err := os.Stat(*configPath); errors.Is(err, os.ErrNotExist) {
		if err := writePanelConfig(*configPath, cfg); err != nil {
			return err
		}
		created = true
	}

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	applied, err := db.Migrate(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("config: %s (created=%t)\n", *configPath, created)
	fmt.Printf("migrations applied: %d\n", len(applied))
	for _, name := range applied {
		fmt.Printf("  %s\n", name)
	}
	return nil
}

func writePanelConfig(file string, cfg config.Panel) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	var body bytes.Buffer
	body.WriteString("# DelphiMC panel configuration. See docs/operations/install.md.\n")
	if err := toml.NewEncoder(&body).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(file, []byte(body.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	return nil
}
