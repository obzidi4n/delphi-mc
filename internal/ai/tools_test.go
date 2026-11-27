package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

func TestRunPluginsListsVersionsAndUpdates(t *testing.T) {
	tb := &Toolbox{
		Plugins: func(context.Context, capability.Principal, string) ([]store.ServerPlugin, error) {
			return []store.ServerPlugin{
				{Project: "luckperms", Version: "5.5.53", Provider: "modrinth", Policy: "pinned"},
				{Project: "chunky", Version: "1.4.40", Provider: "modrinth", Policy: "latest"},
			}, nil
		},
		Updates: func(context.Context, capability.Principal, string) ([]capability.PluginUpdate, error) {
			return []capability.PluginUpdate{{Project: "luckperms", Current: "5.5.53", Latest: "5.5.71"}}, nil
		},
	}
	res, err := tb.runPlugins(context.Background(), capability.Principal{OrgID: "default"},
		json.RawMessage(`{"server":"SMP"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"luckperms 5.5.53", "chunky 1.4.40", "5.5.53 -> 5.5.71"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("result missing %q:\n%s", want, res.Text)
		}
	}
	if len(res.Citations) != 1 || res.Citations[0].Kind != "server" {
		t.Fatalf("citations = %+v", res.Citations)
	}
}
