// Command gen-api generates the DelphiMC Go DTOs and HTTP client from the
// hand-written OpenAPI spec. Run it from the repository root or any directory
// inside it: `go run ./cmd/gen-api`.
//
// It deliberately supports only the subset of OpenAPI this spec uses:
// component schemas of type object/string/integer/number/boolean/array, $ref,
// nullable, path and query parameters, a JSON request body, and one JSON 2xx
// response. Unsupported constructs fail loudly rather than generating guesses.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type schema struct {
	Ref        string            `yaml:"$ref"`
	Type       string            `yaml:"type"`
	Format     string            `yaml:"format"`
	Properties map[string]schema `yaml:"properties"`
	Required   []string          `yaml:"required"`
	Items      *schema           `yaml:"items"`
	Nullable   bool              `yaml:"nullable"`
	Enum       []any             `yaml:"enum"`
	Additional any               `yaml:"additionalProperties"`
}

type param struct {
	Name     string `yaml:"name"`
	In       string `yaml:"in"`
	Required bool   `yaml:"required"`
	Schema   schema `yaml:"schema"`
}

type mediaType struct {
	Schema schema `yaml:"schema"`
}

type requestBody struct {
	Content map[string]mediaType `yaml:"content"`
}

type response struct {
	Content map[string]mediaType `yaml:"content"`
}

type operation struct {
	OperationID string              `yaml:"operationId"`
	Summary     string              `yaml:"summary"`
	Parameters  []param             `yaml:"parameters"`
	RequestBody *requestBody        `yaml:"requestBody"`
	Responses   map[string]response `yaml:"responses"`
}

type spec struct {
	Paths      map[string]map[string]operation `yaml:"paths"`
	Components struct {
		Schemas map[string]schema `yaml:"schemas"`
	} `yaml:"components"`
}

var initialisms = map[string]string{
	"id": "ID", "url": "URL", "api": "API", "cpu": "CPU", "fs": "FS",
	"sha256": "SHA256", "eula": "EULA", "ttl": "TTL", "csr": "CSR",
	"mc": "MC", "json": "JSON", "ip": "IP", "tls": "TLS", "jwt": "JWT",
	"mb": "MB", "kb": "KB", "gb": "GB", "tps": "TPS", "uuid": "UUID",
}

func words(snake string) []string {
	return strings.Split(snake, "_")
}

func goName(snake string) string {
	var b strings.Builder
	for _, w := range words(snake) {
		if up, ok := initialisms[w]; ok {
			b.WriteString(up)
			continue
		}
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		b.WriteString(strings.ToUpper(string(r[0])))
		b.WriteString(string(r[1:]))
	}
	return b.String()
}

func lowerName(snake string) string {
	var b strings.Builder
	for i, w := range words(snake) {
		if i == 0 {
			b.WriteString(strings.ToLower(w))
			continue
		}
		if up, ok := initialisms[w]; ok {
			b.WriteString(up)
			continue
		}
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		b.WriteString(strings.ToUpper(string(r[0])))
		b.WriteString(string(r[1:]))
	}
	return b.String()
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func refName(ref string) (string, error) {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("unsupported $ref %q", ref)
	}
	return strings.TrimPrefix(ref, prefix), nil
}

type gen struct {
	needsTime bool
	needsJSON bool
}

func (g *gen) goType(s schema) (string, error) {
	if s.Ref != "" {
		name, err := refName(s.Ref)
		if err != nil {
			return "", err
		}
		return name, nil
	}
	switch s.Type {
	case "string":
		if s.Format == "date-time" {
			g.needsTime = true
			return "time.Time", nil
		}
		return "string", nil
	case "integer":
		if s.Format == "int64" {
			return "int64", nil
		}
		return "int", nil
	case "number":
		return "float64", nil
	case "boolean":
		return "bool", nil
	case "array":
		if s.Items == nil {
			return "", errors.New("array without items")
		}
		item, err := g.goType(*s.Items)
		if err != nil {
			return "", err
		}
		return "[]" + item, nil
	case "object":
		if len(s.Properties) == 0 {
			if sub, ok, err := s.additionalSchema(); err != nil {
				return "", err
			} else if ok && sub != nil {
				t, err := g.goType(*sub)
				if err != nil {
					return "", err
				}
				return "map[string]" + t, nil
			}
			g.needsJSON = true
			return "json.RawMessage", nil
		}
		return "", fmt.Errorf("inline object schemas are not supported; use a $ref")
	default:
		return "", fmt.Errorf("unsupported schema type %q", s.Type)
	}
}

