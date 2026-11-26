package pki

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadOrCreateCAIsIdempotent(t *testing.T) {
	dir := Dir(t.TempDir())

	first, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cert.SerialNumber.Cmp(second.Cert.SerialNumber) != 0 {
		t.Fatal("CA was regenerated instead of loaded")
	}

	info, err := os.Stat(filepath.Join(dir, caKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("ca.key mode = %o, want 600", perm)
	}
}

func TestSignCSRAndVerify(t *testing.T) {
	ca, err := LoadOrCreateCA(Dir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: "node-a"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	certPEM, serial, notAfter, err := SignCSR(ca, csrPEM, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if serial == "" || time.Until(notAfter) < 29*24*time.Hour {
		t.Fatalf("unexpected certificate metadata: serial=%q notAfter=%v", serial, notAfter)
	}

	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "node-a" {
		t.Fatalf("CN = %q, want node-a", cert.Subject.CommonName)
	}
	if cert.SerialNumber.String() != serial {
		t.Fatalf("serial mismatch: %s vs %s", cert.SerialNumber, serial)
	}
	if err := ca.VerifyClient(cert, time.Now()); err != nil {
		t.Fatalf("certificate does not verify: %v", err)
	}

	other, err := LoadOrCreateCA(Dir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := other.VerifyClient(cert, time.Now()); err == nil {
		t.Fatal("certificate verified against the wrong CA")
	}
}

func TestServerCertHasLoopbackSAN(t *testing.T) {
	ca, err := LoadOrCreateCA(Dir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := EnsureServerCert(Dir(t.TempDir()), ca, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.VerifyHostname("127.0.0.1"); err != nil {
		t.Fatalf("loopback SAN missing: %v", err)
	}
	if err := leaf.VerifyHostname("localhost"); err != nil {
		t.Fatalf("localhost SAN missing: %v", err)
	}
}

func TestSignCSRRejectsGarbage(t *testing.T) {
	ca, err := LoadOrCreateCA(Dir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := SignCSR(ca, []byte("not a csr"), time.Hour); err == nil {
		t.Fatal("expected an error")
	}
}
