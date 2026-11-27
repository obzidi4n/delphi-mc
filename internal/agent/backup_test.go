package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTarGzExcludesTransientData(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(name, content string) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("world/level.dat", "level")
	mustWrite("server.properties", "motd=x")
	mustWrite("logs/latest.log", "noise")
	mustWrite("world/session.lock", "lock")
	mustWrite(".delphi-state.json", "{}")

	var archive bytes.Buffer
	if err := writeTarGz(&archive, dir); err != nil {
		t.Fatal(err)
	}
	names := tarNames(t, archive.Bytes())
	for _, want := range []string{"world/level.dat", "server.properties"} {
		if !names[want] {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	for _, unwanted := range []string{"logs/latest.log", "world/session.lock", ".delphi-state.json"} {
		if names[unwanted] {
			t.Fatalf("included transient %s", unwanted)
		}
	}

	restore := t.TempDir()
	if err := extractTarGz(bytes.NewReader(archive.Bytes()), restore); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(restore, "world", "level.dat"))
	if err != nil || string(got) != "level" {
		t.Fatalf("restored level = %q err=%v", got, err)
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()

	root := t.TempDir()
	if err := extractTarGz(bytes.NewReader(buf.Bytes()), root); err == nil {
		t.Fatal("traversal archive accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "..", "escape")); !os.IsNotExist(err) {
		t.Fatal("traversal wrote outside the server directory")
	}
}

func tarNames(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names[header.Name] = true
	}
}

func TestExcludedSessionLockAnywhere(t *testing.T) {
	if !excluded("world/session.lock", nil) {
		t.Fatal("session.lock must be excluded")
	}
}