func isPointer(t string, optional, nullable bool) bool {
	if !optional && !nullable {
		return false
	}
	if strings.HasPrefix(t, "[]") || strings.HasPrefix(t, "map[") || t == "json.RawMessage" {
		return false
	}
	return true
}

// additionalSchema decodes additionalProperties into a schema. A bool (true or
// false) means "any" and yields no typed schema.
func (s schema) additionalSchema() (*schema, bool, error) {
	if s.Additional == nil {
		return nil, false, nil
	}
	if _, isBool := s.Additional.(bool); isBool {
		return nil, false, nil
	}
	data, err := yaml.Marshal(s.Additional)
	if err != nil {
		return nil, false, err
	}
	var sub schema
	if err := yaml.Unmarshal(data, &sub); err != nil {
		return nil, false, err
	}
	if sub.Type == "" && sub.Ref == "" {
		return nil, false, nil
	}
	return &sub, true, nil
}

func main() {
	check := flag.Bool("check", false, "verify the generated files are current")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}
	specPath := filepath.Join(root, "internal", "api", "openapi.yaml")
	data, err := os.ReadFile(specPath)
	if err != nil {
		fatal(err)
	}
	var s spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		fatal(fmt.Errorf("parse %s: %w", specPath, err))
	}

	g := &gen{}
	typesOut, err := g.types(s)
	if err != nil {
		fatal(err)
	}
	if err := writeGo(filepath.Join(root, "internal", "apitypes", "types_gen.go"), typesOut, *check); err != nil {
		fatal(err)
	}
	clientOut, err := g.client(s)
	if err != nil {
		fatal(err)
	}
	if err := writeGo(filepath.Join(root, "internal", "apiclient", "client_gen.go"), clientOut, *check); err != nil {
		fatal(err)
	}
	if !*check {
		fmt.Println("generated internal/apitypes/types_gen.go")
		fmt.Println("generated internal/apiclient/client_gen.go")
	}
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

func writeGo(path string, src []byte, check bool) error {
	formatted, err := format.Source(src)
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", path, err, src)
	}
	if check {
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, formatted) {
			return fmt.Errorf("%s is stale; run `make generate`", path)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, formatted, 0o644)
}

func header() string {
	return `// Code generated by cmd/gen-api from internal/api/openapi.yaml. DO NOT EDIT.

`
}

func (g *gen) types(s spec) ([]byte, error) {
	names := make([]string, 0, len(s.Components.Schemas))
	for name := range s.Components.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	var body bytes.Buffer
	for _, name := range names {
		sc := s.Components.Schemas[name]
		if sc.Type != "object" || len(sc.Properties) == 0 {
			return nil, fmt.Errorf("schema %s: only object schemas with properties are supported", name)
		}
		required := map[string]bool{}
		for _, r := range sc.Required {
			required[r] = true
		}
		props := make([]string, 0, len(sc.Properties))
		for p := range sc.Properties {
			props = append(props, p)
		}
		sort.Strings(props)

		fmt.Fprintf(&body, "// %s is generated from the spec.\n", name)
		fmt.Fprintf(&body, "type %s struct {\n", name)
		for _, p := range props {
			ps := sc.Properties[p]
			t, err := g.goType(ps)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", name, p, err)
			}
			isRequired := required[p]
			if isPointer(t, !isRequired, ps.Nullable) {
				t = "*" + t
			}
			tag := p
			if !isRequired {
				tag += ",omitempty"
			}
			fmt.Fprintf(&body, "\t%s %s `json:\"%s\"`\n", goName(p), t, tag)
		}
		body.WriteString("}\n\n")
	}

	var imports []string
	if g.needsJSON {
		imports = append(imports, `"encoding/json"`)
	}
	if g.needsTime {
		imports = append(imports, `"time"`)
	}

	var out bytes.Buffer
	out.WriteString(header())
	out.WriteString("package apitypes\n\n")
	if len(imports) > 0 {
		out.WriteString("import (\n")
		for _, imp := range imports {
			fmt.Fprintf(&out, "\t%s\n", imp)
		}
		out.WriteString(")\n\n")
	}
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

