package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"delphi.dev/delphi/internal/pki"
)

func TestEnrollWritesIdentity(t *testing.T) {
	dataDir := t.TempDir()
	caDir := pki.Dir(dataDir)
	ca, err := pki.LoadOrCreateCA(caDir)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := pki.EnsureServerCert(caDir, ca, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(pki.CACertPath(dataDir))
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /agent/enroll", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token string `json:"token"`
			CSR   string `json:"csr"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Token != "tok_good" {
			http.Error(w, `{"code":"invalid_token"}`, http.StatusUnauthorized)
			return
		}
		certPEM, _, notAfter, err := pki.SignCSR(ca, []byte(req.CSR), time.Hour)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":     "n_test",
			"certificate": string(certPEM),
			"not_after":   notAfter,
		})
	})
	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}}
	srv.StartTLS()
	defer srv.Close()

	stateDir := filepath.Join(t.TempDir(), "state")
	nodeID, err := Enroll(context.Background(), EnrollOptions{
		PanelURL: srv.URL,
		Token:    "tok_good",
		Name:     "node-a",
		StateDir: stateDir,
		CAPEM:    caPEM,
	})
	if err != nil {
		t.Fatal(err)
	}
	if nodeID != "n_test" {
		t.Fatalf("node id = %q", nodeID)
	}

	cert, roots, err := LoadIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.Certificate) == 0 || roots == nil {
		t.Fatal("identity is incomplete")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "node-a" {
		t.Fatalf("CN = %q", leaf.Subject.CommonName)
	}

	info, err := os.Stat(filepath.Join(stateDir, keyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("agent.key mode = %o, want 600", perm)
	}

	caInfo, err := os.Stat(filepath.Join(stateDir, caFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !caInfo.Mode().IsRegular() {
		t.Fatal("ca.crt was not written")
	}
}

func TestEnrollRejectsBadToken(t *testing.T) {
	dataDir := t.TempDir()
	caDir := pki.Dir(dataDir)
	ca, err := pki.LoadOrCreateCA(caDir)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := pki.EnsureServerCert(caDir, ca, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(pki.CACertPath(dataDir))
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /agent/enroll", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"invalid_token"}`, http.StatusUnauthorized)
	})
	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}}
	srv.StartTLS()
	defer srv.Close()

	_, err = Enroll(context.Background(), EnrollOptions{
		PanelURL: srv.URL,
		Token:    "wrong",
		Name:     "node-a",
		StateDir: filepath.Join(t.TempDir(), "state"),
		CAPEM:    caPEM,
	})
	if err == nil {
		t.Fatal("expected enrollment to fail")
	}
}

func TestLoadIdentityWithoutEnrollment(t *testing.T) {
	if _, _, err := LoadIdentity(t.TempDir()); err == nil {
		t.Fatal("expected an error when no identity exists")
	}
}

func TestSocketURL(t *testing.T) {
	got, err := socketURL("https://panel.example:8443/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://panel.example:8443/agent/ws" {
		t.Fatalf("socketURL = %q", got)
	}
	if _, err := socketURL("http://panel.example"); err == nil {
		t.Fatal("expected http to be rejected")
	}
}
