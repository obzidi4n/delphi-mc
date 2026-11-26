// Command delphi runs the control plane, the node daemon, and first-run setup.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/BurntSushi/toml"

	"delphi.dev/delphi/internal/agent"
	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/config"
	"delphi.dev/delphi/internal/pki"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/version"
)

const (
	defaultPanelConfig   = "/etc/delphi/panel.toml"
	defaultAgentConfig   = "/etc/delphi/agent.toml"
	defaultAgentStateDir = "/etc/delphi"
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
	case "nodes":
		err = runNodes(ctx, os.Args[2:])
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
  panel             run the control plane
  agent enroll      enroll this node with a token and CA certificate
  agent run         keep the agent channel open
  nodes list        list nodes (admin token)
  nodes token       issue an enrollment token (admin token)
  nodes revoke ID   revoke a node certificate (admin token)
  init              write config, create the CA, apply migrations
  version           print the build version

flags are per command; run "delphi <command> -h" for details.
`)
}

func runPanel(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("panel", flag.ContinueOnError)
	configPath := fs.String("config", defaultPanelConfig, "panel config file")
	listen := fs.String("listen", "", "override the listen address")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, err := config.LoadPanel(*configPath)
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.Listen = *listen
	}

	log := newLogger(cfg.SlogLevel())
	slog.SetDefault(log)

	ready := func(context.Context) error { return errors.New("database not configured") }
	var nodes api.NodeStore
	var db *store.DB
	if cfg.DatabaseURL == "" {
		log.Warn("no database configured; /readyz will report unready")
	} else {
		db, err = store.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer db.Close()
		ready = db.Ping
		nodes = db
		go sweepNodes(ctx, db, log)
	}

	hub := api.NewHub()
	var serversCap *capability.Servers
	if db != nil {
		serversCap = &capability.Servers{Store: db, Notify: hub}
		hub.Build = serversCap.SpecsForNode
	}

	var ca *pki.CA
	var tlsConfig *tls.Config
	if cfg.TLS {
		ca, err = pki.LoadOrCreateCA(pki.Dir(cfg.DataDir))
		if err != nil {
			return err
		}
		cert, err := pki.EnsureServerCert(pki.Dir(cfg.DataDir), ca, listenHost(cfg.Listen))
		if err != nil {
			return err
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.VerifyClientCertIfGiven,
			ClientCAs:    ca.Pool(),
			MinVersion:   tls.VersionTLS12,
		}
	} else {
		log.Warn("tls is disabled; the agent channel is unavailable and enrollment returns 501")
	}

	if cfg.AdminToken == "" {
		log.Warn("admin_token is empty; /api/v1 is disabled until it is set")
	}

	handler, err := api.New(api.Options{
		BasePath:   cfg.BasePath,
		Version:    version.Version,
		Ready:      ready,
		Nodes:      nodes,
		AdminToken: cfg.AdminToken,
		CA:         ca,
		Servers:    serversCap,
		Hub:        hub,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         tlsConfig,
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	log.Info("panel listening",
		"addr", cfg.Listen,
		"scheme", scheme,
		"base_path", cfg.BasePath,
		"data_dir", cfg.DataDir,
		"version", version.Version,
	)

	if cfg.TLS {
		if err := srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func sweepNodes(ctx context.Context, db *store.DB, log *slog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed, err := db.SweepStaleNodes(ctx, time.Now().Add(-90*time.Second))
			if err != nil {
				log.Warn("node sweep failed", "err", err)
				continue
			}
			if changed > 0 {
				log.Info("nodes marked unhealthy", "count", changed)
			}
		}
	}
}

func runAgent(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "enroll":
			return runAgentEnroll(ctx, args[1:])
		case "run":
			args = args[1:]
		}
	}

	fs := flag.NewFlagSet("agent run", flag.ContinueOnError)
	configPath := fs.String("config", defaultAgentConfig, "agent config file")
	panelURL := fs.String("panel", "", "override the panel URL")
	stateDir := fs.String("state-dir", "", "override the identity state dir")
	dataDir := fs.String("data-dir", "", "override the server data dir")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, err := config.LoadAgent(*configPath)
	if err != nil {
		return err
	}
	if *panelURL != "" {
		cfg.PanelURL = *panelURL
	}
	if *stateDir != "" {
		cfg.StateDir = *stateDir
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	log := newLogger(slog.LevelInfo)
	slog.SetDefault(log)

	runtime, err := agent.DetectRuntime(cfg.Runtime)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	supervisor := agent.NewSupervisor(cfg.DataDir, runtime, log)
	fsCapability := agent.DetectFSCapability(cfg.DataDir)
	log.Info("agent supervisor ready",
		"runtime", runtime.Name(),
		"data_dir", cfg.DataDir,
		"fs_capability", fsCapability,
	)

	return agent.Connect(ctx, agent.ConnectOptions{
		PanelURL:     cfg.PanelURL,
		StateDir:     cfg.StateDir,
		AgentVersion: version.Version,
		Interval:     cfg.Interval.Std(),
		Supervisor:   supervisor,
		FSCapability: fsCapability,
	}, log)
}

func runAgentEnroll(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent enroll", flag.ContinueOnError)
	panelURL := fs.String("panel", "", "panel base URL (https)")
	token := fs.String("token", "", "enrollment token (prefer DELPHI_ENROLL_TOKEN)")
	name := fs.String("name", "", "node name (default: hostname)")
	caPath := fs.String("ca", "", "panel CA certificate PEM")
	stateDir := fs.String("state-dir", defaultAgentStateDir, "identity state dir")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *panelURL == "" || *caPath == "" {
		return errors.New("enroll: --panel and --ca are required")
	}
	if *token == "" {
		*token = os.Getenv("DELPHI_ENROLL_TOKEN")
	}
	if *token == "" {
		return errors.New("enroll: provide DELPHI_ENROLL_TOKEN (preferred) or --token")
	}
	if *name == "" {
		if host, err := os.Hostname(); err == nil {
			*name = host
		}
	}

	caPEM, err := os.ReadFile(*caPath)
	if err != nil {
		return fmt.Errorf("read CA: %w", err)
	}

	nodeID, err := agent.Enroll(ctx, agent.EnrollOptions{
		PanelURL: *panelURL,
		Token:    *token,
		Name:     *name,
		StateDir: *stateDir,
		CAPEM:    caPEM,
	})
	if err != nil {
		return err
	}
	fmt.Printf("enrolled node: %s\nidentity: %s\n", nodeID, *stateDir)
	return nil
}

type adminConfig struct {
	panel string
	token string
	ca    string
}

func addAdminFlags(fs *flag.FlagSet, a *adminConfig) {
	fs.StringVar(&a.panel, "panel", "", "panel base URL (https)")
	fs.StringVar(&a.token, "token", "", "admin token (or DELPHI_ADMIN_TOKEN)")
	fs.StringVar(&a.ca, "ca", "", "panel CA certificate PEM")
}

func (a *adminConfig) resolve() error {
	if a.token == "" {
		a.token = os.Getenv("DELPHI_ADMIN_TOKEN")
	}
	if a.panel == "" || a.ca == "" || a.token == "" {
		return errors.New("--panel, --ca and an admin token (or DELPHI_ADMIN_TOKEN) are required")
	}
	return nil
}

func (a *adminConfig) request(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	caPEM, err := os.ReadFile(a.ca)
	if err != nil {
		return nil, 0, fmt.Errorf("read CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, 0, errors.New("the CA file is not valid PEM")
	}
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}},
	}

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.panel, "/")+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return payload, resp.StatusCode, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	return payload, resp.StatusCode, nil
}

func runNodes(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: delphi nodes <list|token|revoke> [flags]")
	}
	sub, rest := args[0], args[1:]

	// Flags come before the id as usual, but "revoke <id> --panel …" reads
	// naturally, so lift the positional out before parsing.
	var id string
	if sub == "revoke" {
		id, rest = extractPositional(rest, map[string]bool{"panel": true, "token": true, "ca": true, "ttl": true})
	}

	fs := flag.NewFlagSet("nodes "+sub, flag.ContinueOnError)
	var admin adminConfig
	addAdminFlags(fs, &admin)
	jsonOut := fs.Bool("json", false, "print JSON")
	ttl := fs.Int("ttl", 900, "token lifetime in seconds")
	if err := parseFlags(fs, rest); err != nil {
		return err
	}
	if err := admin.resolve(); err != nil {
		return err
	}

	switch sub {
	case "list":
		payload, _, err := admin.request(ctx, http.MethodGet, "/api/v1/nodes", nil)
		if err != nil {
			return err
		}
		if *jsonOut {
			fmt.Println(strings.TrimSpace(string(payload)))
			return nil
		}
		var out struct {
			Items []struct {
				ID           string     `json:"id"`
				Name         string     `json:"name"`
				Status       string     `json:"status"`
				AgentVersion string     `json:"agent_version"`
				FSCapability string     `json:"fs_capability"`
				LastSeen     *time.Time `json:"last_seen"`
				CertExpires  time.Time  `json:"cert_expires_at"`
				RevokedAt    *time.Time `json:"revoked_at"`
			} `json:"items"`
		}
		if err := json.Unmarshal(payload, &out); err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSTATUS\tVERSION\tFS\tLAST SEEN\tID")
		for _, n := range out.Items {
			last := "-"
			if n.LastSeen != nil {
				last = n.LastSeen.Local().Format("15:04:05")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				n.Name, n.Status, dash(n.AgentVersion), n.FSCapability, last, n.ID)
		}
		return w.Flush()

	case "token":
		payload, _, err := admin.request(ctx, http.MethodPost, "/api/v1/enrollment-tokens",
			map[string]int{"ttl_seconds": *ttl})
		if err != nil {
			return err
		}
		var out struct {
			Token     string    `json:"token"`
			ExpiresAt time.Time `json:"expires_at"`
		}
		if err := json.Unmarshal(payload, &out); err != nil {
			return err
		}
		if *jsonOut {
			fmt.Println(strings.TrimSpace(string(payload)))
			return nil
		}
		fmt.Printf("token: %s\nexpires: %s\n", out.Token, out.ExpiresAt.Local().Format(time.RFC3339))
		return nil

	case "revoke":
		if id == "" {
			return errors.New("usage: delphi nodes revoke <id> [flags]")
		}
		if _, _, err := admin.request(ctx, http.MethodPost, "/api/v1/nodes/"+id+"/revoke", nil); err != nil {
			return err
		}
		fmt.Printf("revoked: %s\n", id)
		return nil

	default:
		return fmt.Errorf("unknown nodes command %q", sub)
	}
}

func runInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := fs.String("config", defaultPanelConfig, "panel config file to create or read")
	databaseURL := fs.String("database-url", "", "database DSN (required when the config has none)")
	dataDir := fs.String("data-dir", "", "override data_dir")
	listen := fs.String("listen", "", "override listen (also sets the certificate SAN host)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, err := config.LoadPanel(*configPath)
	if err != nil {
		return err
	}
	if *databaseURL != "" {
		cfg.DatabaseURL = *databaseURL
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if cfg.DatabaseURL == "" {
		return errors.New("init: set database_url in the config or pass --database-url")
	}

	created := false
	adminToken := ""
	if _, err := os.Stat(*configPath); errors.Is(err, os.ErrNotExist) {
		if cfg.AdminToken == "" {
			cfg.AdminToken = randomToken()
			adminToken = cfg.AdminToken
		}
		if err := writePanelConfig(*configPath, cfg); err != nil {
			return err
		}
		created = true
	} else if cfg.AdminToken == "" {
		fmt.Fprintln(os.Stderr, "warning: admin_token is empty; set it to use /api/v1 (delphi nodes ...)")
	}

	ca, err := pki.LoadOrCreateCA(pki.Dir(cfg.DataDir))
	if err != nil {
		return err
	}
	if _, err := pki.EnsureServerCert(pki.Dir(cfg.DataDir), ca, listenHost(cfg.Listen)); err != nil {
		return err
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

	fmt.Printf("config:   %s (created=%t)\n", *configPath, created)
	fmt.Printf("data dir: %s\n", cfg.DataDir)
	fmt.Printf("panel CA: %s\n", pki.CACertPath(cfg.DataDir))
	fmt.Printf("migrations applied: %d\n", len(applied))
	for _, name := range applied {
		fmt.Printf("  %s\n", name)
	}
	if adminToken != "" {
		fmt.Printf("admin token (store it now, not shown again): %s\n", adminToken)
	}
	return nil
}

// extractPositional removes the first non-flag argument, skipping values of
// known string flags, so a command can accept "revoke <id> --panel …".
func extractPositional(args []string, valueFlags map[string]bool) (string, []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			name := strings.TrimLeft(arg, "-")
			if strings.ContainsRune(name, '=') {
				continue
			}
			if valueFlags[name] {
				i++
			}
			continue
		}
		rest := append(append([]string{}, args[:i]...), args[i+1:]...)
		return arg, rest
	}
	return "", args
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	return nil
}

func newLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func listenHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func randomToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic("delphi: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func writePanelConfig(file string, cfg config.Panel) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	var body bytes.Buffer
	body.WriteString("# DelphiMC panel configuration. See docs/reference/configuration.md.\n")
	if err := toml.NewEncoder(&body).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(file, body.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	return nil
}
