package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/nbt"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

// FileService performs node file operations and records revisions.
type FileService struct {
	store *store.DB
	hub   *Hub
	audit AuditFunc
}

// NewFileService wires the file manager.
func NewFileService(db *store.DB, hub *Hub, audit AuditFunc) *FileService {
	return &FileService{store: db, hub: hub, audit: audit}
}

func (f *FileService) call(ctx context.Context, nodeID string, req wire.FileRequest) (wire.FileResult, error) {
	data, err := f.hub.Call(ctx, nodeID, func(requestID string) any {
		req.Type = wire.TypeFileRequest
		req.RequestID = requestID
		return req
	}, 20*time.Second)
	if err != nil {
		if errors.Is(err, ErrNodeOffline) {
			return wire.FileResult{}, fmt.Errorf("the server's node is not connected")
		}
		return wire.FileResult{}, err
	}
	var result wire.FileResult
	if err := json.Unmarshal(data, &result); err != nil {
		return wire.FileResult{}, err
	}
	if result.Error != "" {
		return wire.FileResult{}, errors.New(result.Error)
	}
	return result, nil
}

// List lists a directory on the node that owns the server.
func (f *FileService) List(ctx context.Context, srv store.Server, path string) ([]wire.FileEntry, error) {
	res, err := f.call(ctx, srv.NodeID, wire.FileRequest{ServerID: srv.ID, Op: "list", Path: path})
	return res.Entries, err
}

// Read returns a file's text and hash.
func (f *FileService) Read(ctx context.Context, srv store.Server, path string) (string, string, int64, error) {
	res, err := f.call(ctx, srv.NodeID, wire.FileRequest{ServerID: srv.ID, Op: "read", Path: path})
	return res.Content, res.SHA256, res.Size, err
}

// NBT reads and decodes a file through the node.
func (f *FileService) NBT(ctx context.Context, srv store.Server, file string) (nbt.Document, error) {
	res, err := f.call(ctx, srv.NodeID, wire.FileRequest{ServerID: srv.ID, Op: "nbt", Path: file})
	if err != nil {
		return nbt.Document{}, err
	}
	if res.NBT == nil {
		return nbt.Document{}, errors.New("node returned no NBT data")
	}
	return *res.NBT, nil
}

// Download reads raw bytes through the node.
func (f *FileService) Download(ctx context.Context, srv store.Server, file string) ([]byte, error) {
	res, err := f.call(ctx, srv.NodeID, wire.FileRequest{ServerID: srv.ID, Op: "download", Path: file})
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

// ErrFileConflict reports a stale expected hash.
var ErrFileConflict = errors.New("file changed since it was read")

// Write replaces a file and records a revision.
func (f *FileService) Write(ctx context.Context, p capability.Principal, srv store.Server, path, content, expected, auditAction string) (string, int, error) {
	if expected != "" {
		_, current, _, err := f.Read(ctx, srv, path)
		if err != nil || current != expected {
			return "", 0, ErrFileConflict
		}
	}
	res, err := f.call(ctx, srv.NodeID, wire.FileRequest{
		ServerID: srv.ID, Op: "write", Path: path, Content: content,
	})
	if err != nil {
		return "", 0, err
	}
	rev, err := f.store.AddFileRevision(ctx, srv.OrgID, srv.ID, path, content, res.SHA256, actorName(p))
	if err != nil {
		return "", 0, err
	}
	if f.audit != nil {
		_ = f.audit(ctx, srv.OrgID, actorName(p), "ui", auditAction, srv.ID,
			map[string]any{"path": path, "revision": rev.Revision, "sha256": res.SHA256})
	}
	if err := f.store.MarkPendingFiles(ctx, srv.ID, path); err != nil {
		return "", 0, err
	}
	return res.SHA256, rev.Revision, nil
}

func (s *server) fileTarget(w http.ResponseWriter, r *http.Request) (capability.Principal, store.Server, bool) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return capability.Principal{}, store.Server{}, false
	}
	if s.opts.Files == nil || s.opts.Services == nil || s.opts.Services.Servers == nil {
		writeProblem(w, http.StatusNotImplemented, "files_disabled", "the file manager is not configured")
		return capability.Principal{}, store.Server{}, false
	}
	srv, err := s.opts.Services.Servers.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return capability.Principal{}, store.Server{}, false
	}
	return principal, srv, true
}

