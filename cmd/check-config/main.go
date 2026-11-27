// Command check-config lints and validates repository YAML, TOML, and JSON.
//
// It checks every non-ignored file with one of those extensions, including
// untracked files, so a new config is linted before its first commit. Known
// files get their real loader or schema checks; the rest must at least parse.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"delphi.dev/delphi/internal/config"
	"delphi.dev/delphi/internal/template"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	files, err := repoFiles(root)
	if err != nil {
		fatal(err)
	}

	var checked int
	var failed int
	for _, rel := range files {
		path := filepath.Join(root, rel)
		var err error
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".toml":
			err = checkTOML(rel, path)
		case ".yaml", ".yml":
			err = checkYAML(rel, path)
		case ".json":
			err = checkJSON(rel, path)
		default:
			continue
		}
		checked++
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", rel, err)
			failed++
		}
	}
	if failed > 0 {
		fatal(fmt.Errorf("%d of %d config files failed", failed, checked))
	}
	fmt.Printf("config check: %d files ok\n", checked)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "check-config:", err)
	os.Exit(1)
}

// repoFiles returns non-ignored files when git is available, otherwise a
// bounded walk. The fallback skips generated and local-only directories.
func repoFiles(root string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err == nil {
		var files []string
		for _, raw := range bytes.Split(out, []byte{0}) {
			if len(raw) == 0 {
				continue
			}
			files = append(files, filepath.ToSlash(string(raw)))
		}
		sort.Strings(files)
		return files, nil
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".delphi", "bin", "node_modules", "build":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func checkTOML(rel, path string) error {
	var target any
	switch rel {
	case "panel.example.toml":
		target = &config.Panel{}
	case "agent.example.toml":
		target = &config.Agent{}
	default:
		var raw map[string]any
		if _, err := toml.DecodeFile(path, &raw); err != nil {
			return fmt.Errorf("parse TOML: %w", err)
		}
		return nil
	}

	md, err := toml.DecodeFile(path, target)
	if err != nil {
		return fmt.Errorf("parse TOML: %w", err)
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		keys := make([]string, len(unknown))
		for i, key := range unknown {
			keys[i] = key.String()
		}
		sort.Strings(keys)
		return fmt.Errorf("unknown keys: %s", strings.Join(keys, ", "))
	}
	switch rel {
	case "panel.example.toml":
		if _, err := config.LoadPanel(path); err != nil {
			return err
		}
	case "agent.example.toml":
		if _, err := config.LoadAgent(path); err != nil {
			return err
		}
	}
	return nil
}

func checkYAML(rel, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []any
	for {
		var doc any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("parse YAML: %w", err)
		}
		docs = append(docs, doc)
	}
	if len(docs) == 0 {
		return errors.New("empty YAML document")
	}

	switch rel {
	case "internal/api/openapi.yaml":
		if len(docs) != 1 {
			return fmt.Errorf("want one YAML document, got %d", len(docs))
		}
		return checkOpenAPI(docs[0])
	case "plugin/bridge/plugin.yml":
		if len(docs) != 1 {
			return fmt.Errorf("want one YAML document, got %d", len(docs))
		}
		return checkPlugin(docs[0])
	}
	return nil
}

func checkOpenAPI(doc any) error {
	root, ok := doc.(map[string]any)
	if !ok {
		return errors.New("OpenAPI document must be a mapping")
	}
	version, ok := stringField(root, "openapi")
	if !ok || !strings.HasPrefix(version, "3.") {
		return errors.New("openapi must be a 3.x version string")
	}
	info, ok := root["info"].(map[string]any)
	if !ok {
		return errors.New("info must be a mapping")
	}
	if title, _ := stringField(info, "title"); title == "" {
		return errors.New("info.title is required")
	}
	if ver, _ := stringField(info, "version"); ver == "" {
		return errors.New("info.version is required")
	}
	paths, ok := root["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		return errors.New("paths must be a non-empty mapping")
	}
	for path := range paths {
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("path %q must start with /", path)
		}
	}
	return nil
}

func checkPlugin(doc any) error {
	root, ok := doc.(map[string]any)
	if !ok {
		return errors.New("plugin manifest must be a mapping")
	}
	for _, key := range []string{"name", "version", "main", "api-version"} {
		if value, _ := stringField(root, key); value == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	return nil
}

func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func checkJSON(rel, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return fmt.Errorf("trailing data: %w", err)
	}
	if strings.HasPrefix(rel, "internal/template/builtin/") {
		if _, err := template.Parse(data); err != nil {
			return fmt.Errorf("template: %w", err)
		}
		var strict template.Template
		yamlDec := yaml.NewDecoder(bytes.NewReader(data))
		yamlDec.KnownFields(true)
		if err := yamlDec.Decode(&strict); err != nil {
			return fmt.Errorf("template fields: %w", err)
		}
	}
	return nil
}
