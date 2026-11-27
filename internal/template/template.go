// Package template defines the server template schema, renders its files, and
// ships the built-in templates. Templates are plain data; the capability layer
// resolves versions and instantiates them.
package template

import (
	"embed"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the only template schema this build understands.
const SchemaVersion = 1

// Limits keep a template from becoming an unbounded upload.
const (
	MaxFileBytes  = 64 << 10
	MaxTotalBytes = 256 << 10
	MaxFiles      = 64
	MaxPlugins    = 32
)

type Template struct {
	SchemaVersion int                 `json:"schema_version" yaml:"schema_version"`
	ID            string              `json:"id,omitempty" yaml:"id,omitempty"`
	Name          string              `json:"name" yaml:"name"`
	Description   string              `json:"description,omitempty" yaml:"description,omitempty"`
	Base          string              `json:"base" yaml:"base"`
	MCVersion     string              `json:"mc_version" yaml:"mc_version"`
	Java          int                 `json:"java" yaml:"java"`
	Defaults      Defaults            `json:"defaults" yaml:"defaults"`
	Files         map[string]string   `json:"files,omitempty" yaml:"files,omitempty"`
	Plugins       []Plugin            `json:"plugins,omitempty" yaml:"plugins,omitempty"`
	Variables     map[string]Variable `json:"variables,omitempty" yaml:"variables,omitempty"`
}

type Defaults struct {
	MemoryMB int     `json:"memory_mb" yaml:"memory_mb"`
	CPUCores float64 `json:"cpu_cores" yaml:"cpu_cores"`
	Port     int     `json:"port" yaml:"port"`
}

type Plugin struct {
	Project  string `json:"project" yaml:"project"`
	Provider string `json:"provider" yaml:"provider"`
	Version  string `json:"version,omitempty" yaml:"version,omitempty"`
	Range    string `json:"range,omitempty" yaml:"range,omitempty"`
	Required bool   `json:"required,omitempty" yaml:"required,omitempty"`
}

type Variable struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Default     string `json:"default,omitempty" yaml:"default,omitempty"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
}

// Input is what a caller knows when rendering a template.
type Input struct {
	Name      string
	Port      int
	MemoryMB  int
	CPUCores  float64
	Java      int
	MCVersion string
	Overrides map[string]string // custom variables only
}

// Instance is the rendered result persisted with the server.
type Instance struct {
	Files     map[string]string
	Variables map[string]string
}

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}$`)
	versionFixed  = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)
	varPattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	refPattern    = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Parse decodes JSON or YAML and validates the result.
func Parse(data []byte) (Template, error) {
	var t Template
	if err := yaml.Unmarshal(data, &t); err != nil {
		return Template{}, fmt.Errorf("parse template: %w", err)
	}
	if err := t.Validate(); err != nil {
		return Template{}, err
	}
	return t, nil
}