func (s *server) listFiles(w http.ResponseWriter, r *http.Request) {
	_, srv, ok := s.fileTarget(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	entries, err := s.opts.Files.List(r.Context(), srv, path)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "file_error", err.Error())
		return
	}
	items := make([]apitypes.FileEntryDTO, 0, len(entries))
	for _, entry := range entries {
		items = append(items, apitypes.FileEntryDTO{
			Name: entry.Name, Dir: entry.Dir, Size: entry.Size, ModTime: entry.ModTime,
		})
	}
	writeJSON(w, http.StatusOK, apitypes.FileListDTO{Path: path, Entries: items})
}

func (s *server) readFile(w http.ResponseWriter, r *http.Request) {
	_, srv, ok := s.fileTarget(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeProblem(w, http.StatusBadRequest, "invalid_path", "path is required")
		return
	}
	content, sha, size, err := s.opts.Files.Read(r.Context(), srv, path)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "file_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apitypes.FileContentDTO{Path: path, Content: content, SHA256: sha, Size: size})
}

func (s *server) readNBT(w http.ResponseWriter, r *http.Request) {
	_, srv, ok := s.fileTarget(w, r)
	if !ok {
		return
	}
	file := r.URL.Query().Get("path")
	if file == "" {
		writeProblem(w, http.StatusBadRequest, "invalid_path", "path is required")
		return
	}
	doc, err := s.opts.Files.NBT(r.Context(), srv, file)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "nbt_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nbtDTO(file, doc))
}

func nbtDTO(file string, doc nbt.Document) apitypes.NBTDocumentDTO {
	root := nbtNodeDTO(doc.Root)
	return apitypes.NBTDocumentDTO{
		Path: file, Compression: doc.Compression, Size: doc.Size,
		DecompressedSize: doc.DecompressedSize, Root: root,
	}
}

func nbtNodeDTO(n nbt.Node) apitypes.NBTNodeDTO {
	children := make([]apitypes.NBTNodeDTO, 0, len(n.Children))
	for _, child := range n.Children {
		children = append(children, nbtNodeDTO(child))
	}
	out := apitypes.NBTNodeDTO{
		Name: n.Name, Type: n.Type, Value: n.Value,
		Length: n.Length, Truncated: n.Truncated,
	}
	if len(children) > 0 {
		out.Children = children
	}
	return out
}

