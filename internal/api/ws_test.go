package api_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/pki"
	"delphi.dev/delphi/internal/store"
)

func TestAgentChannelOverMTLS(t *testing.T) {
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

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: "node-a"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, serial, _, err := pki.SignCSR(ca,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair(certPEM,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}

	nodes := &fakeNodes{node: store.Node{ID: "n_1", Name: "node-a", CertSerial: serial}}
	handler, err := api.New(api.Options{
		BasePath:   "/",
		Version:    "test",
		Nodes:      nodes,
		AdminToken: "secret",
		CA:         ca,
		Heartbeat:  2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    ca.Pool(),
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      ca.Pool(),
		MinVersion:   tls.VersionTLS12,
	}
	wsURL := "wss" + strings.TrimPrefix(srv.URL, "https") + "/agent/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	readCtx, cancelRead := context.WithTimeout(ctx, 3*time.Second)
	_, ack, err := conn.Read(readCtx)
	cancelRead()
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if !strings.Contains(string(ack), "hello.ack") {
		t.Fatalf("ack = %s", ack)
	}

	if err := conn.Write(ctx, websocket.MessageText, mustJSON(t, map[string]any{
		"type": "hello", "agent_version": "test-1", "fs_capability": "reflink",
	})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		version, _ := nodes.hello()
		return version == "test-1"
	})
	if _, fs := nodes.hello(); fs != "reflink" {
		t.Fatalf("fs capability = %q", fs)
	}

	before := nodes.touchCount()
	if err := conn.Write(ctx, websocket.MessageText, mustJSON(t, map[string]any{"type": "heartbeat"})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool { return nodes.touchCount() > before })

	nodes.setRevoked(true)
	if err := conn.Write(ctx, websocket.MessageText, mustJSON(t, map[string]any{"type": "heartbeat"})); err != nil {
		t.Fatal(err)
	}
	readCtx, cancelRead = context.WithTimeout(ctx, 5*time.Second)
	_, _, err = conn.Read(readCtx)
	cancelRead()
	if err == nil {
		t.Fatal("expected the channel to close after revocation")
	}
}

func TestAgentChannelRejectsUnknownCertificate(t *testing.T) {
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

	otherCA, err := pki.LoadOrCreateCA(pki.Dir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	csrDER, _ := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: "stranger"}}, key)
	certPEM, _, _, err := pki.SignCSR(otherCA,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	clientCert, err := tls.X509KeyPair(certPEM,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}

	handler, err := api.New(api.Options{BasePath: "/", Nodes: &fakeNodes{}, CA: ca, Heartbeat: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    ca.Pool(),
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      ca.Pool(),
		MinVersion:   tls.VersionTLS12,
	}
	wsURL := "wss" + strings.TrimPrefix(srv.URL, "https") + "/agent/ws"
	if _, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}},
	}); err == nil {
		t.Fatal("a certificate from another CA was accepted")
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}
