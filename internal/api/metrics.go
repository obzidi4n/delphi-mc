package api

import (
	"sync"
	"time"
)

// MetricPoint is one sample or bucket average.
type MetricPoint struct {
	At          time.Time `json:"at"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemoryBytes int64     `json:"memory_bytes"`
}

// Retention documented in docs/architecture/agent.md.
const (
	secCap   = 3600
	m15Cap   = 5760
	m5Cap    = 8640
	queryMax = 2000
)

// MetricsStore keeps the three rollups in memory: 1s for 1h, 15s for 24h,
// 5m for 30d. Persistence is a later concern; the API reads these rings.
type MetricsStore struct {
	mu      sync.Mutex
	servers map[string]*metricSeries
}

type metricSeries struct {
	sec []MetricPoint
	m15 []MetricPoint
	m5  []MetricPoint

	cur15 bucket
	cur5  bucket
}

type bucket struct {
	start time.Time
	cpu   float64
	mem   float64
	n     int
}

// NewMetricsStore returns an empty store.
func NewMetricsStore() *MetricsStore {
	return &MetricsStore{servers: map[string]*metricSeries{}}
}

// Ingest adds one 1s sample and feeds the aggregations.
func (m *MetricsStore) Ingest(serverID string, p MetricPoint) {
	if serverID == "" {
		return
	}
	p.At = p.At.UTC().Truncate(time.Second)
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.servers[serverID]
	if s == nil {
		s = &metricSeries{}
		m.servers[serverID] = s
	}
	s.sec = appendCapped(s.sec, p, secCap)

	slot := p.At.Truncate(15 * time.Second)
	if s.cur15.n == 0 {
		s.cur15.start = slot
	}
	if !s.cur15.start.Equal(slot) {
		m.flush15(s)
		s.cur15 = bucket{start: slot}
	}
	s.cur15.cpu += p.CPUPercent
	s.cur15.mem += float64(p.MemoryBytes)
	s.cur15.n++

	slot5 := p.At.Truncate(5 * time.Minute)
	if s.cur5.n == 0 {
		s.cur5.start = slot5
	}
	if !s.cur5.start.Equal(slot5) {
		m.flush5(s)
		s.cur5 = bucket{start: slot5}
	}
	s.cur5.cpu += p.CPUPercent
	s.cur5.mem += float64(p.MemoryBytes)
	s.cur5.n++
}

func (m *MetricsStore) flush15(s *metricSeries) {
	if s.cur15.n == 0 {
		return
	}
	s.m15 = appendCapped(s.m15, MetricPoint{
		At: s.cur15.start, CPUPercent: s.cur15.cpu / float64(s.cur15.n),
		MemoryBytes: int64(s.cur15.mem / float64(s.cur15.n)),
	}, m15Cap)
}

func (m *MetricsStore) flush5(s *metricSeries) {
	if s.cur5.n == 0 {
		return
	}
	s.m5 = appendCapped(s.m5, MetricPoint{
		At: s.cur5.start, CPUPercent: s.cur5.cpu / float64(s.cur5.n),
		MemoryBytes: int64(s.cur5.mem / float64(s.cur5.n)),
	}, m5Cap)
}

func appendCapped(points []MetricPoint, p MetricPoint, limit int) []MetricPoint {
	points = append(points, p)
	if len(points) > limit {
		points = points[len(points)-limit:]
	}
	return points
}

// Query returns the newest points for a resolution: 1s, 15m, or 5m.
// ("15m" is the 15-second rollup's name in the API to avoid digit confusion.)
func (m *MetricsStore) Query(serverID, resolution string, limit int) []MetricPoint {
	if limit <= 0 {
		limit = 60
	}
	if limit > queryMax {
		limit = queryMax
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.servers[serverID]
	if s == nil {
		return nil
	}
	var src []MetricPoint
	switch resolution {
	case "15s", "15m":
		src = s.m15
	case "5m":
		src = s.m5
	default:
		src = s.sec
	}
	if len(src) > limit {
		src = src[len(src)-limit:]
	}
	out := make([]MetricPoint, len(src))
	copy(out, src)
	return out
}

// Latest returns the newest 1s sample and whether one exists.
func (m *MetricsStore) Latest(serverID string) (MetricPoint, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.servers[serverID]
	if s == nil || len(s.sec) == 0 {
		return MetricPoint{}, false
	}
	return s.sec[len(s.sec)-1], true
}
