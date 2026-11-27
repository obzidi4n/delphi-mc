// Package configfmt validates server config files and formats JSON for the
// file browser. YAML and TOML are validated but returned as written so comments
// and ordering survive.
package configfmt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"delphi.dev/delphi/internal/serverprops"
)

// ErrUnsupported means the path is not one of the supported config formats.
var ErrUnsupported = errors.New("unsupported config format")

// Result is a validation result. Formatted is the pretty form for JSON and the
// original content for YAML/TOML.
type Result struct {
	Format    string `json:"format"`
	Valid     bool   `json:"valid"`
	Error     string `json:"error"`
	Warning   string `json:"warning"`
	Formatted string `json:"formatted"`
}

// Check validates path's content by extension and pretty-prints JSON.
func Check(path, content string) (Result, error) {
	return CheckVersion(path, content, "")
}

// CheckVersion validates path's content with an optional Minecraft version for
// server.properties.
func CheckVersion(file, content, version string) (Result, error) {
	if path.Base(file) == "server.properties" {
		res := serverprops.Validate(content, version)
		return Result{
			Format: "properties", Valid: res.Valid,
			Error: strings.Join(res.Errors, "; "), Warning: strings.Join(res.Warnings, "; "),
			Formatted: content,
		}, nil
	}
	switch strings.ToLower(filepath.Ext(file)) {
	case ".json":
		return checkJSON(content), nil
	case ".yaml", ".yml":
		return checkYAML(content), nil
	case ".toml":
		return checkTOML(content), nil
	default:
		return Result{}, ErrUnsupported
	}
}

func checkJSON(content string) Result {
	data := []byte(content)
	if !json.Valid(data) {
		var v any
		err := json.Unmarshal(data, &v)
		return Result{Format: "json", Error: jsonError(data, err)}
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, bytes.TrimSpace(data), "", "  "); err != nil {
		return Result{Format: "json", Error: err.Error()}
	}
	formatted := buf.String()
	if !strings.HasSuffix(formatted, "\n") {
		formatted += "\n"
	}
	return Result{Format: "json", Valid: true, Formatted: formatted}
}

func checkYAML(content string) Result {
	dec := yaml.NewDecoder(strings.NewReader(content))
	for {
		var doc any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Result{Format: "yaml", Error: err.Error()}
		}
	}
	return Result{Format: "yaml", Valid: true, Formatted: content}
}

func checkTOML(content string) Result {
	var doc map[string]any
	if _, err := toml.Decode(content, &doc); err != nil {
		return Result{Format: "toml", Error: err.Error()}
	}
	return Result{Format: "toml", Valid: true, Formatted: content}
}

func jsonError(data []byte, err error) string {
	if err == nil {
		return "invalid JSON"
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		line, column := lineColumn(data, syntax.Offset)
		return fmt.Sprintf("%s (line %d, column %d)", syntax.Error(), line, column)
	}
	return err.Error()
}

func lineColumn(data []byte, offset int64) (line, column int) {
	if offset < 0 {
		offset = 0
	}
	line, column = 1, 1
	for i, b := range data {
		if int64(i) >= offset-1 {
			break
		}
		if b == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return line, column
}
