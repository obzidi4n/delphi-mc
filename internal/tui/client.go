package tui

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"delphi.dev/delphi/internal/apitypes"
)

// Client is the panel API client used by the TUI.
type Client struct {
	base   string
	token  string
	http   *http.Client
	stream *http.Client
}

// New builds a token-authenticated client.
func New(panel, caPath, token string) (*Client, error) {
	panel = strings.TrimSpace(panel)
	if panel == "" {
		return nil, errors.New("panel URL is required (--panel)")
	}
	if token == "" {
		return nil, errors.New("token is required (--token or DELPHI_TOKEN)")
	}
	if !strings.HasPrefix(panel, "https://") && !strings.HasPrefix(panel, "http://") {
		return nil, errors.New("panel URL must be http(s)")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA file contains no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	return &Client{
		base:   strings.TrimRight(panel, "/"),
		token:  token,
		http:   &http.Client{Transport: transport, Timeout: 60 * time.Second},
		stream: &http.Client{Transport: transport},
	}, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var problem apitypes.Problem
		if json.Unmarshal(data, &problem) == nil && problem.Detail != "" {
			return fmt.Errorf("%s: %s", problem.Title, problem.Detail)
		}
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Servers lists the fleet.
func (c *Client) Servers(ctx context.Context) ([]apitypes.ServerDTO, error) {
	var out apitypes.ServerListDTO
	if err := c.do(ctx, http.MethodGet, "/api/v1/servers", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// Server fetches one server.
func (c *Client) Server(ctx context.Context, id string) (apitypes.ServerDTO, error) {
	var out apitypes.ServerDTO
	err := c.do(ctx, http.MethodGet, "/api/v1/servers/"+id, nil, &out)
	return out, err
}

// Power changes a server's desired lifecycle.
func (c *Client) Power(ctx context.Context, id, action string) (apitypes.ServerDTO, error) {
	var out apitypes.ServerDTO
	err := c.do(ctx, http.MethodPost, "/api/v1/servers/"+id+"/power", apitypes.PowerRequest{Action: action}, &out)
	return out, err
}

// Backup creates a snapshot.
func (c *Client) Backup(ctx context.Context, id string) (apitypes.BackupDTO, error) {
	var out apitypes.BackupDTO
	err := c.do(ctx, http.MethodPost, "/api/v1/servers/"+id+"/backups", nil, &out)
	return out, err
}

// Plugins lists managed plugins and updates.
func (c *Client) Plugins(ctx context.Context, id string) (apitypes.PluginListDTO, apitypes.PluginUpdateListDTO, error) {
	var plugins apitypes.PluginListDTO
	if err := c.do(ctx, http.MethodGet, "/api/v1/servers/"+id+"/plugins", nil, &plugins); err != nil {
		return plugins, apitypes.PluginUpdateListDTO{}, err
	}
	var updates apitypes.PluginUpdateListDTO
	err := c.do(ctx, http.MethodGet, "/api/v1/servers/"+id+"/updates", nil, &updates)
	return plugins, updates, err
}

// ApplyUpdates installs available plugin updates.
func (c *Client) ApplyUpdates(ctx context.Context, id string) (apitypes.PluginUpdateApplyDTO, error) {
	var out apitypes.PluginUpdateApplyDTO
	err := c.do(ctx, http.MethodPost, "/api/v1/servers/"+id+"/updates/apply", nil, &out)
	return out, err
}
