package api

import (
	"errors"
	"log/slog"
	"net/http"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

func (s *server) backupService(w http.ResponseWriter) *capability.Backups {
	if s.opts.Backups == nil {
		writeProblem(w, http.StatusNotImplemented, "backups_disabled", "the backup service is not configured")
		return nil
	}
	return s.opts.Backups
}

func toBackupDTO(b store.Backup) apitypes.BackupDTO {
	item := apitypes.BackupDTO{
		ID: b.ID, ServerID: b.ServerID, State: b.State, SizeBytes: b.Size,
		SHA256: b.SHA256, Format: b.Format, Encrypted: b.Encrypted,
		CreatedAt: b.CreatedAt, ReadyAt: b.ReadyAt,
	}
	if b.Error != "" {
		item.Error = strPtr(b.Error)
	}
	return item
}

func (s *server) listBackups(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.backupService(w)
	if service == nil {
		return
	}
	items, err := service.List(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := make([]apitypes.BackupDTO, 0, len(items))
	for _, b := range items {
		out = append(out, toBackupDTO(b))
	}
	writeJSON(w, http.StatusOK, apitypes.BackupListDTO{Items: out})
}

func (s *server) createBackup(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.backupService(w)
	if service == nil {
		return
	}
	row, err := service.Create(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toBackupDTO(row))
}

func (s *server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.backupService(w)
	if service == nil {
		return
	}
	row, err := service.Restore(r.Context(), principal, r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrNoBackup):
		writeProblem(w, http.StatusNotFound, "backup_not_found", "no such backup")
		return
	case err != nil:
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toBackupDTO(row))
}

func (s *server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.backupService(w)
	if service == nil {
		return
	}
	err := service.Delete(r.Context(), principal, r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrNoBackup):
		writeProblem(w, http.StatusNotFound, "backup_not_found", "no such backup")
		return
	case err != nil:
		writeCapabilityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// uploadBackup accepts a node's tar.gz after ownership checks.
func (s *server) uploadBackup(w http.ResponseWriter, r *http.Request) {
	service := s.backupService(w)
	if service == nil {
		return
	}
	node, ok := s.agentNode(w, r)
	if !ok {
		return
	}
	body := http.MaxBytesReader(w, r.Body, 5<<30)
	row, err := service.Upload(r.Context(), node.ID, r.PathValue("id"), body, r.Header.Get("X-Delphi-SHA256"))
	switch {
	case errors.Is(err, store.ErrNoBackup):
		writeProblem(w, http.StatusNotFound, "backup_not_found", "no such backup for this node")
		return
	case err != nil:
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toBackupDTO(row))
}

// downloadBackup streams a decrypted snapshot to its node.
func (s *server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	service := s.backupService(w)
	if service == nil {
		return
	}
	node, ok := s.agentNode(w, r)
	if !ok {
		return
	}
	row, err := service.Store.BackupByID(r.Context(), r.PathValue("id"))
	if err != nil || row.NodeID != node.ID || row.State != "ready" {
		writeProblem(w, http.StatusNotFound, "backup_not_found", "no such backup for this node")
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("X-Delphi-SHA256", row.SHA256)
	w.WriteHeader(http.StatusOK)
	if err := service.Download(node.ID, row.ID, w); err != nil {
		slog.Default().Error("backup download failed", "backup", row.ID, "err", err)
	}
}

// agentNode authenticates a request by an enrolled, non-revoked node cert.
func (s *server) agentNode(w http.ResponseWriter, r *http.Request) (store.Node, bool) {
	if s.opts.Nodes == nil {
		writeProblem(w, http.StatusNotImplemented, "nodes_disabled", "the node registry is not configured")
		return store.Node{}, false
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 {
		writeProblem(w, http.StatusForbidden, "client_cert_required", "a node certificate is required")
		return store.Node{}, false
	}
	leaf := r.TLS.PeerCertificates[0]
	node, err := s.opts.Nodes.NodeBySerial(r.Context(), leaf.SerialNumber.String())
	if err != nil || node.RevokedAt != nil {
		writeProblem(w, http.StatusForbidden, "node_unknown", "unknown or revoked node certificate")
		return store.Node{}, false
	}
	return node, true
}