// Validate reports schema problems; the message names the offending field.
func (t Template) Validate() error {
	if t.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %d", SchemaVersion)
	}
	if strings.TrimSpace(t.Name) == "" || len(t.Name) > 64 {
		return errors.New("name must be 1-64 characters")
	}
	if t.ID != "" && !idPattern.MatchString(t.ID) {
		return errors.New("id must be a lowercase slug (3-64 characters)")
	}
	switch t.Base {
	case "paper", "velocity":
	case "purpur":
		return errors.New("base purpur is not resolvable yet")
	default:
		return errors.New("base must be paper or velocity")
	}
	if t.MCVersion != "latest" && t.MCVersion != "recommended" && !versionFixed.MatchString(t.MCVersion) {
		return errors.New("mc_version must be latest, recommended, or a version like 1.21.4")
	}
	switch t.Java {
	case 8, 17, 21, 25:
	default:
		return errors.New("java must be one of 8, 17, 21, 25")
	}
	if t.Defaults.MemoryMB != 0 && (t.Defaults.MemoryMB < 512 || t.Defaults.MemoryMB > 32768) {
		return errors.New("defaults.memory_mb must be between 512 and 32768")
	}
	if t.Defaults.CPUCores < 0 || t.Defaults.CPUCores > 16 {
		return errors.New("defaults.cpu_cores must be between 0 and 16")
	}
	if t.Defaults.Port != 0 && (t.Defaults.Port < 1024 || t.Defaults.Port > 65535) {
		return errors.New("defaults.port must be 0 (allocate) or between 1024 and 65535")
	}
	if len(t.Files) > MaxFiles {
		return fmt.Errorf("at most %d files", MaxFiles)
	}
	total := 0
	for path, body := range t.Files {
		if !safeRelPath(path) {
			return fmt.Errorf("file path %q must be relative and may not contain ..", path)
		}
		if path == "eula.txt" || path == "paper.jar" {
			return fmt.Errorf("file %q is managed by the agent", path)
		}
		if len(body) > MaxFileBytes {
			return fmt.Errorf("file %q exceeds %d bytes", path, MaxFileBytes)
		}
		total += len(body)
	}
	if total > MaxTotalBytes {
		return fmt.Errorf("template files exceed %d bytes", MaxTotalBytes)
	}
	if len(t.Plugins) > MaxPlugins {
		return fmt.Errorf("at most %d plugins", MaxPlugins)
	}
	for i, p := range t.Plugins {
		if strings.TrimSpace(p.Project) == "" {
			return fmt.Errorf("plugins[%d].project is required", i)
		}
		switch p.Provider {
		case "modrinth", "hangar", "github":
		default:
			return fmt.Errorf("plugins[%d].provider must be modrinth, hangar, or github", i)
		}
		if p.Version != "" && p.Range != "" {
			return fmt.Errorf("plugins[%d]: set version or range, not both", i)
		}
		if p.Version != "" && !sha256Pattern.MatchString(p.Version) && strings.ContainsAny(p.Version, " \t\r\n") {
			return fmt.Errorf("plugins[%d].version is not a valid pin", i)
		}
	}
	for name, v := range t.Variables {
		if !varPattern.MatchString(name) {
			return fmt.Errorf("variable %q must match %s", name, varPattern)
		}
		if strings.HasPrefix(name, "server") || strings.HasPrefix(name, "template") {
			return fmt.Errorf("variable %q is reserved", name)
		}
		if v.Required && v.Default != "" {
			return fmt.Errorf("variable %q is required and has a default", name)
		}
	}
	return nil
}

// Render substitutes variables into files. Unknown references and overrides
// are errors: a typo must not silently ship a broken config.
func (t Template) Render(in Input) (Instance, error) {
	if in.Name == "" || in.Port < 1 {
		return Instance{}, errors.New("render requires a name and a concrete port")
	}
	vars := map[string]string{
		"server.name":         oneLine(in.Name),
		"server.port":         strconv.Itoa(in.Port),
		"server.memory_mb":    strconv.Itoa(in.MemoryMB),
		"server.cpu_cores":    strconv.FormatFloat(in.CPUCores, 'f', -1, 64),
		"server.java":         strconv.Itoa(in.Java),
		"template.mc_version": in.MCVersion,
	}
	for name, v := range t.Variables {
		value := v.Default
		if override, ok := in.Overrides[name]; ok {
			value = override
		}
		if value == "" {
			if v.Required {
				return Instance{}, fmt.Errorf("variable %q is required", name)
			}
			value = ""
		}
		vars[name] = value
	}
	for name := range in.Overrides {
		if _, declared := t.Variables[name]; !declared {
			return Instance{}, fmt.Errorf("unknown variable override %q", name)
		}
	}

	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)

	files := make(map[string]string, len(t.Files))
	for path, body := range t.Files {
		rendered, err := expand(body, vars)
		if err != nil {
			return Instance{}, fmt.Errorf("%s: %w", path, err)
		}
		files[path] = rendered
	}
	return Instance{Files: files, Variables: vars}, nil
}

func expand(body string, vars map[string]string) (string, error) {
	var firstErr error
	out := refPattern.ReplaceAllStringFunc(body, func(ref string) string {
		name := refPattern.FindStringSubmatch(ref)[1]
		value, ok := vars[name]
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("unknown variable %q", name)
			}
			return ref
		}
		return value
	})
	if firstErr != nil {
		return "", firstErr
	}
	if len(out) > MaxTotalBytes {
		return "", errors.New("rendered file is too large")
	}
	return out, nil
}

func safeRelPath(path string) bool {
	if path == "" || len(path) > 255 || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// ---- built-ins ----

//go:embed builtin/*.json
var builtinFS embed.FS

// Builtins returns the embedded templates, validated, sorted by name.
func Builtins() ([]Template, error) {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil, err
	}
	var out []Template
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := builtinFS.ReadFile("builtin/" + entry.Name())
		if err != nil {
			return nil, err
		}
		t, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
