package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"delphi.dev/delphi/internal/wire"
)

func TestEnsurePluginsInstallsAndReplaces(t *testing.T) {
	body := "plugin-one"
	sum := sha256.Sum256([]byte(body))
	sha := hex.EncodeToString(sum[:])
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/blobs/"+sha {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		hits.Add(1)
		io.WriteString(w, body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	sup := &Supervisor{
		DataDir: dir, HTTP: srv.Client(), BlobHTTP: srv.Client(), BlobBase: srv.URL,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	spec := wire.ServerSpec{ID: "s_1", Plugins: []wire.PluginSpec{{Name: "luckperms.jar", SHA256: sha}}}
	if err := sup.ensurePlugins(context.Background(), spec, dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "plugins", "luckperms.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("plugin content = %q", got)
	}
	if err := sup.ensurePlugins(context.Background(), spec, dir); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("reinstall should not download again, hits=%d", hits.Load())
	}

	spec.Plugins[0].Name = "../escape.jar"
	if err := sup.ensurePlugins(context.Background(), spec, dir); err == nil {
		t.Fatal("unsafe plugin name must fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.jar")); !os.IsNotExist(err) {
		t.Fatal("escaped plugin file must not exist")
	}
}

func TestEnsurePluginsRemovesStaleManagedJar(t *testing.T) {
	body := "plugin-one"
	sum := sha256.Sum256([]byte(body))
	sha := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	dir := t.TempDir()
	sup := &Supervisor{
		DataDir: dir, HTTP: srv.Client(), BlobHTTP: srv.Client(), BlobBase: srv.URL,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	spec := wire.ServerSpec{ID: "s_1", Plugins: []wire.PluginSpec{{Name: "luckperms.jar", SHA256: sha}}}
	if err := sup.ensurePlugins(context.Background(), spec, dir); err != nil {
		t.Fatal(err)
	}
	empty := wire.ServerSpec{ID: "s_1"}
	if err := sup.ensurePlugins(context.Background(), empty, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "luckperms.jar")); !os.IsNotExist(err) {
		t.Fatal("managed plugin should be removed when it leaves the desired set")
	}
}

func TestEnsurePluginsUsesPreviousNamesWithoutManifest(t *testing.T) {
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(plugins, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(plugins, "old.jar")
	if err := os.WriteFile(target, []byte("PK\x03\x04old"), 0o644); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{DataDir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	spec := wire.ServerSpec{ID: "s_1", PreviousPlugins: []string{"old.jar"}}
	if err := sup.ensurePlugins(context.Background(), spec, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("previous managed plugin was not removed")
	}
}

func TestHandleDevCloneCopiesAndRewritesSettings(t *testing.T) {
	dir := t.TempDir()
	sup := &Supervisor{DataDir: dir, Runtime: newFakeRuntime(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	source := filepath.Join(dir, "servers", "s_prod")
	if err := os.MkdirAll(filepath.Join(source, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "server.properties"), []byte("server-port=25565\nmotd=Production\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "plugins", "kept.jar"), []byte("PK\x03\x04kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := HandleDevClone(context.Background(), sup, nil, wire.DevCloneRequest{
		RequestID: "req_1", SourceServerID: "s_prod", TargetServerID: "s_dev", Port: 25999,
		Whitelist: []wire.WhitelistEntry{{Name: "owner", UUID: "11111111-1111-1111-1111-111111111111"}},
	})
	if !res.OK {
		t.Fatal(res.Error)
	}
	target := filepath.Join(dir, "servers", "s_dev")
	props, err := os.ReadFile(filepath.Join(target, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"server-port=25999", "white-list=true", "enforce-whitelist=true", "motd=Production"} {
		if !strings.Contains(string(props), want) {
			t.Fatalf("properties missing %q: %s", want, props)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "plugins", "kept.jar")); err != nil {
		t.Fatalf("plugin not copied: %v", err)
	}
	whitelist, err := os.ReadFile(filepath.Join(target, "whitelist.json"))
	if err != nil || !strings.Contains(string(whitelist), `"name":"owner"`) {
		t.Fatalf("whitelist = %s (%v)", whitelist, err)
	}
}
