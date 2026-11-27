package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

func (s *server) drainNode(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.opts.Services == nil || s.opts.Services.Fleet == nil {
		writeProblem(w, http.StatusNotImplemented, "fleet_disabled", "the fleet service is not configured")
		return
	}
	draining := r.URL.Query().Get("draining") != "false"
	err := s.opts.Services.Fleet.SetDrain(r.Context(), principal, r.PathValue("id"), draining)
	if errors.Is(err, store.ErrNoNode) {
		writeProblem(w, http.StatusNotFound, "node_not_found", "no such node")
		return
	}
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "draining": draining})
}

// uploadAgentUpdate stores a signed agent binary and pushes it to every live
// node. The agent verifies the signature against its configured public key.
func (s *server) uploadAgentUpdate(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !auth.Allows(principal.Scopes, "org:"+principal.OrgID+":admin") && !auth.Allows(principal.Scopes, "*") {
		writeProblem(w, http.StatusForbidden, "forbidden", "org admin is required")
		return
	}
	if s.opts.AgentUpdatesDir == "" || s.opts.Hub == nil || s.opts.Services == nil || s.opts.Services.Fleet == nil {
		writeProblem(w, http.StatusNotImplemented, "updates_disabled", "agent updates are not configured")
		return
	}
	version := strings.TrimSpace(r.Header.Get("X-Delphi-Version"))
	signature := strings.TrimSpace(r.Header.Get("X-Delphi-Signature"))
	if version == "" || signature == "" {
		writeProblem(w, http.StatusBadRequest, "missing_headers", "X-Delphi-Version and X-Delphi-Signature are required")
		return
	}
	if err := os.MkdirAll(s.opts.AgentUpdatesDir, 0o700); err != nil {
		writeProblem(w, http.StatusInternalServerError, "update_storage", err.Error())
		return
	}
	tmp, err := os.CreateTemp(s.opts.AgentUpdatesDir, ".upload-*")
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "update_storage", err.Error())
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hasher := sha256.New()
	body := http.MaxBytesReader(w, r.Body, 256<<20)
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), body); err != nil {
		tmp.Close()
		writeProblem(w, http.StatusBadRequest, "upload_failed", err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		writeProblem(w, http.StatusInternalServerError, "upload_failed", err.Error())
		return
	}
	sha := hex.EncodeToString(hasher.Sum(nil))
	target := filepath.Join(s.opts.AgentUpdatesDir, sha)
	if err := os.Rename(tmpName, target); err != nil {
		writeProblem(w, http.StatusInternalServerError, "update_storage", err.Error())
		return
	}
	frame := wire.AgentUpdateFrame{Type: wire.TypeAgentUpdate, SHA256: sha, Version: version, Signature: signature}
	nodes, _, err := s.opts.Services.Fleet.ListNodes(r.Context(), principal, 200, "")
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	sent, failed := 0, 0
	for _, node := range nodes {
		if node.RevokedAt != nil || node.Status != "healthy" {
			continue
		}
		if err := s.opts.Hub.Send(r.Context(), node.ID, frame); err != nil {
			failed++
			continue
		}
		sent++
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"sha256": sha, "version": version, "sent": sent, "failed": failed,
	})
}

// downloadAgentUpdate serves a stored agent binary to a node certificate.
func (s *server) downloadAgentUpdate(w http.ResponseWriter, r *http.Request) {
	if s.opts.AgentUpdatesDir == "" || s.opts.Nodes == nil {
		writeProblem(w, http.StatusNotImplemented, "updates_disabled", "agent updates are not configured")
		return
	}
	if _, ok := s.agentNode(w, r); !ok {
		return
	}
	sha := r.PathValue("sha")
	if len(sha) != 64 || strings.ContainsAny(sha, "/\\..") {
		writeProblem(w, http.StatusBadRequest, "invalid_artifact", "invalid sha256")
		return
	}
	path := filepath.Join(s.opts.AgentUpdatesDir, sha)
	file, err := os.Open(path)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "artifact_not_found", "no such update")
		return
	}
	defer file.Close()
	http.ServeContent(w, r, sha, fileModTime(file), file)
}

func fileModTime(file *os.File) time.Time {
	if info, err := file.Stat(); err == nil {
		return info.ModTime()
	}
	return time.Now()
}

// placeNode resolves "auto"/empty node ids for server creation.
func (s *server) placeNode(w http.ResponseWriter, r *http.Request, principal capability.Principal) (string, bool) {
	if s.opts.Services == nil || s.opts.Services.Fleet == nil {
		writeProblem(w, http.StatusNotImplemented, "fleet_disabled", "the fleet service is not configured")
		return "", false
	}
	node, err := s.opts.Services.Fleet.Place(r.Context(), principal)
	if err != nil {
		writeCapabilityError(w, err)
		return "", false
	}
	return node.ID, true
}
