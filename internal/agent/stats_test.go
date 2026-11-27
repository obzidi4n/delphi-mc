package agent

import "testing"

func TestParseStatsPodmanArray(t *testing.T) {
	raw := `[{"name":"delphi-s_1","cpu":"12.34%","mem_usage":"512.5MB / 1GiB"},
	         {"name":"delphi-s_2","cpu":"0.00%","mem_usage":"0B / 1GiB"}]`
	stats, err := ParseStats(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].Container != "delphi-s_1" || stats[0].CPUPercent != 12.34 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats[0].MemoryBytes < 512_000_000 || stats[0].MemoryBytes > 513_000_000 {
		t.Fatalf("memory = %d", stats[0].MemoryBytes)
	}
	if stats[1].MemoryBytes != 0 {
		t.Fatalf("zero memory = %d", stats[1].MemoryBytes)
	}
}

func TestParseStatsDockerLines(t *testing.T) {
	raw := `{"Container":"abc","Name":"delphi-s_1","CPUPerc":"3.50%","MemUsage":"1.5MiB / 2GiB"}
{"Container":"def","Name":"delphi-s_2","CPUPerc":"0.00%","MemUsage":"0B / 2GiB"}`
	stats, err := ParseStats(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].CPUPercent != 3.5 || stats[0].MemoryBytes != 1.5*(1<<20) {
		t.Fatalf("stats = %+v", stats)
	}
}