type opInfo struct {
	Method      string
	Path        string
	ID          string
	PathParams  []param
	QueryParams []param
	BodyType    string
	RespType    string
	NoContent   bool
}

func (g *gen) client(s spec) ([]byte, error) {
	ops, err := g.operations(s)
	if err != nil {
		return nil, err
	}
	needStrconv := false
	for _, op := range ops {
		for _, p := range op.QueryParams {
			if p.Schema.Type == "integer" && p.Schema.Format == "int64" {
				needStrconv = true
			}
		}
	}

	var body bytes.Buffer
	for _, op := range ops {
		fmt.Fprintf(&body, "// %s %s\n", op.Method, op.Path)
		if op.ID != "enrollNode" {
			body.WriteString("//\n// Requires a bearer token configured on the Client.\n")
		}
		body.WriteString("func (c *Client) " + upperFirst(op.ID) + "(ctx context.Context")
		for _, p := range op.PathParams {
			t, err := g.goType(p.Schema)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&body, ", %s %s", lowerName(p.Name), t)
		}
		for _, p := range op.QueryParams {
			t, err := g.goType(p.Schema)
			if err != nil {
				return nil, err
			}
			if !p.Required {
				t = "*" + t
			}
			fmt.Fprintf(&body, ", %s %s", lowerName(p.Name), t)
		}
		if op.BodyType != "" {
			fmt.Fprintf(&body, ", body apitypes.%s", op.BodyType)
		}
		if op.RespType != "" {
			fmt.Fprintf(&body, ") (*apitypes.%s, error) {\n", op.RespType)
		} else {
			body.WriteString(") error {\n")
		}

		// Path.
		var parts []string
		for _, seg := range strings.Split(op.Path, "/") {
			if seg == "" {
				continue
			}
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				name := strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
				parts = append(parts, `"/" + url.PathEscape(`+lowerName(name)+`)`)
				continue
			}
			parts = append(parts, fmt.Sprintf("%q", "/"+seg))
		}
		body.WriteString("\tpath := " + strings.Join(parts, " + ") + "\n")

		// Query.
		if len(op.QueryParams) > 0 {
			body.WriteString("\tq := url.Values{}\n")
			for _, p := range op.QueryParams {
				n := lowerName(p.Name)
				switch {
				case p.Schema.Type == "string" && p.Required:
					fmt.Fprintf(&body, "\tq.Set(%q, %s)\n", p.Name, n)
				case p.Schema.Type == "string":
					fmt.Fprintf(&body, "\tif %s != nil {\n\t\tq.Set(%q, *%s)\n\t}\n", n, p.Name, n)
				case p.Schema.Type == "integer" && p.Required:
					fmt.Fprintf(&body, "\tq.Set(%q, strconv.FormatInt(int64(%s), 10))\n", p.Name, n)
				case p.Schema.Type == "integer":
					fmt.Fprintf(&body, "\tif %s != nil {\n\t\tq.Set(%q, strconv.FormatInt(int64(*%s), 10))\n\t}\n", n, p.Name, n)
				default:
					return nil, fmt.Errorf("%s: unsupported query parameter type %q", op.ID, p.Schema.Type)
				}
			}
			body.WriteString("\n")
		} else {
			body.WriteString("\tvar q url.Values\n\n")
		}

		// Call.
		bodyArgs := "nil"
		if op.BodyType != "" {
			bodyArgs = "body"
		}
		if op.RespType != "" {
			fmt.Fprintf(&body, "\tvar out apitypes.%s\n", op.RespType)
			fmt.Fprintf(&body, "\tif err := c.do(ctx, %q, path, q, %s, &out); err != nil {\n\t\treturn nil, err\n\t}\n", op.Method, bodyArgs)
			body.WriteString("\treturn &out, nil\n")
		} else {
			fmt.Fprintf(&body, "\treturn c.do(ctx, %q, path, q, %s, nil)\n", op.Method, bodyArgs)
		}
		body.WriteString("}\n\n")
	}

	var out bytes.Buffer
	out.WriteString(header())
	out.WriteString(`package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
`)
	if needStrconv {
		out.WriteString("\t\"strconv\"\n")
	}
	out.WriteString(`
	"delphi.dev/delphi/internal/apitypes"
)

// Client talks to one panel. BaseURL is the panel root (for example
// https://127.0.0.1:8443); API paths are absolute.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

// New returns a client. A nil HTTPClient uses http.DefaultClient.
func New(baseURL string, hc *http.Client, token string) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{BaseURL: baseURL, HTTPClient: hc, Token: token}
}

// APIError is a non-2xx response carrying the problem details.
type APIError struct {
	Status  int
	Problem apitypes.Problem
}

func (e *APIError) Error() string {
	if e.Problem.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Problem.Title, e.Problem.Detail)
	}
	return fmt.Sprintf("%s (status %d)", e.Problem.Title, e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	target := strings.TrimRight(c.BaseURL, "/") + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var problem apitypes.Problem
		if json.Unmarshal(data, &problem) != nil || problem.Title == "" {
			problem = apitypes.Problem{Title: http.StatusText(resp.StatusCode), Detail: string(data)}
		}
		return &APIError{Status: resp.StatusCode, Problem: problem}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

`)
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

