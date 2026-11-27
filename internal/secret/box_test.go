package secret

import (
	"bytes"
	"testing"
)

func TestSealOpen(t *testing.T) {
	box, err := New(t.TempDir() + "/secrets.key")
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"url":"https://discord.example/webhook"}`)
	sealed, err := box.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("discord.example")) {
		t.Fatal("plaintext leaked")
	}
	got, err := box.Open(sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open = %q, %v", got, err)
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := box.Open(sealed); err == nil {
		t.Fatal("tampered secret opened")
	}
}
