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
	"testing"

	"delphi.dev/delphi/internal/wire"
)

type fakeRuntime struct {
	follow   io.ReadCloser
	stats    []ContainerStat
	state    map[string]string
	revision map[string]int64
	ensures  int
	starts   int
	stops    int
	kills    int
	restarts int
	logs     string
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{state: map[string]string{}, revision: map[string]int64{}, logs: "container output"}
}

func (f *fakeRuntime) Name() string { return "fake" }

func (f *fakeRuntime) Ensure(_ context.Context, spec wire.ServerSpec, _ string) error {
	f.ensures++
	f.state[spec.ID] = "running"
	f.revision[spec.ID] = spec.Revision
	return nil
}

func (f *fakeRuntime) Start(_ context.Context, id string) error {
	f.starts++
	f.state[id] = "running"
	return nil
}

func (f *fakeRuntime) Stop(_ context.Context, id string) error {
	f.stops++
	f.state[id] = "exited"
	return nil
}

func (f *fakeRuntime) Kill(_ context.Context, id string) error {
	f.kills++
	f.state[id] = "exited"
	return nil
}

func (f *fakeRuntime) Restart(_ context.Context, id string) error {
	f.restarts++
	f.state[id] = "running"
	return nil
}

func (f *fakeRuntime) State(_ context.Context, id string) (string, error) {
	if state, ok := f.state[id]; ok {
		return state, nil
	}
	return "missing", nil
}

func (f *fakeRuntime) AppliedRevision(_ context.Context, id string) (int64, error) {
	return f.revision[id], nil
}

func (f *fakeRuntime) Remove(_ context.Context, id string) error {
	delete(f.state, id)
	delete(f.revision, id)
	return nil
}

func (f *fakeRuntime) Logs(context.Context, string, int) (string, error) { return f.logs, nil }

func (f *fakeRuntime) FollowLogs(context.Context, string) (io.ReadCloser, error) {
	if f.follow == nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return f.follow, nil
}

func (f *fakeRuntime) Stats(context.Context) ([]ContainerStat, error) { return f.stats, nil }

func jarServer(t *testing.T, payload []byte) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(payload)
	return srv, hex.EncodeToString(sum[:])
}

func newTestSupervisor(t *testing.T, rt Runtime, payload []byte) (*Supervisor, wire.ServerSpec, string) {
	t.Helper()
	srv, sum := jarServer(t, payload)
	dir := t.TempDir()
	sup := NewSupervisor(dir, rt, slog.New(slog.NewTextHandler(io.Discard, nil)))
	spec := wire.ServerSpec{
		ID:           "s_1",
		Name:         "survival-1",
		Revision:     1,
		DesiredState: "running",
		AcceptEULA:   true,
		Image:        "delphi/java:21-temurin",
		JavaVersion:  21,
		MemoryMB:     1024,
		CPUCores:     1,
		Port:         25565,
		JarURL:       srv.URL + "/paper.jar",
		JarSHA256:    sum,
	}
	return sup, spec, filepath.Join(dir, "servers", "s_1")
}

