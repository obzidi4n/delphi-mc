package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"delphi.dev/delphi/internal/mc"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/template"
)

// TemplateStore is template persistence.
type TemplateStore interface {
	ListTemplates(ctx context.Context, orgID string, limit int, after string) ([]store.Template, string, error)
	TemplateByID(ctx context.Context, orgID, id string) (store.Template, error)
	ImportTemplate(ctx context.Context, orgID, name string, body []byte) (store.Template, error)
	UpdateTemplate(ctx context.Context, orgID, id, name string, body []byte) (store.Template, error)
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// ServerCreator is the server capability as templates need it.
type ServerCreator interface {
	Create(ctx context.Context, p Principal, req CreateRequest) (store.Server, error)
}

// PortAllocator finds a free port before the server row exists.
type PortAllocator interface {
	AllocatePort(ctx context.Context, nodeID string) (int, error)
}

// Templates lists, imports, and instantiates templates.
type Templates struct {
	Store    TemplateStore
	Servers  ServerCreator
	Ports    PortAllocator
	Resolver mc.Resolver
	Plugins  *Updates
}

// TemplateList is a page of stored templates.
type TemplateList struct {
	Items []store.Template
	Next  string
}

// List returns built-ins and the org's templates.
func (t *Templates) List(ctx context.Context, p Principal, limit int, after string) (TemplateList, error) {
	if !p.Allows(orgScope(p, "read")) {
		return TemplateList{}, forbidden(orgScope(p, "read"))
	}
	items, next, err := t.Store.ListTemplates(ctx, p.OrgID, limit, after)
	return TemplateList{Items: items, Next: next}, err
}

// Get loads and parses one template.
func (t *Templates) Get(ctx context.Context, p Principal, id string) (template.Template, store.Template, error) {
	if !p.Allows(orgScope(p, "read")) {
		return template.Template{}, store.Template{}, forbidden(orgScope(p, "read"))
	}
	row, err := t.Store.TemplateByID(ctx, p.OrgID, id)
	if err != nil {
		return template.Template{}, store.Template{}, err
	}
	tpl, err := template.Parse(row.Body)
	if err != nil {
		return template.Template{}, store.Template{}, fmt.Errorf("stored template %s is invalid: %w", id, err)
	}
	return tpl, row, nil
}

// Import validates and stores a template body for the org.
func (t *Templates) Import(ctx context.Context, p Principal, body []byte) (template.Template, store.Template, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return template.Template{}, store.Template{}, forbidden(orgScope(p, "admin"))
	}
	tpl, err := template.Parse(body)
	if err != nil {
		return template.Template{}, store.Template{}, invalid("%v", err)
	}
	canonical, err := json.Marshal(tpl)
	if err != nil {
		return template.Template{}, store.Template{}, err
	}
	row, err := t.Store.ImportTemplate(ctx, p.OrgID, tpl.Name, canonical)
	if err != nil {
		return template.Template{}, store.Template{}, err
	}
	if err := t.Store.Audit(ctx, p.OrgID, actor(p), p.Via, "template.import", row.ID,
		map[string]string{"name": tpl.Name, "base": tpl.Base}); err != nil {
		return template.Template{}, store.Template{}, err
	}
	return tpl, row, nil
}

// Update validates and writes an org-owned template, preserving its id and
// incrementing its revision. Built-ins are immutable.
func (t *Templates) Update(ctx context.Context, p Principal, id string, body []byte) (template.Template, store.Template, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return template.Template{}, store.Template{}, forbidden(orgScope(p, "admin"))
	}
	row, err := t.Store.TemplateByID(ctx, p.OrgID, id)
	if err != nil {
		return template.Template{}, store.Template{}, err
	}
	if row.Builtin {
		return template.Template{}, store.Template{}, invalid("built-in templates are read-only; duplicate it to edit")
	}
	tpl, err := template.Parse(body)
	if err != nil {
		return template.Template{}, store.Template{}, invalid("%v", err)
	}
	canonical, err := json.Marshal(tpl)
	if err != nil {
		return template.Template{}, store.Template{}, err
	}
	updated, err := t.Store.UpdateTemplate(ctx, p.OrgID, id, tpl.Name, canonical)
	if err != nil {
		return template.Template{}, store.Template{}, err
	}
	if err := t.Store.Audit(ctx, p.OrgID, actor(p), p.Via, "template.update", id,
		map[string]string{"name": tpl.Name, "base": tpl.Base}); err != nil {
		return template.Template{}, store.Template{}, err
	}
	return tpl, updated, nil
}

