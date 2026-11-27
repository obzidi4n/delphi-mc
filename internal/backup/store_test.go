package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := New(filepath.Join(dir, "backups"), filepath.Join(dir, "backup.key"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestEncryptDecryptRoundTripAndTamper(t *testing.T) {
	store := newStore(t)
	plain := bytes.Repeat([]byte("world-data-"), 200000) // crosses chunk boundaries
	sha, size, err := store.Encrypt("b_0123456789abcdef", bytes.NewReader(plain))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(plain)
	if sha != hex.EncodeToString(sum[:]) || size != int64(len(plain)) {
		t.Fatalf("sha/size = %s/%d", sha, size)
	}
	path, _ := store.Path("b_0123456789abcdef")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte(magic)) {
		t.Fatal("missing magic header")
	}
	if bytes.Contains(raw, []byte("world-data")) {
		t.Fatal("plaintext leaked into the backup")
	}

	var out bytes.Buffer
	if err := store.Decrypt("b_0123456789abcdef", &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), plain) {
		t.Fatal("round trip mismatch")
	}

	raw[len(raw)-1] ^= 0xff
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Decrypt("b_0123456789abcdef", &bytes.Buffer{}); err == nil {
		t.Fatal("tampered backup must fail")
	}
}

func TestRejectsPathsAndWhitespace(t *testing.T) {
	store := newStore(t)
	if _, err := store.Path("../../etc/passwd"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, err := store.Path("b_deadbeef"); err == nil {
		t.Fatal("short id accepted")
	}
	if _, err := store.Path(strings.Repeat("b", 20)); err == nil {
		t.Fatal("malformed id accepted")
	}
}

func TestDecryptWithDifferentKeyFails(t *testing.T) {
	store := newStore(t)
	if _, _, err := store.Encrypt("b_0123456789abcdef", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	other := newStore(t)
	path, _ := store.Path("b_0123456789abcdef")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	otherPath, _ := other.Path("b_0123456789abcdef")
	if err := os.WriteFile(otherPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := other.Decrypt("b_0123456789abcdef", &bytes.Buffer{}); err == nil {
		t.Fatal("wrong key must fail")
	}
}
