package api

import (
	"strings"
	"testing"
	"time"

	"delphi.dev/delphi/internal/store"
)

func TestDegradedReasonCountsOnlyLiveOfflineNodes(t *testing.T) {
	now := time.Now()
	s := &server{opts: Options{Now: func() time.Time { return now }, Heartbeat: time.Minute, StartedAt: now.Add(-time.Hour)}}
	old := now.Add(-time.Hour)
	revoked := now.Add(-time.Hour)
	nodes := []store.Node{
		{Name: "healthy", Status: "healthy", LastSeen: &now},
		{Name: "offline", Status: "disconnected", LastSeen: &old},
		{Name: "revoked", Status: "revoked", LastSeen: &old, RevokedAt: &revoked},
	}
	got := s.degradedReason(nodes)
	if !strings.Contains(got, "1 node(s) offline") {
		t.Fatalf("reason = %q", got)
	}
	if strings.Contains(got, "2 node(s)") {
		t.Fatalf("revoked node counted: %q", got)
	}
}