func TestApplyStartsAndPreparesFiles(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, dir := newTestSupervisor(t, rt, []byte("jar-bytes"))

	state, detail := sup.Apply(context.Background(), spec)
	if state != "running" || detail != "" {
		t.Fatalf("state=%q detail=%q", state, detail)
	}
	if rt.ensures != 1 {
		t.Fatalf("ensures = %d, want 1", rt.ensures)
	}
	for _, name := range []string{"eula.txt", "server.properties", "paper.jar"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	eula, _ := os.ReadFile(filepath.Join(dir, "eula.txt"))
	if string(eula) != "eula=true\n" {
		t.Fatalf("eula = %q", eula)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, _ := newTestSupervisor(t, rt, []byte("jar-bytes"))

	if state, _ := sup.Apply(context.Background(), spec); state != "running" {
		t.Fatalf("first apply: %q", state)
	}
	state, _ := sup.Apply(context.Background(), spec)
	if state != "running" {
		t.Fatalf("second apply: %q", state)
	}
	if rt.ensures != 1 || rt.restarts != 0 || rt.starts != 0 {
		t.Fatalf("re-applied unchanged revision: ensures=%d restarts=%d starts=%d", rt.ensures, rt.restarts, rt.starts)
	}
}

func TestApplyRestartsOnNewRevision(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, _ := newTestSupervisor(t, rt, []byte("jar-bytes"))

	_, _ = sup.Apply(context.Background(), spec)
	spec.Revision = 2
	state, _ := sup.Apply(context.Background(), spec)
	if state != "running" {
		t.Fatalf("state = %q", state)
	}
	if rt.restarts != 1 {
		t.Fatalf("restarts = %d, want 1", rt.restarts)
	}
	if rt.ensures != 1 {
		t.Fatalf("ensures = %d, want 1", rt.ensures)
	}
}

func TestApplyStopsAndKills(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, _ := newTestSupervisor(t, rt, []byte("jar-bytes"))
	_, _ = sup.Apply(context.Background(), spec)

	stop := spec
	stop.DesiredState = "stopped"
	stop.Revision = 2
	if state, _ := sup.Apply(context.Background(), stop); state != "stopped" {
		t.Fatalf("stop state = %q", state)
	}
	if rt.stops != 1 || rt.kills != 0 {
		t.Fatalf("stops=%d kills=%d", rt.stops, rt.kills)
	}

	start := stop
	start.DesiredState = "running"
	start.Revision = 3
	_, _ = sup.Apply(context.Background(), start)

	kill := start
	kill.DesiredState = "stopped"
	kill.KillRequested = true
	kill.Revision = 4
	if state, _ := sup.Apply(context.Background(), kill); state != "stopped" {
		t.Fatalf("kill state = %q", state)
	}
	if rt.kills != 1 {
		t.Fatalf("kills = %d, want 1", rt.kills)
	}
}

func TestApplyRejectsJarMismatch(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, _ := newTestSupervisor(t, rt, []byte("jar-bytes"))
	spec.JarSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

	state, detail := sup.Apply(context.Background(), spec)
	if state != "error" || detail == "" {
		t.Fatalf("state=%q detail=%q", state, detail)
	}
	if rt.ensures != 0 {
		t.Fatal("container was started despite a bad jar")
	}
}

func TestApplyRequiresEULA(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, _ := newTestSupervisor(t, rt, []byte("jar-bytes"))
	spec.AcceptEULA = false

	if state, _ := sup.Apply(context.Background(), spec); state != "error" {
		t.Fatalf("state = %q, want error", state)
	}
}

func TestObserveReportsExitedAndStopping(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, _ := newTestSupervisor(t, rt, []byte("jar-bytes"))

	rt.state[spec.ID] = "stopping"
	if state, _ := sup.Observe(context.Background(), spec); state != "stopping" {
		t.Fatalf("stopping state = %q", state)
	}

	rt.state[spec.ID] = "exited"
	spec.DesiredState = "stopped"
	if state, _ := sup.Observe(context.Background(), spec); state != "stopped" {
		t.Fatalf("stopped state = %q", state)
	}

	spec.DesiredState = "running"
	if state, _ := sup.Observe(context.Background(), spec); state != "exited" {
		t.Fatalf("exited state = %q", state)
	}
}

func TestCrashArtifactsOnlyOnUnexpectedExit(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, dir := newTestSupervisor(t, rt, []byte("jar-bytes"))
	_, _ = sup.Apply(context.Background(), spec)

	// Container dies while desired running: capture.
	rt.state[spec.ID] = "exited"
	if state, _ := sup.Observe(context.Background(), spec); state != "exited" {
		t.Fatalf("state = %q", state)
	}
	data, err := os.ReadFile(filepath.Join(dir, "crash", "rev-1", "container.log"))
	if err != nil {
		t.Fatalf("crash log missing: %v", err)
	}
	if string(data) != rt.logs {
		t.Fatalf("crash log = %q", data)
	}

	// Graceful stop at a new revision: no crash directory.
	stop := spec
	stop.DesiredState = "stopped"
	stop.Revision = 2
	_, _ = sup.Apply(context.Background(), stop)
	if _, err := os.Stat(filepath.Join(dir, "crash", "rev-2")); err == nil {
		t.Fatal("graceful stop was captured as a crash")
	}
}

func TestVelocitySkipsPaperBootFiles(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, dir := newTestSupervisor(t, rt, []byte("velocity-bytes"))
	spec.Base = "velocity"
	spec.AcceptEULA = false
	spec.Port = 25566
	spec.Files = map[string]string{"velocity.toml": "bind = \"0.0.0.0:25566\"\n"}

	if state, detail := sup.Apply(context.Background(), spec); state != "running" || detail != "" {
		t.Fatalf("state=%q detail=%q", state, detail)
	}
	if _, err := os.Stat(filepath.Join(dir, "velocity.toml")); err != nil {
		t.Fatalf("velocity.toml missing: %v", err)
	}
	for _, name := range []string{"eula.txt", "server.properties"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist for velocity (err=%v)", name, err)
		}
	}
}

func TestFoliaSkipsPaperBridge(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, dir := newTestSupervisor(t, rt, []byte("jar"))
	spec.Base = "folia"
	spec.AcceptEULA = true
	if state, detail := sup.Apply(context.Background(), spec); state != "running" || detail != "" {
		t.Fatalf("state=%q detail=%q", state, detail)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "DelphiBridge.jar")); !os.IsNotExist(err) {
		t.Fatalf("Folia should not receive the Paper bridge (err=%v)", err)
	}
}

func TestTemplateFilesApplyOnce(t *testing.T) {
	rt := newFakeRuntime()
	sup, spec, dir := newTestSupervisor(t, rt, []byte("jar"))
	spec.Files = map[string]string{
		"server.properties": "motd=from-template\nserver-port=25565\n",
		"config/extra.yml":  "a: 1\n",
	}
	if _, detail := sup.Apply(context.Background(), spec); detail != "" {
		t.Fatal(detail)
	}

	spec.Files["server.properties"] = "motd=changed\n"
	if err := sup.prepare(context.Background(), spec, dir); err != nil {
		t.Fatal(err)
	}
	props, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	if string(props) != "motd=from-template\nserver-port=25565\n" {
		t.Fatalf("template file was overwritten: %q", props)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "config/extra.yml")); err != nil || string(data) != "a: 1\n" {
		t.Fatalf("nested file = %q, err = %v", data, err)
	}
}
