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
	"delphi.dev/delphi/internal/apiclient"
	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/auth"
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
	case "servers":
		err = runServers(ctx, os.Args[2:])
	case "audit":
		err = runAudit(ctx, os.Args[2:])
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
  nodes list        list nodes
  nodes token       issue an enrollment token
  nodes revoke ID   revoke a node certificate
  servers list      list servers
  servers start ID  start / stop / restart / kill a server
  servers delete ID delete a server (data kept 7 days)
  audit list        recent mutations
  init              write config, create the CA and owner, apply migrations
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
	var services *capability.Services
	var identity api.IdentityStore
	if db != nil {
		identity = db
		servers := &capability.Servers{Store: db, Notify: hub}
		services = &capability.Services{
			Servers: servers,
			Fleet:   &capability.Fleet{Store: db},
			Groups:  &capability.Groups{Store: db},
			Audit:   &capability.Audit{Store: db},
			Keys:    &capability.Keys{Store: db},
			Users:   &capability.Users{Store: db},
		}
		hub.Build = servers.SpecsForNode
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
		Services:   services,
		Identity:   identity,
		AdminToken: cfg.AdminToken,
		CA:         ca,
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
		a.token = os.Getenv("DELPHI_API_TOKEN")
	}
	if a.token == "" {
		a.token = os.Getenv("DELPHI_ADMIN_TOKEN")
	}
	if a.panel == "" || a.ca == "" || a.token == "" {
		return errors.New("--panel, --ca and an admin token (or DELPHI_ADMIN_TOKEN) are required")
	}
	return nil
}

func (a *adminConfig) newClient() (*apiclient.Client, error) {
	caPEM, err := os.ReadFile(a.ca)
	if err != nil {
		return nil, fmt.Errorf("read CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("the CA file is not valid PEM")
	}
	hc := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}},
	}
	return apiclient.New(a.panel, hc, a.token), nil
}

func optString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func printJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
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
	limit := fs.Int("limit", 50, "nodes per page")
	after := fs.String("after", "", "cursor from a previous page")
	if err := parseFlags(fs, rest); err != nil {
		return err
	}
	if err := admin.resolve(); err != nil {
		return err
	}

	switch sub {
	case "list":
		client, err := admin.newClient()
		if err != nil {
			return err
		}
		pageSize := int64(*limit)
		out, err := client.ListNodes(ctx, &pageSize, optString(*after))
		if err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(out)
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
		if err := w.Flush(); err != nil {
			return err
		}
		if out.NextCursor != nil {
			fmt.Printf("next: %s\n", *out.NextCursor)
		}
		return nil

	case "token":
		client, err := admin.newClient()
		if err != nil {
			return err
		}
		ttlSeconds := *ttl
		out, err := client.CreateEnrollmentToken(ctx, apitypes.EnrollmentTokenRequest{TTLSeconds: &ttlSeconds})
		if err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(out)
		}
		fmt.Printf("token: %s\nexpires: %s\n", out.Token, out.ExpiresAt.Local().Format(time.RFC3339))
		return nil

	case "revoke":
		if id == "" {
			return errors.New("usage: delphi nodes revoke <id> [flags]")
		}
		client, err := admin.newClient()
		if err != nil {
			return err
		}
		if err := client.RevokeNode(ctx, id); err != nil {
			return err
		}
		fmt.Printf("revoked: %s\n", id)
		return nil

	default:
		return fmt.Errorf("unknown nodes command %q", sub)
	}
}

