package api

import (
	"testing"
	"time"
)

func sample(at time.Time, cpu float64) MetricPoint {
	return MetricPoint{At: at, CPUPercent: cpu, MemoryBytes: int64(cpu) * 1024}
}

func TestMetricsRollups(t *testing.T) {
	store := NewMetricsStore()
	base := time.Unix(0, 0).UTC()
	for i := 0; i <= 30; i++ {
		store.Ingest("s_1", sample(base.Add(time.Duration(i)*time.Second), float64(i)))
	}
	sec := store.Query("s_1", "1s", 100)
	if len(sec) != 31 || sec[0].CPUPercent != 0 || sec[30].CPUPercent != 30 {
		t.Fatalf("1s points = %d (%v)", len(sec), sec)
	}
	m15 := store.Query("s_1", "15s", 10)
	if len(m15) != 2 {
		t.Fatalf("15s points = %d (%v)", len(m15), m15)
	}
	if m15[0].CPUPercent != 7 || m15[1].CPUPercent != 22 {
		t.Fatalf("15s averages = %v", m15)
	}
	if m15[0].MemoryBytes != 7*1024 {
		t.Fatalf("memory average = %d", m15[0].MemoryBytes)
	}
	if len(store.Query("s_1", "5m", 10)) != 0 {
		t.Fatal("5m bucket flushed before its boundary")
	}
}

func TestMetricsFiveMinuteRollup(t *testing.T) {
	store := NewMetricsStore()
	base := time.Unix(0, 0).UTC()
	for i := 0; i <= 300; i++ {
		store.Ingest("s_1", sample(base.Add(time.Duration(i)*time.Second), 10))
	}
	m5 := store.Query("s_1", "5m", 5)
	if len(m5) != 1 || m5[0].CPUPercent != 10 {
		t.Fatalf("5m = %v", m5)
	}
	if got := store.Query("s_1", "15m", 5); len(got) == 0 {
		t.Fatal("15m alias returned nothing")
	}
}

func TestMetricsLimitAndUnknownServer(t *testing.T) {
	store := NewMetricsStore()
	base := time.Unix(0, 0).UTC()
	for i := 0; i < 10; i++ {
		store.Ingest("s_1", sample(base.Add(time.Duration(i)*time.Second), float64(i)))
	}
	got := store.Query("s_1", "1s", 3)
	if len(got) != 3 || got[2].CPUPercent != 9 {
		t.Fatalf("limit = %v", got)
	}
	if store.Query("nope", "1s", 5) != nil {
		t.Fatal("unknown server returned points")
	}
}