func (g *gen) operations(s spec) ([]opInfo, error) {
	methods := map[string]bool{"get": true, "post": true, "patch": true, "delete": true, "put": true}
	var paths []string
	for p := range s.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var ops []opInfo
	for _, path := range paths {
		var names []string
		for method := range s.Paths[path] {
			names = append(names, method)
		}
		sort.Strings(names)
		for _, method := range names {
			op := s.Paths[path][method]
			if !methods[method] {
				continue
			}
			if op.OperationID == "" {
				return nil, fmt.Errorf("%s %s: missing operationId", method, path)
			}
			info := opInfo{Method: strings.ToUpper(method), Path: path, ID: op.OperationID}
			for _, p := range op.Parameters {
				switch p.In {
				case "path":
					info.PathParams = append(info.PathParams, p)
				case "query":
					info.QueryParams = append(info.QueryParams, p)
				default:
					return nil, fmt.Errorf("%s: unsupported parameter location %q", op.OperationID, p.In)
				}
			}
			if op.RequestBody != nil {
				media, ok := op.RequestBody.Content["application/json"]
				if !ok {
					return nil, fmt.Errorf("%s: request body is not application/json", op.OperationID)
				}
				name, err := refName(media.Schema.Ref)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", op.OperationID, err)
				}
				info.BodyType = name
			}
			var codes []string
			for code := range op.Responses {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			for _, code := range codes {
				if !strings.HasPrefix(code, "2") {
					continue
				}
				resp := op.Responses[code]
				media, ok := resp.Content["application/json"]
				if !ok {
					info.NoContent = true
					continue
				}
				name, err := refName(media.Schema.Ref)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", op.OperationID, code, err)
				}
				info.RespType = name
				break
			}
			ops = append(ops, info)
		}
	}
	return ops, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gen-api:", err)
	os.Exit(1)
}
