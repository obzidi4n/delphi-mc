package agent

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"delphi.dev/delphi/internal/wire"
)

func TestObserveRestartsCrashedServer(t *testing.T) {
	rt := newFakeRuntime()
	rt.state["s_1"] = "exited"
	sup := NewSupervisor(t.TempDir(), rt, slog.New(slog.NewTextHandler(io.Discard, nil)))
	sup.RestartDelay = 10 * time.Millisecond
	dir := filepath.Join(sup.DataDir, "servers", "s_1")
	if _, err := EnsureServerDir(dir); err != nil {
		t.Fatal(err)
	}
	spec := wire.ServerSpec{ID: "s_1", DesiredState: "running", RestartPolicy: "on-failure", Revision: 1}

	state, _ := sup.Observe(context.Background(), spec)
	if state != "exited" {
		t.Fatalf("first observation = %q, want exited", state)
	}
	if rt.starts != 0 {
		t.Fatal("restarted before the panel saw the crash")
	}
	time.Sleep(15 * time.Millisecond)
	state, detail := sup.Observe(context.Background(), spec)
	if state != "running" {
		t.Fatalf("second observation = %q (%s)", state, detail)
	}
	if rt.starts != 1 {
		t.Fatalf("starts = %d", rt.starts)
	}
}

func TestNeverPolicyDoesNotRestart(t *testing.T) {
	rt := newFakeRuntime()
	rt.state["s_1"] = "exited"
	sup := NewSupervisor(t.TempDir(), rt, slog.New(slog.NewTextHandler(io.Discard, nil)))
	dir := filepath.Join(sup.DataDir, "servers", "s_1")
	if _, err := EnsureServerDir(dir); err != nil {
		t.Fatal(err)
	}
	spec := wire.ServerSpec{ID: "s_1", DesiredState: "running", RestartPolicy: "never"}
	if state, _ := sup.Observe(context.Background(), spec); state != "exited" {
		t.Fatalf("state = %q", state)
	}
	time.Sleep(5 * time.Millisecond)
	sup.Observe(context.Background(), spec)
	if rt.starts != 0 {
		t.Fatal("never policy restarted the container")
	}
}