// InstantiateRequest is a create-from-template request.
type InstantiateRequest struct {
	Name         string
	NodeID       string
	TemplateID   string
	Overrides    map[string]string
	DesiredState string
	AcceptEULA   bool
}

// CreateServer resolves a template and creates a server from it.
func (t *Templates) CreateServer(ctx context.Context, p Principal, req InstantiateRequest) (store.Server, error) {
	if !p.Allows(orgScope(p, "server:create")) {
		return store.Server{}, forbidden(orgScope(p, "server:create"))
	}
	if strings.TrimSpace(req.TemplateID) == "" {
		return store.Server{}, invalid("template is required")
	}
	if req.NodeID == "" {
		return store.Server{}, invalid("node_id is required")
	}
	tpl, row, err := t.Get(ctx, p, req.TemplateID)
	if err != nil {
		return store.Server{}, err
	}

	memory := tpl.Defaults.MemoryMB
	if memory == 0 {
		memory = 4096
	}
	cpu := tpl.Defaults.CPUCores
	if cpu == 0 {
		cpu = 1
	}
	port := tpl.Defaults.Port
	java := tpl.Java
	custom := map[string]string{}
	for key, value := range req.Overrides {
		switch key {
		case "memory_mb":
			n, err := strconv.Atoi(value)
			if err != nil {
				return store.Server{}, invalid("memory_mb must be an integer")
			}
			memory = n
		case "cpu_cores":
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return store.Server{}, invalid("cpu_cores must be a number")
			}
			cpu = n
		case "port":
			n, err := strconv.Atoi(value)
			if err != nil {
				return store.Server{}, invalid("port must be an integer")
			}
			port = n
		case "java":
			n, err := strconv.Atoi(value)
			if err != nil {
				return store.Server{}, invalid("java must be an integer")
			}
			java = n
		default:
			custom[key] = value
		}
	}

	artifact, err := t.Resolver.Resolve(ctx, tpl.Base, tpl.MCVersion)
	if err != nil {
		return store.Server{}, err
	}
	if port == 0 {
		if t.Ports == nil {
			return store.Server{}, invalid("a port is required")
		}
		port, err = t.Ports.AllocatePort(ctx, req.NodeID)
		if err != nil {
			return store.Server{}, err
		}
	}

	inst, err := tpl.Render(template.Input{
		Name: req.Name, Port: port, MemoryMB: memory, CPUCores: cpu,
		Java: java, MCVersion: artifact.Version, Overrides: custom,
	})
	if err != nil {
		return store.Server{}, invalid("%v", err)
	}
	acceptEULA := req.AcceptEULA || tpl.Base == "velocity"
	var managed []store.ServerPlugin
	if t.Plugins != nil && len(tpl.Plugins) > 0 {
		managed, err = t.Plugins.Install(ctx, artifact.Version, tpl.Plugins)
		if err != nil {
			return store.Server{}, invalid("resolve plugins: %v", err)
		}
	}

	created, err := t.Servers.Create(ctx, p, CreateRequest{
		Name:            req.Name,
		NodeID:          req.NodeID,
		Image:           fmt.Sprintf("docker.io/library/eclipse-temurin:%d-jre", java),
		MemoryMB:        memory,
		CPUCores:        cpu,
		Port:            port,
		JarURL:          artifact.JarURL,
		JarSHA256:       artifact.JarSHA256,
		AcceptEULA:      acceptEULA,
		JavaVersion:     java,
		DesiredState:    req.DesiredState,
		Base:            tpl.Base,
		TemplateID:      row.ID,
		TemplateVersion: row.Revision,
		Variables:       inst.Variables,
		Files:           inst.Files,
		MCVersion:       artifact.Version,
		Plugins:         managed,
	})
	if err != nil {
		return store.Server{}, err
	}
	return created, nil
}
