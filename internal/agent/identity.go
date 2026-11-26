package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	caFileName   = "ca.crt"
	certFileName = "agent.crt"
	keyFileName  = "agent.key"
)

// EnrollOptions configures one-time enrollment.
type EnrollOptions struct {
	PanelURL string
	Token    string
	Name     string
	StateDir string
	CAPEM    []byte
}

// Enroll generates a key, submits a CSR with the token, and writes the identity
// files into StateDir. It returns the panel-assigned node id.
func Enroll(ctx context.Context, opts EnrollOptions) (string, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return "", errors.New("enroll: name is required")
	}
	if strings.TrimSpace(opts.StateDir) == "" {
		return "", errors.New("enroll: state dir is required")
	}

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	csrPEM, err := makeCSR(key, opts.Name)
	if err != nil {
		return "", err
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(opts.CAPEM) {
		return "", errors.New("enroll: the panel CA certificate is not valid PEM")
	}
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}},
	}

	body, _ := json.Marshal(map[string]string{
		"token": opts.Token,
		"csr":   string(csrPEM),
		"name":  opts.Name,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(opts.PanelURL, "/")+"/agent/enroll", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build enroll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("enroll: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("enroll: %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}

	var out struct {
		NodeID      string    `json:"node_id"`
		Certificate string    `json:"certificate"`
		NotAfter    time.Time `json:"not_after"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return "", fmt.Errorf("decode enroll response: %w", err)
	}
	if out.NodeID == "" || out.Certificate == "" {
		return "", errors.New("enroll: response is missing the node id or certificate")
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("marshal key: %w", err)
	}
	files := map[string]struct {
		data []byte
		mode os.FileMode
	}{
		keyFileName:  {pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600},
		certFileName: {[]byte(out.Certificate), 0o644},
		caFileName:   {opts.CAPEM, 0o644},
	}
	if err := os.MkdirAll(opts.StateDir, 0o700); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	for name, file := range files {
		path := filepath.Join(opts.StateDir, name)
		if err := os.WriteFile(path, file.data, file.mode); err != nil {
			return "", fmt.Errorf("write %s: %w", path, err)
		}
	}
	return out.NodeID, nil
}

func makeCSR(key ed25519.PrivateKey, name string) ([]byte, error) {
	tmpl := &x509.CertificateRequest{Subject: pkix.Name{CommonName: name}}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return nil, fmt.Errorf("create csr: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

// LoadIdentity reads the enrollment state: the client certificate and the CA
// pool used to verify the panel.
func LoadIdentity(stateDir string) (tls.Certificate, *x509.CertPool, error) {
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(stateDir, certFileName),
		filepath.Join(stateDir, keyFileName),
	)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("load agent identity (run `delphi agent enroll` first): %w", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(stateDir, caFileName))
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("load panel CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return tls.Certificate{}, nil, errors.New("the panel CA file is not valid PEM")
	}
	return cert, roots, nil
}

func tlsClientConfig(cert tls.Certificate, roots *x509.CertPool) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      roots,
		MinVersion:   tls.VersionTLS12,
	}
}
