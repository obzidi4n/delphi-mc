package capability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"delphi.dev/delphi/internal/artifact"
	"delphi.dev/delphi/internal/plugins"
	"delphi.dev/delphi/internal/store"
)

type fakeUpdateStore struct {
	srv     store.Server
	updated *store.Server
	audits  []string
}

func (f *fakeUpdateStore) GetServer(context.Context, string) (store.Server, error) {
	return f.srv, nil
}
func (f *fakeUpdateStore) SetArtifacts(_ context.Context, id, mcVersion, jarURL, jarSHA string, list []store.ServerPlugin, desired string) (store.Server, error) {
	f.srv.MCVersion, f.srv.JarURL, f.srv.JarSHA256 = mcVersion, jarURL, jarSHA
	f.srv.Plugins, f.srv.DesiredState = list, desired
	f.srv.DesiredRevision++
	f.updated = &f.srv
	return f.srv, nil
}
func (f *fakeUpdateStore) RecordArtifact(context.Context, store.Artifact) error { return nil }
func (f *fakeUpdateStore) ListArtifacts(context.Context) ([]store.Artifact, error) {
	return nil, nil
}
func (f *fakeUpdateStore) DeleteArtifact(context.Context, string) error { return nil }
func (f *fakeUpdateStore) ArtifactRefs(context.Context) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}
func (f *fakeUpdateStore) Audit(_ context.Context, _, _, _, action, _ string, _ any) error {
	f.audits = append(f.audits, action)
	return nil
}

func newUpdateFixture(t *testing.T, policy, installed string) (*Updates, *fakeUpdateStore, []byte) {
	t.Helper()
	jar := []byte("new-jar-bytes")
	jarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(jar)
	}))
	t.Cleanup(jarSrv.Close)
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `[{"id":"v2","version_number":"2.0.0","game_versions":["1.21.11"],"loaders":["paper"],
		 "files":[{"filename":"p.jar","url":%q,"size":%d,"primary":true}]}]`, jarSrv.URL, len(jar))
	}))
	t.Cleanup(meta.Close)

	resolver := plugins.NewResolver(meta.Client())
	resolver.ModrinthBase = meta.URL
	arts, err := artifact.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeUpdateStore{srv: store.Server{
		ID: "s_1", OrgID: "default", NodeID: "n_1", MCVersion: "1.21.11",
		DesiredState: "stopped", DesiredRevision: 3,
		Plugins: []store.ServerPlugin{{
			Project: "p", Provider: "modrinth", Version: installed, Policy: policy,
			Name: "p.jar", SHA256: strings.Repeat("a", 64),
		}},
	}}
	return &Updates{Store: fake, Resolver: resolver, Artifacts: arts, HTTP: jarSrv.Client()}, fake, jar
}

func TestDetectAndApplyUpdates(t *testing.T) {
	updates, fake, jar := newUpdateFixture(t, "latest", "1.0.0")
	p := Principal{UserID: "u", OrgID: "default", Via: "ui",
		Scopes: []string{"server:s_1:read", "server:s_1:update"}}

	items, _, err := updates.Detect(context.Background(), p, "s_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Latest != "2.0.0" {
		t.Fatalf("detect = %+v", items)
	}

	result, err := updates.Apply(context.Background(), p, "s_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Applied) != 1 || result.BootCheck != "skipped" {
		t.Fatalf("apply = %+v", result)
	}
	if fake.updated == nil || fake.updated.Plugins[0].Version != "2.0.0" {
		t.Fatalf("plugins not updated: %+v", fake.updated)
	}
	want := sha256.Sum256(jar)
	if fake.updated.Plugins[0].SHA256 != hex.EncodeToString(want[:]) {
		t.Fatalf("sha = %s", fake.updated.Plugins[0].SHA256)
	}
	if len(fake.audits) == 0 || fake.audits[0] != "server.update" {
		t.Fatalf("audits = %v", fake.audits)
	}
}

func TestPinnedPluginsAreVisibleButNotAutomatic(t *testing.T) {
	updates, _, _ := newUpdateFixture(t, "pinned", "1.0.0")
	p := Principal{UserID: "u", OrgID: "default", Via: "ui", Scopes: []string{"server:s_1:read"}}
	items, _, err := updates.Detect(context.Background(), p, "s_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Latest != "2.0.0" {
		t.Fatalf("pinned detection: %+v", items)
	}
}

func TestBootCheckRollsBackOnPersistentFailure(t *testing.T) {
	updates, fake, _ := newUpdateFixture(t, "latest", "2.0.0")
	old := fake.srv
	old.Plugins[0].Version = "1.0.0"
	old.Plugins[0].SHA256 = strings.Repeat("b", 64)
	old.DesiredRevision = 1
	old.AppliedRevision = 1
	fake.srv.ObservedState = "exited"
	fake.srv.AppliedRevision = 2
	fake.srv.LastError = "boom"
	updates.BootPoll = 5 * time.Millisecond
	updates.BootTimeout = 2 * time.Second

	updates.bootCheck(old, Principal{UserID: "u", OrgID: "default", Via: "ui"}, 2)

	if fake.updated == nil || fake.updated.Plugins[0].Version != "1.0.0" {
		t.Fatalf("plugins were not restored: %+v", fake.updated)
	}
	found := false
	for _, a := range fake.audits {
		if a == "server.update.rollback" {
			found = true
		}
	}
	if !found {
		t.Fatalf("audits = %v", fake.audits)
	}
}

