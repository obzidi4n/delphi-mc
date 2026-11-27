package configfmt

import (
	"errors"
	"strings"
	"testing"
)

func TestJSONFormats(t *testing.T) {
	if _, err := Check("config.ini", ""); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	res, err := Check("config.json", `{"a":1,"nested":{"b":true}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("valid JSON rejected: %s", res.Error)
	}
	want := "{\n  \"a\": 1,\n  \"nested\": {\n    \"b\": true\n  }\n}\n"
	if res.Formatted != want {
		t.Fatalf("formatted = %q, want %q", res.Formatted, want)
	}
}

func TestJSONReportsLineAndColumn(t *testing.T) {
	res, err := Check("config.json", "{\n  \"a\": 1,\n  \"b\":\n}")
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid || !strings.Contains(res.Error, "line 4") {
		t.Fatalf("result = %+v, want a line 4 error", res)
	}
}

func TestServerPropertiesValidAndInvalid(t *testing.T) {
	res, err := CheckVersion("server.properties", "online-mode=true\nview-distance=10\n", "1.21.11")
	if err != nil || !res.Valid || res.Format != "properties" {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
	res, err = CheckVersion("server.properties", "online-mode=perhaps\n", "1.21.11")
	if err != nil || res.Valid || res.Error == "" {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
}

func TestYAMLValidAndInvalid(t *testing.T) {
	res, err := Check("server.yml", "motd: hello\nplayers: 20\n")
	if err != nil || !res.Valid || res.Format != "yaml" {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
	res, err = Check("server.yaml", "motd: [unterminated\n")
	if err != nil || res.Valid || res.Error == "" {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
}

func TestTOMLValidAndInvalid(t *testing.T) {
	res, err := Check("server.toml", "motd = \"hello\"\nplayers = 20\n")
	if err != nil || !res.Valid || res.Format != "toml" {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
	res, err = Check("server.toml", "motd = [unterminated\n")
	if err != nil || res.Valid || res.Error == "" {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
}