func (s *server) downloadFile(w http.ResponseWriter, r *http.Request) {
	_, user, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	_ = user
	if s.opts.Files == nil || s.opts.Services == nil || s.opts.Services.Servers == nil {
		http.Error(w, "files are not configured", http.StatusNotImplemented)
		return
	}
	principal, ok := s.principalFrom(r)
	if !ok {
		http.Redirect(w, r, s.base, http.StatusSeeOther)
		return
	}
	srv, err := s.opts.Services.Servers.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	file := r.URL.Query().Get("path")
	if file == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	data, err := s.opts.Files.Download(r.Context(), srv, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(file)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func (s *server) writeFile(w http.ResponseWriter, r *http.Request) {
	principal, srv, ok := s.fileTarget(w, r)
	if !ok {
		return
	}
	if !auth.Allows(principal.Scopes, "server:"+srv.ID+":update") {
		writeProblem(w, http.StatusForbidden, "forbidden", "missing scope server:"+srv.ID+":update")
		return
	}
	var body apitypes.FileWriteRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		writeProblem(w, http.StatusBadRequest, "invalid_path", "path is required")
		return
	}
	expected := ""
	if body.ExpectedSHA256 != nil {
		expected = *body.ExpectedSHA256
	}
	sha, revision, err := s.opts.Files.Write(r.Context(), principal, srv, body.Path, body.Content, expected, "file.write")
	if errors.Is(err, ErrFileConflict) {
		writeProblem(w, http.StatusConflict, "file_changed", "the file changed since you loaded it")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "file_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apitypes.FileWriteResultDTO{Path: body.Path, SHA256: sha, Revision: revision})
}

func (s *server) listFileRevisions(w http.ResponseWriter, r *http.Request) {
	_, srv, ok := s.fileTarget(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeProblem(w, http.StatusBadRequest, "invalid_path", "path is required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	revisions, err := s.opts.Files.store.ListFileRevisions(r.Context(), srv.OrgID, srv.ID, path, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "revision_error", err.Error())
		return
	}
	items := make([]apitypes.FileRevisionDTO, 0, len(revisions))
	for _, rev := range revisions {
		items = append(items, apitypes.FileRevisionDTO{
			Revision: rev.Revision, SHA256: rev.SHA256, Author: rev.Author, CreatedAt: rev.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, apitypes.FileRevisionListDTO{Path: path, Items: items})
}

func (s *server) getFileRevision(w http.ResponseWriter, r *http.Request) {
	_, srv, ok := s.fileTarget(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	rev, err := strconv.Atoi(r.PathValue("rev"))
	if path == "" || err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_revision", "path and a numeric revision are required")
		return
	}
	revision, err := s.opts.Files.store.FileRevisionAt(r.Context(), srv.OrgID, srv.ID, path, rev)
	if errors.Is(err, store.ErrNoRevision) {
		writeProblem(w, http.StatusNotFound, "revision_not_found", "no such revision")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "revision_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apitypes.FileContentDTO{
		Path: path, Content: revision.Content, SHA256: revision.SHA256, Size: int64(len(revision.Content)),
	})
}

func (s *server) rollbackFile(w http.ResponseWriter, r *http.Request) {
	principal, srv, ok := s.fileTarget(w, r)
	if !ok {
		return
	}
	if !auth.Allows(principal.Scopes, "server:"+srv.ID+":update") {
		writeProblem(w, http.StatusForbidden, "forbidden", "missing scope server:"+srv.ID+":update")
		return
	}
	var body apitypes.FileRollbackRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if body.Path == "" || body.Revision <= 0 {
		writeProblem(w, http.StatusBadRequest, "invalid_revision", "path and revision are required")
		return
	}
	revision, err := s.opts.Files.store.FileRevisionAt(r.Context(), srv.OrgID, srv.ID, body.Path, body.Revision)
	if errors.Is(err, store.ErrNoRevision) {
		writeProblem(w, http.StatusNotFound, "revision_not_found", "no such revision")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "revision_error", err.Error())
		return
	}
	sha, newRevision, err := s.opts.Files.Write(r.Context(), principal, srv, body.Path, revision.Content, "", "file.rollback")
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "file_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apitypes.FileWriteResultDTO{Path: body.Path, SHA256: sha, Revision: newRevision})
}

func (s *server) diffFile(w http.ResponseWriter, r *http.Request) {
	_, srv, ok := s.fileTarget(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	from, fromErr := strconv.Atoi(r.URL.Query().Get("from"))
	to, toErr := strconv.Atoi(r.URL.Query().Get("to"))
	if path == "" || fromErr != nil || toErr != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_diff", "path, from and to are required")
		return
	}
	before, err := s.opts.Files.store.FileRevisionAt(r.Context(), srv.OrgID, srv.ID, path, from)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "revision_not_found", "no such 'from' revision")
		return
	}
	after, err := s.opts.Files.store.FileRevisionAt(r.Context(), srv.OrgID, srv.ID, path, to)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "revision_not_found", "no such 'to' revision")
		return
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: difflib.SplitLines(before.Content), B: difflib.SplitLines(after.Content),
		FromFile: fmt.Sprintf("%s (r%d)", path, from), ToFile: fmt.Sprintf("%s (r%d)", path, to),
		Context: 3,
	})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "diff_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apitypes.FileDiffDTO{Path: path, From: from, To: to, Diff: diff})
}
