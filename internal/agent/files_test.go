package agent

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func newFileSupervisor(t *testing.T) (*Supervisor, string) {
	t.Helper()
	data := t.TempDir()
	sup := NewSupervisor(data, &fakeRuntime{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := os.MkdirAll(filepath.Join(data, "servers", "s_1"), 0o755); err != nil {
		t.Fatal(err)
	}
	return sup, data
}

func TestFileWriteReadAndList(t *testing.T) {
	sup, _ := newFileSupervisor(t)
	sum, err := sup.FileWrite("s_1", "server.properties", "motd=hello\n")
	if err != nil || sum == "" {
		t.Fatalf("write: %v", err)
	}
	content, sha, size, err := sup.FileRead("s_1", "server.properties")
	if err != nil || content != "motd=hello\n" || sha != sum || size != int64(len(content)) {
		t.Fatalf("read: %q %q %d %v", content, sha, size, err)
	}
	entries, err := sup.FileList("s_1", "")
	if err != nil || len(entries) != 1 || entries[0].Name != "server.properties" {
		t.Fatalf("list: %+v %v", entries, err)
	}
}

func TestFileOperationsRejectEscapesAndBinaries(t *testing.T) {
	sup, data := newFileSupervisor(t)
	outside := filepath.Join(data, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret.txt", "..", "/etc/passwd"} {
		if _, err := sup.FileWrite("s_1", path, "x"); err == nil {
			t.Fatalf("%q write accepted", path)
		}
	}
	if err := os.Symlink(outside, filepath.Join(data, "servers", "s_1", "link.txt")); err == nil {
		if _, _, _, err := sup.FileRead("s_1", "link.txt"); err == nil {
			t.Fatal("symlink escape read accepted")
		}
	}
	if err := os.WriteFile(filepath.Join(data, "servers", "s_1", "blob.bin"), []byte{0, 1, 2, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := sup.FileRead("s_1", "blob.bin"); err == nil {
		t.Fatal("binary read accepted")
	}
	if _, _, _, err := sup.FileRead("bad/../id", "x"); err == nil {
		t.Fatal("unsafe server id accepted")
	}
}
