package capability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"delphi.dev/delphi/internal/store"
)

// NetworkStore is network persistence.
type NetworkStore interface {
	CreateNetwork(ctx context.Context, n store.Network) (store.Network, error)
	SetNetworkProxy(ctx context.Context, id, proxyServerID string) error
	ListNetworks(ctx context.Context, orgID string) ([]store.Network, error)
	NetworkByID(ctx context.Context, orgID, id string) (store.Network, error)
	NetworkServers(ctx context.Context, networkID string) ([]store.Server, error)
	SetNetworkConfig(ctx context.Context, id, networkID, role string, files map[string]string) (store.Server, error)
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// Networks creates a proxy and backends on one container network.
type Networks struct {
	Store     NetworkStore
	Templates *Templates
	Servers   *Servers
}

// NetworkRequest creates a complete network.
type NetworkRequest struct {
	Name            string
	NodeID          string
	ProxyTemplate   string
	BackendTemplate string
	Backends        int
	AcceptEULA      bool
	Mode            string
}

// NetworkDetail is a network with its servers.
type NetworkDetail struct {
	Network store.Network
	Servers []store.Server
}

var networkSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

// Create builds the proxy and backends, writes forwarding configs, and starts
// backends before the proxy.
func (n *Networks) Create(ctx context.Context, p Principal, req NetworkRequest) (NetworkDetail, error) {
	if !p.Allows(orgScope(p, "server:create")) {
		return NetworkDetail{}, forbidden(orgScope(p, "server:create"))
	}
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !networkSlug.MatchString(name) {
		return NetworkDetail{}, invalid("name must be a lowercase slug (3-32 characters)")
	}
	if req.Mode == "" {
		req.Mode = "legacy"
	}
	if req.Mode != "legacy" {
		return NetworkDetail{}, invalid("only legacy forwarding is implemented")
	}
	if req.Backends <= 0 {
		req.Backends = 2
	}
	if req.Backends > 8 {
		return NetworkDetail{}, invalid("at most 8 backends")
	}
	if !req.AcceptEULA {
		return NetworkDetail{}, invalid("accept_eula is required for network backends")
	}
	if req.ProxyTemplate == "" {
		req.ProxyTemplate = "builtin-velocity"
	}
	if req.BackendTemplate == "" {
		req.BackendTemplate = "builtin-dev"
	}
	if n.Templates == nil || n.Servers == nil {
		return NetworkDetail{}, errors.New("network services are not configured")
	}
	proxyTpl, _, err := n.Templates.Get(ctx, p, req.ProxyTemplate)
	if err != nil {
		return NetworkDetail{}, err
	}
	if proxyTpl.Base != "velocity" {
		return NetworkDetail{}, invalid("proxy template must be a velocity template")
	}
	backendTpl, _, err := n.Templates.Get(ctx, p, req.BackendTemplate)
	if err != nil {
		return NetworkDetail{}, err
	}
	if backendTpl.Base != "paper" && backendTpl.Base != "purpur" {
		return NetworkDetail{}, invalid("backend template must be a paper or purpur template")
	}

	networkID, err := idWithPrefix("net_")
	if err != nil {
		return NetworkDetail{}, err
	}
	row, err := n.Store.CreateNetwork(ctx, store.Network{ID: networkID, OrgID: p.OrgID, Name: name, Mode: req.Mode})
	if err != nil {
		return NetworkDetail{}, err
	}

	proxy, err := n.Templates.CreateServer(ctx, p, InstantiateRequest{
		Name: name + "-proxy", NodeID: req.NodeID, TemplateID: req.ProxyTemplate,
		DesiredState: "stopped", AcceptEULA: true,
	})
	if err != nil {
		return NetworkDetail{}, err
	}
	backends := make([]store.Server, 0, req.Backends)
	for i := 1; i <= req.Backends; i++ {
		srv, err := n.Templates.CreateServer(ctx, p, InstantiateRequest{
			Name: name + "-" + strconv.Itoa(i), NodeID: req.NodeID, TemplateID: req.BackendTemplate,
			DesiredState: "stopped", AcceptEULA: true,
		})
		if err != nil {
			return NetworkDetail{}, err
		}
		backends = append(backends, srv)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return NetworkDetail{}, err
	}
	secretHex := hex.EncodeToString(secret)
	proxyFiles := copyFiles(proxy.Files)
	proxyFiles["forwarding.secret"] = secretHex
	proxyFiles["velocity.toml"] = velocityConfig(name, proxy.Port, backends, secretHex, req.Mode)
	if _, err := n.Store.SetNetworkConfig(ctx, proxy.ID, networkID, "proxy", proxyFiles); err != nil {
		return NetworkDetail{}, err
	}
	for _, srv := range backends {
		files := copyFiles(srv.Files)
		files["server.properties"] = offlineServerProperties(files["server.properties"], srv.Name, srv.Port)
		files["spigot.yml"] = "settings:\n  bungeecord: true\n"
		if _, err := n.Store.SetNetworkConfig(ctx, srv.ID, networkID, "backend", files); err != nil {
			return NetworkDetail{}, err
		}
	}
	if err := n.Store.SetNetworkProxy(ctx, networkID, proxy.ID); err != nil {
		return NetworkDetail{}, err
	}

	// Start backends first so the proxy's first join can resolve one.
	for _, srv := range backends {
		if _, err := n.Servers.Power(ctx, p, srv.ID, "start"); err != nil {
			return NetworkDetail{}, err
		}
	}
	started, err := n.Servers.Power(ctx, p, proxy.ID, "start")
	if err != nil {
		return NetworkDetail{}, err
	}
	detail, err := n.Get(ctx, p, networkID)
	if err != nil {
		return NetworkDetail{}, err
	}
	_ = n.Store.Audit(ctx, p.OrgID, p.UserID, p.Via, "network.create", networkID, map[string]any{
		"name": name, "proxy": started.ID, "backends": len(backends), "mode": req.Mode,
	})
	_ = row
	return detail, nil
}

// Get returns a network and its servers.
func (n *Networks) Get(ctx context.Context, p Principal, id string) (NetworkDetail, error) {
	if !p.Allows(orgScope(p, "read")) {
		return NetworkDetail{}, forbidden(orgScope(p, "read"))
	}
	row, err := n.Store.NetworkByID(ctx, p.OrgID, id)
	if err != nil {
		return NetworkDetail{}, err
	}
	servers, err := n.Store.NetworkServers(ctx, id)
	if err != nil {
		return NetworkDetail{}, err
	}
	return NetworkDetail{Network: row, Servers: servers}, nil
}

// List returns an org's networks.
func (n *Networks) List(ctx context.Context, p Principal) ([]store.Network, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, forbidden(orgScope(p, "read"))
	}
	return n.Store.ListNetworks(ctx, p.OrgID)
}

