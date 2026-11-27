package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestPutDedupesAndRejectsMismatch(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := "jar-bytes"
	sum := sha256.Sum256([]byte(body))
	want := hex.EncodeToString(sum[:])

	sha, size, err := store.Put(strings.NewReader(body), "", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if sha != want || size != int64(len(body)) {
		t.Fatalf("got %s %d", sha, size)
	}
	if _, _, err := store.Put(strings.NewReader(body), want, 1024); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one stored file, got %d", len(entries))
	}
	if _, _, err := store.Put(strings.NewReader(body), strings.Repeat("a", 64), 1024); err == nil {
		t.Fatal("mismatched sha must fail")
	}
	if _, _, err := store.Put(strings.NewReader("too big"), "", 3); err == nil {
		t.Fatal("oversized artifact must fail")
	}
	if err := store.Remove(want); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Dir + "/" + want); !os.IsNotExist(err) {
		t.Fatal("artifact should be removed")
	}
}
