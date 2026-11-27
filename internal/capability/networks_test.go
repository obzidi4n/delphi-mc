package capability

import (
	"strings"
	"testing"

	"delphi.dev/delphi/internal/store"
)

func TestVelocityConfigListsBackends(t *testing.T) {
	cfg := velocityConfig("smp", 25565, []store.Server{
		{ID: "s_1", Port: 25566},
		{ID: "s_2", Port: 25567},
	}, "secret", "legacy")
	for _, want := range []string{`config-version = "2.9"`, `bind = "0.0.0.0:25565"`, `[forced-hosts]`, `backend-1 = "delphi-s-1:25566"`, `backend-2 = "delphi-s-2:25567"`, `player-info-forwarding-mode = "legacy"`, `try = ["backend-1", "backend-2"]`} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("config missing %q:\n%s", want, cfg)
		}
	}
}

func TestOfflineServerPropertiesPatchesProxySettings(t *testing.T) {
	in := "difficulty=normal\nlevel-name=world\nmotd=x\nonline-mode=true\nserver-ip=\nserver-port=25565\n"
	got := offlineServerProperties(in, "smp-1", 25599)
	for _, want := range []string{"difficulty=normal", "online-mode=false", "server-ip=0.0.0.0", "server-port=25599"} {
		if !strings.Contains(got, want) {
			t.Fatalf("properties missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "online-mode=true") {
		t.Fatalf("online-mode not replaced:\n%s", got)
	}
}