func velocityConfig(name string, port int, backends []store.Server, secret, mode string) string {
	var b strings.Builder
	b.WriteString("config-version = \"2.9\"\n")
	fmt.Fprintf(&b, "bind = \"0.0.0.0:%d\"\n", port)
	fmt.Fprintf(&b, "motd = %s\n", strconv.Quote(name))
	b.WriteString("show-max-players = 100\n")
	b.WriteString("online-mode = true\n")
	fmt.Fprintf(&b, "player-info-forwarding-mode = %q\n", mode)
	b.WriteString("forwarding-secret-file = \"forwarding.secret\"\n\n")
	b.WriteString("[servers]\n")
	names := make([]string, 0, len(backends))
	for i, srv := range backends {
		key := fmt.Sprintf("backend-%d", i+1)
		names = append(names, key)
		fmt.Fprintf(&b, "%s = \"%s:%d\"\n", key, networkAlias(srv.ID), srv.Port)
	}
	fmt.Fprintf(&b, "\ntry = [%s]\n", quoteJoin(names))
	b.WriteString("\n[forced-hosts]\n")
	return b.String()
}

func quoteJoin(items []string) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, strconv.Quote(item))
	}
	return strings.Join(out, ", ")
}

func copyFiles(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+2)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// offlineServerProperties patches a template's server.properties for proxy
// forwarding: offline backend, explicit port, no wildcard surprises.
func offlineServerProperties(existing, name string, port int) string {
	if strings.TrimSpace(existing) == "" {
		existing = "level-name=world\nmotd=" + name + "\n"
	}
	seen := map[string]bool{}
	var lines []string
	for _, line := range strings.Split(existing, "\n") {
		key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		switch key {
		case "server-port":
			lines = append(lines, fmt.Sprintf("server-port=%d", port))
			seen[key] = true
		case "server-ip":
			lines = append(lines, "server-ip=0.0.0.0")
			seen[key] = true
		case "online-mode":
			lines = append(lines, "online-mode=false")
			seen[key] = true
		default:
			if line != "" {
				lines = append(lines, line)
			}
		}
	}
	if !seen["server-port"] {
		lines = append(lines, fmt.Sprintf("server-port=%d", port))
	}
	if !seen["server-ip"] {
		lines = append(lines, "server-ip=0.0.0.0")
	}
	if !seen["online-mode"] {
		lines = append(lines, "online-mode=false")
	}
	return strings.Join(lines, "\n") + "\n"
}

func idWithPrefix(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf), nil
}
