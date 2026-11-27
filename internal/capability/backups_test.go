package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"delphi.dev/delphi/internal/backup"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

type fakeBackupStore struct {
	srv     store.Server
	rows    []store.Backup
	deleted []string
	audits  []string
}

func (f *fakeBackupStore) GetServer(context.Context, string) (store.Server, error) { return f.srv, nil }
func (f *fakeBackupStore) CreateBackup(_ context.Context, b store.Backup) (store.Backup, error) {
	b.OrgID, b.NodeID, b.State = f.srv.OrgID, f.srv.NodeID, "creating"
	f.rows = append(f.rows, b)
	return b, nil
}
func (f *fakeBackupStore) BackupByID(_ context.Context, id string) (store.Backup, error) {
	for _, b := range f.rows {
		if b.ID == id {
			return b, nil
		}
	}
	return store.Backup{}, store.ErrNoBackup
}
func (f *fakeBackupStore) ListBackups(context.Context, string, string, int) ([]store.Backup, error) {
	return f.rows, nil
}
func (f *fakeBackupStore) AllBackupsForServer(context.Context, string) ([]store.Backup, error) {
	return f.rows, nil
}
func (f *fakeBackupStore) MarkBackupReady(_ context.Context, id string, size int64, sha string) error {
	for i := range f.rows {
		if f.rows[i].ID == id {
			f.rows[i].State, f.rows[i].Size, f.rows[i].SHA256 = "ready", size, sha
		}
	}
	return nil
}
func (f *fakeBackupStore) FailBackup(_ context.Context, id, msg string) error {
	for i := range f.rows {
		if f.rows[i].ID == id {
			f.rows[i].State, f.rows[i].Error = "failed", msg
		}
	}
	return nil
}
func (f *fakeBackupStore) DeleteBackup(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	var keep []store.Backup
	for _, b := range f.rows {
		if b.ID != id {
			keep = append(keep, b)
		}
	}
	f.rows = keep
	return nil
}
func (f *fakeBackupStore) SetDesiredState(_ context.Context, id, desired string, _ bool) (store.Server, error) {
	f.srv.DesiredState = desired
	f.srv.DesiredRevision++
	return f.srv, nil
}
func (f *fakeBackupStore) Audit(_ context.Context, _, _, _, action, _ string, _ any) error {
	f.audits = append(f.audits, action)
	return nil
}

type fakeBackupHub struct {
	store *fakeBackupStore
	typ   string
}

func (h *fakeBackupHub) Call(_ context.Context, _ string, build func(string) any, _ time.Duration) (json.RawMessage, error) {
	req := build("req_1").(wire.BackupRequest)
	if req.Type == wire.TypeBackupCreate {
		_ = h.store.MarkBackupReady(context.Background(), req.BackupID, 42, strings.Repeat("a", 64))
	}
	return json.Marshal(wire.BackupResult{Type: wire.TypeBackupResult, RequestID: "req_1", OK: true})
}

func backupFixture(t *testing.T) (*Backups, *fakeBackupStore) {
	t.Helper()
	fake := &fakeBackupStore{srv: store.Server{
		ID: "s_1", OrgID: "default", NodeID: "n_1", DesiredState: "running", DesiredRevision: 1,
	}}
	files, err := backup.New(t.TempDir(), t.TempDir()+"/backup.key")
	if err != nil {
		t.Fatal(err)
	}
	return &Backups{Store: fake, Files: files, Hub: &fakeBackupHub{store: fake}}, fake
}

func TestCreateBackupAwaitsReadyAndAudits(t *testing.T) {
	svc, fake := backupFixture(t)
	p := Principal{UserID: "u", OrgID: "default", Via: "ui", Scopes: []string{"server:s_1:update"}}
	row, err := svc.Create(context.Background(), p, "s_1")
	if err != nil {
		t.Fatal(err)
	}
	if row.State != "ready" || row.Size != 42 {
		t.Fatalf("row = %+v", row)
	}
	if len(fake.audits) == 0 || fake.audits[0] != "backup.create" {
		t.Fatalf("audits = %v", fake.audits)
	}
}

func TestRestoreBumpsRevision(t *testing.T) {
	svc, fake := backupFixture(t)
	p := Principal{UserID: "u", OrgID: "default", Via: "ui", Scopes: []string{"server:s_1:update"}}
	row, err := svc.Create(context.Background(), p, "s_1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Restore(context.Background(), p, row.ID); err != nil {
		t.Fatal(err)
	}
	if fake.srv.DesiredRevision != 2 {
		t.Fatalf("revision = %d", fake.srv.DesiredRevision)
	}
	found := false
	for _, a := range fake.audits {
		if a == "backup.restore" {
			found = true
		}
	}
	if !found {
		t.Fatalf("audits = %v", fake.audits)
	}
}

func TestPruneKeepsNewestSeven(t *testing.T) {
	fake := &fakeBackupStore{}
	for i := 0; i < 9; i++ {
		fake.rows = append(fake.rows, store.Backup{ID: fmt.Sprintf("b_%016x", i), ServerID: "s_1"})
	}
	svc := &Backups{Store: fake}
	svc.prune(context.Background(), "s_1")
	if len(fake.deleted) != 2 {
		t.Fatalf("deleted = %v", fake.deleted)
	}
	if fake.deleted[0] != "b_0000000000000007" || fake.deleted[1] != "b_0000000000000008" {
		t.Fatalf("deleted = %v", fake.deleted)
	}
}
