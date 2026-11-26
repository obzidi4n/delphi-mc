package store

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreValid(t *testing.T) {
	all, err := loadMigrations(migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("no embedded migrations")
	}
	if all[0].version != 1 || !strings.HasSuffix(all[0].name, ".sql") {
		t.Fatalf("unexpected first migration: %+v", all[0])
	}
	if !strings.Contains(all[0].sql, "create table orgs") {
		t.Fatal("baseline does not create orgs")
	}
	for i := 1; i < len(all); i++ {
		if all[i].version <= all[i-1].version {
			t.Fatalf("migrations not strictly ordered: %d then %d", all[i-1].version, all[i].version)
		}
	}
}

func TestParseMigrationName(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int64
		wantErr bool
	}{
		{"0001_baseline.sql", 1, false},
		{"0042_add_groups.sql", 42, false},
		{"bad.sql", 0, true},
		{"x_baseline.sql", 0, true},
		{"0000_zero.sql", 0, true},
		{"0002_missing_suffix", 0, true},
	} {
		got, err := parseMigrationName(tc.name)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: expected an error", tc.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.name, err)
			continue
		}
		if got != tc.version {
			t.Errorf("%q: version = %d, want %d", tc.name, got, tc.version)
		}
	}
}

func TestPlanMigrationsSkipsApplied(t *testing.T) {
	all := []migration{{version: 1}, {version: 2}, {version: 3}}
	pending, err := planMigrations(all, map[int64]bool{1: true, 3: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].version != 2 {
		t.Fatalf("pending = %+v, want only version 2", pending)
	}
}
