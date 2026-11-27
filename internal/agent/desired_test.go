package agent

import (
	"testing"

	"delphi.dev/delphi/internal/wire"
)

func TestDesiredRoundTrip(t *testing.T) {
	dir := t.TempDir()
	items := []wire.ServerSpec{{ID: "s_1", DesiredState: "running", RestartPolicy: "on-failure", Revision: 7}}
	if err := saveDesired(dir, items); err != nil {
		t.Fatal(err)
	}
	got, err := loadDesired(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "s_1" || got[0].DesiredState != "running" || got[0].Revision != 7 {
		t.Fatalf("got %+v", got)
	}
}
