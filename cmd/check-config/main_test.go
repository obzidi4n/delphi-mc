package main

import (
	"os"
	"path/filepath"
	"testing"
)

func tempFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckJSONRejectsMalformed(t *testing.T) {
	if err := checkJSON("x.json", tempFile(t, "x.json", `{"a":`)); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}

func TestCheckJSONRejectsUnknownTemplateField(t *testing.T) {
	body := `{
		"schema_version": 1,
		"name": "x",
		"base": "paper",
		"mc_version": "latest",
		"java": 21,
		"memroy_mb": 1024
	}`
	if err := checkJSON("internal/template/builtin/test.json", tempFile(t, "test.json", body)); err == nil {
		t.Fatal("unknown template field accepted")
	}
}

func TestCheckTOMLRejectsUnknownPanelKey(t *testing.T) {
	body := "listen = \"127.0.0.1:9999\"\ndata_dir = \"/tmp\"\nunknown = true\n"
	if err := checkTOML("panel.example.toml", tempFile(t, "panel.example.toml", body)); err == nil {
		t.Fatal("unknown panel key accepted")
	}
}

func TestCheckYAMLRejectsIncompletePlugin(t *testing.T) {
	body := "name: DelphiBridge\nversion: 0.1.0\n"
	if err := checkYAML("plugin/bridge/plugin.yml", tempFile(t, "plugin.yml", body)); err == nil {
		t.Fatal("plugin manifest without main/api-version accepted")
	}
}

func TestCheckYAMLRejectsMalformed(t *testing.T) {
	if err := checkYAML("x.yaml", tempFile(t, "x.yaml", "a: [\n")); err == nil {
		t.Fatal("malformed YAML accepted")
	}
}

func TestCheckJSONAcceptsValidTemplate(t *testing.T) {
	body := `{"schema_version":1,"name":"x","base":"paper","mc_version":"latest","java":21}`
	if err := checkJSON("internal/template/builtin/test.json", tempFile(t, "test.json", body)); err != nil {
		t.Fatal(err)
	}
}
