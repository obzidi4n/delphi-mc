package api_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/pki"
	"delphi.dev/delphi/internal/store"
)

// fakeNodes is an in-memory NodeStore; the channel test writes from a server
// goroutine while the test reads.
type fakeNodes struct {
	mu           sync.Mutex
	node         store.Node
	enrollErr    error
	helloVersion string
	helloFS      string
	touches      int
	status       string
	revoked      bool
	tokenTTL     time.Duration
	tokenOrg     string
}

func (f *fakeNodes) CreateEnrollmentToken(_ context.Context, orgID string, ttl time.Duration) (string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenOrg, f.tokenTTL = orgID, ttl
	return "tok_test", time.Now().Add(ttl), nil
}

func (f *fakeNodes) EnrollNode(_ context.Context, _ string, n store.Node) (store.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enrollErr != nil {
		return store.Node{}, f.enrollErr
	}
	n.Status = "pending"
	f.node = n
	return n, nil
}

func (f *fakeNodes) NodeBySerial(_ context.Context, serial string) (store.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.node.CertSerial != serial {
		return store.Node{}, store.ErrNoNode
	}
	return f.node, nil
}

func (f *fakeNodes) TouchNode(_ context.Context, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revoked {
		return store.ErrNodeRevoked
	}
	f.touches++
	return nil
}

func (f *fakeNodes) UpdateNodeHello(_ context.Context, _ string, version, fs string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.helloVersion, f.helloFS = version, fs
	return nil
}

func (f *fakeNodes) SetNodeStatus(_ context.Context, _, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
	return nil
}

func (f *fakeNodes) ListNodes(context.Context) ([]store.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.node.ID == "" {
		return nil, nil
	}
	return []store.Node{f.node}, nil
}

func (f *fakeNodes) RevokeNode(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != f.node.ID {
		return store.ErrNoNode
	}
	f.revoked = true
	now := time.Now()
	f.node.RevokedAt = &now
	return nil
}

func (f *fakeNodes) snapshot() store.Node {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.node
}

func (f *fakeNodes) hello() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.helloVersion, f.helloFS
}

func (f *fakeNodes) touchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.touches
}

func (f *fakeNodes) setRevoked(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = v
}

func (f *fakeNodes) wasRevoked() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revoked
}

func (f *fakeNodes) tokenRequest() (time.Duration, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenTTL, f.tokenOrg
}

func newNodesHandler(t *testing.T, nodes *fakeNodes, ca *pki.CA) http.Handler {
	t.Helper()
	h, err := api.New(api.Options{
		BasePath:   "/",
		Version:    "test",
		Nodes:      nodes,
		AdminToken: "secret",
		CA:         ca,
		Now:        time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func adminRequest(h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func makeCSRPEM(t *testing.T, name string) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: name}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func TestEnrollmentIssuesCertificate(t *testing.T) {
	ca, err := pki.LoadOrCreateCA(pki.Dir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &fakeNodes{}
	h := newNodesHandler(t, nodes, ca)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agent/enroll", bytes.NewReader(mustJSON(t, map[string]string{
		"token": "tok_test",
		"csr":   makeCSRPEM(t, "node-a"),
		"name":  "node-a",
	})))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var out struct {
		NodeID      string `json:"node_id"`
		Certificate string `json:"certificate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.NodeID == "" || out.Certificate == "" {
		t.Fatalf("incomplete response: %+v", out)
	}
	block, _ := pem.Decode([]byte(out.Certificate))
	if block == nil {
		t.Fatal("certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := ca.VerifyClient(cert, time.Now()); err != nil {
		t.Fatalf("issued certificate does not verify: %v", err)
	}
	if n := nodes.snapshot(); n.Name != "node-a" || n.CertSerial == "" {
		t.Fatalf("node not registered: %+v", n)
	}
}

func TestEnrollmentRejectsUsedToken(t *testing.T) {
	ca, _ := pki.LoadOrCreateCA(pki.Dir(t.TempDir()))
	nodes := &fakeNodes{enrollErr: store.ErrTokenUsed}
	h := newNodesHandler(t, nodes, ca)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agent/enroll", bytes.NewReader(mustJSON(t, map[string]string{
		"token": "used", "csr": makeCSRPEM(t, "node-a"), "name": "node-a",
	})))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestEnrollmentRejectsBadCSR(t *testing.T) {
	ca, _ := pki.LoadOrCreateCA(pki.Dir(t.TempDir()))
	h := newNodesHandler(t, &fakeNodes{}, ca)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agent/enroll", bytes.NewReader(mustJSON(t, map[string]string{
		"token": "tok", "csr": "nope", "name": "node-a",
	})))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestNodeAPIRequiresToken(t *testing.T) {
	h := newNodesHandler(t, &fakeNodes{}, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestTokenAndList(t *testing.T) {
	nodes := &fakeNodes{node: store.Node{ID: "n_1", Name: "node-a", Status: "healthy", CertExpiresAt: time.Now().Add(time.Hour)}}
	h := newNodesHandler(t, nodes, nil)

	rec := adminRequest(h, http.MethodPost, "/api/v1/enrollment-tokens", map[string]int{"ttl_seconds": 120})
	if rec.Code != http.StatusOK {
		t.Fatalf("token status = %d: %s", rec.Code, rec.Body.String())
	}
	if ttl, org := nodes.tokenRequest(); ttl != 120*time.Second || org != "default" {
		t.Fatalf("token request = %v/%v", ttl, org)
	}

	rec = adminRequest(h, http.MethodGet, "/api/v1/nodes", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0]["name"] != "node-a" || out.Items[0]["status"] != "healthy" {
		t.Fatalf("unexpected list: %+v", out.Items)
	}
}

func TestRevoke(t *testing.T) {
	nodes := &fakeNodes{node: store.Node{ID: "n_1", Name: "node-a"}}
	h := newNodesHandler(t, nodes, nil)

	if rec := adminRequest(h, http.MethodPost, "/api/v1/nodes/n_1/revoke", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if !nodes.wasRevoked() {
		t.Fatal("node was not revoked")
	}
	if rec := adminRequest(h, http.MethodPost, "/api/v1/nodes/nope/revoke", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown node status = %d, want 404", rec.Code)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
