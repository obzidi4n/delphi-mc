package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ParseStats accepts both shapes the runtimes emit: podman's JSON array of
// objects ("name", "cpu", "mem_usage") and docker's per-line JSON
// ("Name", "CPUPerc", "MemUsage").
func ParseStats(raw string) ([]ContainerStat, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var objects []map[string]any
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &objects); err != nil {
			return nil, fmt.Errorf("parse stats: %w", err)
		}
	} else {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				return nil, fmt.Errorf("parse stats line: %w", err)
			}
			objects = append(objects, obj)
		}
	}

	out := make([]ContainerStat, 0, len(objects))
	for _, obj := range objects {
		name := field(obj, "name", "Name", "container", "Container")
		if name == "" {
			continue
		}
		cpu, err := parsePercent(field(obj, "cpu", "CPUPerc", "cpu_perc"))
		if err != nil {
			return nil, err
		}
		mem, err := parseMemory(field(obj, "mem_usage", "MemUsage", "mem_usage_bytes"))
		if err != nil {
			return nil, err
		}
		out = append(out, ContainerStat{Container: name, CPUPercent: cpu, MemoryBytes: mem})
	}
	return out, nil
}

func field(obj map[string]any, names ...string) string {
	for _, name := range names {
		if v, ok := obj[name]; ok {
			if s, ok := v.(string); ok {
				return s
			}
			return fmt.Sprint(v)
		}
	}
	return ""
}

func parsePercent(s string) (float64, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if s == "" || s == "--" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cpu percent %q: %w", s, err)
	}
	return v, nil
}

var memUnits = map[string]float64{
	"b": 1, "kb": 1e3, "mb": 1e6, "gb": 1e9, "tb": 1e12,
	"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40,
	"k": 1e3, "m": 1e6, "g": 1e9,
}

// parseMemory reads "12.3MB / 1GiB" or a raw byte count.
func parseMemory(s string) (int64, error) {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	i := 0
	for i < len(s) && (s[i] == '.' || s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("parse memory %q", s)
	}
	value, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, err
	}
	unit := strings.ToLower(strings.TrimSpace(s[i:]))
	mult, ok := memUnits[unit]
	if !ok {
		return 0, fmt.Errorf("parse memory unit %q", unit)
	}
	return int64(value * mult), nil
}