func TestApplyWithNoUpdates(t *testing.T) {
	updates, fake, _ := newUpdateFixture(t, "pinned", "1.0.0")
	fake.srv.Plugins[0].Version = "2.0.0"
	p := Principal{UserID: "u", OrgID: "default", Via: "ui",
		Scopes: []string{"server:s_1:read", "server:s_1:update"}}
	if _, err := updates.Apply(context.Background(), p, "s_1"); err != ErrNoUpdates {
		t.Fatalf("err = %v", err)
	}
}

func TestDetectSkipsProviderFailureIntoError(t *testing.T) {
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer meta.Close()
	resolver := plugins.NewResolver(meta.Client())
	resolver.ModrinthBase = meta.URL
	fake := &fakeUpdateStore{srv: store.Server{
		ID: "s_1", OrgID: "default", MCVersion: "1.21.11",
		Plugins: []store.ServerPlugin{{Project: "p", Provider: "modrinth", Version: "1", Policy: "latest", Name: "p.jar"}},
	}}
	updates := &Updates{Store: fake, Resolver: resolver}
	p := Principal{UserID: "u", OrgID: "default", Via: "ui", Scopes: []string{"server:s_1:read"}}
	items, _, err := updates.Detect(context.Background(), p, "s_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Error == "" {
		t.Fatalf("items = %+v", items)
	}
}

func TestAddAndRemovePlugin(t *testing.T) {
	updates, fake, _ := newUpdateFixture(t, "latest", "1.0.0")
	p := Principal{UserID: "u", OrgID: "default", Via: "ui", Scopes: []string{"server:s_1:update"}}
	result, err := updates.Add(context.Background(), p, "s_1", PluginRequest{
		Provider: "modrinth", Project: "added",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Plugin.Project != "added" || result.Plugin.Version != "2.0.0" || result.Plugin.Policy != "latest" {
		t.Fatalf("add = %+v", result)
	}
	if len(fake.updated.Plugins) != 2 {
		t.Fatalf("installed = %+v", fake.updated.Plugins)
	}
	if _, err := updates.Remove(context.Background(), p, "s_1", "added"); err != nil {
		t.Fatal(err)
	}
	if len(fake.updated.Plugins) != 1 || fake.updated.Plugins[0].Project != "p" {
		t.Fatalf("removed = %+v", fake.updated.Plugins)
	}
	if _, err := updates.Remove(context.Background(), p, "s_1", "nope"); err == nil {
		t.Fatal("missing plugin should fail")
	}
}

func TestScheduledUpdateAppliesPluginUpdates(t *testing.T) {
	updates, fake, _ := newUpdateFixture(t, "latest", "1.0.0")
	s := &Schedules{Updates: updates}
	p := Principal{
		UserID: "schedule:nightly", OrgID: "default", Via: "schedule",
		Scopes: []string{"server:*:read", "server:*:update"},
	}
	if err := s.run(context.Background(), p, store.Schedule{
		ID: "sch_1", Name: "nightly", OrgID: "default", TargetID: "s_1",
		Action: "updates.apply",
	}); err != nil {
		t.Fatal(err)
	}
	if fake.updated == nil || fake.updated.Plugins[0].Version != "2.0.0" {
		t.Fatalf("plugins not updated: %+v", fake.updated)
	}
}

func TestUploadSpigotPluginIsManualAndNotRemoteChecked(t *testing.T) {
	updates, fake, _ := newUpdateFixture(t, "latest", "1.0.0")
	p := Principal{UserID: "u", OrgID: "default", Via: "ui", Scopes: []string{"server:s_1:read", "server:s_1:update"}}
	result, err := updates.Upload(context.Background(), p, "s_1", "DelphiVote", "1.6",
		bytes.NewReader([]byte("PK\x03\x04jar-bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if result.Plugin.Provider != "spigot" || result.Plugin.Policy != "manual" || result.Plugin.Version != "1.6" {
		t.Fatalf("upload = %+v", result)
	}
	if len(fake.updated.Plugins) != 2 || fake.updated.Plugins[1].Project != "DelphiVote" {
		t.Fatalf("installed = %+v", fake.updated.Plugins)
	}
	items, _, err := updates.Detect(context.Background(), p, "s_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Project == "DelphiVote" {
		t.Fatalf("manual plugin was remote-checked: %+v", items)
	}
	if _, err := updates.Upload(context.Background(), p, "s_1", "not-a-jar", "1",
		bytes.NewReader([]byte("plain text"))); err == nil {
		t.Fatal("non-jar upload should fail")
	}
}