func runServers(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: delphi servers <list|get|start|stop|restart|kill|delete|rename> [flags]")
	}
	sub, rest := args[0], args[1:]
	valueFlags := map[string]bool{"panel": true, "token": true, "ca": true}

	var id, name string
	switch sub {
	case "get", "start", "stop", "restart", "kill", "delete":
		id, rest = extractPositional(rest, valueFlags)
	case "rename":
		id, rest = extractPositional(rest, valueFlags)
		name, rest = extractPositional(rest, valueFlags)
	}

	fs := flag.NewFlagSet("servers "+sub, flag.ContinueOnError)
	var admin adminConfig
	addAdminFlags(fs, &admin)
	jsonOut := fs.Bool("json", false, "print JSON")
	limit := fs.Int("limit", 50, "servers per page")
	after := fs.String("after", "", "cursor from a previous page")
	if err := parseFlags(fs, rest); err != nil {
		return err
	}
	if err := admin.resolve(); err != nil {
		return err
	}

	switch sub {
	case "list":
		client, err := admin.newClient()
		if err != nil {
			return err
		}
		pageSize := int64(*limit)
		out, err := client.ListServers(ctx, &pageSize, optString(*after))
		if err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(out)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tDESIRED\tOBSERVED\tPORT\tID")
		for _, srv := range out.Items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
				srv.Name, srv.DesiredState, srv.ObservedState, srv.Port, srv.ID)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		if out.NextCursor != nil {
			fmt.Printf("next: %s\n", *out.NextCursor)
		}
		return nil

	case "get":
		if id == "" {
			return errors.New("usage: delphi servers get <id> [flags]")
		}
		client, err := admin.newClient()
		if err != nil {
			return err
		}
		srv, err := client.GetServer(ctx, id)
		if err != nil {
			return err
		}
		return printJSON(srv)

	case "start", "stop", "restart", "kill":
		if id == "" {
			return fmt.Errorf("usage: delphi servers %s <id> [flags]", sub)
		}
		client, err := admin.newClient()
		if err != nil {
			return err
		}
		srv, err := client.PowerServer(ctx, id, apitypes.PowerRequest{Action: sub})
		if err != nil {
			return err
		}
		fmt.Printf("%s: desired=%s revision=%d\n", srv.Name, srv.DesiredState, srv.DesiredRevision)
		return nil

	case "delete":
		if id == "" {
			return errors.New("usage: delphi servers delete <id> [flags]")
		}
		client, err := admin.newClient()
		if err != nil {
			return err
		}
		existing, err := client.GetServer(ctx, id)
		if err != nil {
			return err
		}
		if _, err := client.DeleteServer(ctx, id, existing.Name); err != nil {
			return err
		}
		fmt.Printf("deleted: %s (data kept for 7 days)\n", existing.Name)
		return nil

	case "rename":
		if id == "" || name == "" {
			return errors.New("usage: delphi servers rename <id> <name> [flags]")
		}
		client, err := admin.newClient()
		if err != nil {
			return err
		}
		srv, err := client.RenameServer(ctx, id, apitypes.RenameRequest{Name: name})
		if err != nil {
			return err
		}
		fmt.Printf("%s: renamed to %s\n", srv.ID, srv.Name)
		return nil

	default:
		return fmt.Errorf("unknown servers command %q", sub)
	}
}

func runAudit(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "list" {
		args = args[1:]
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("unknown audit command %q (want: list)", args[0])
	}
	fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
	var admin adminConfig
	addAdminFlags(fs, &admin)
	limit := fs.Int("limit", 50, "entries to show")
	after := fs.String("after", "", "cursor from a previous page")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if err := admin.resolve(); err != nil {
		return err
	}

	client, err := admin.newClient()
	if err != nil {
		return err
	}
	limit64 := int64(*limit)
	out, err := client.ListAudit(ctx, &limit64, optString(*after))
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(out)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEQ\tWHEN\tACTOR\tVIA\tACTION\tTARGET")
	for _, e := range out.Items {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			e.Seq, e.CreatedAt.Local().Format("15:04:05"), e.Actor, e.Via, e.Action, e.Target)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if out.NextCursor != nil {
		fmt.Printf("next: %s\n", *out.NextCursor)
	}
	return nil
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
	if _, err := os.Stat(*configPath); errors.Is(err, os.ErrNotExist) {
		if err := writePanelConfig(*configPath, cfg); err != nil {
			return err
		}
		created = true
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
	if err := ensureOwner(ctx, db); err != nil {
		return err
	}
	return nil
}

// ensureOwner creates the single owner account and its bootstrap API key once.
func ensureOwner(ctx context.Context, db *store.DB) error {
	const email = "owner@delphi.local"
	if _, err := db.UserByEmail(ctx, "default", email); err == nil {
		fmt.Printf("owner: %s (exists)\n", email)
		return nil
	} else if !errors.Is(err, store.ErrNoUser) {
		return err
	}

	password := auth.NewSecret()
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	user, err := db.CreateUser(ctx, "default", email, hash)
	if err != nil {
		return err
	}
	if err := db.SetOrgRole(ctx, user.ID, "default", "owner"); err != nil {
		return err
	}
	token, _, err := db.CreateAPIKey(ctx, "default", user.ID, "bootstrap", []string{"*"})
	if err != nil {
		return err
	}
	fmt.Printf("owner: %s\npassword: %s\napi token: %s\n", email, password, token)
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
