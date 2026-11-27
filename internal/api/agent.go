package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/pki"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

// enrollmentTTL is the lifetime of a node certificate. Rotation arrives with
// self-update at v1.1.
const enrollmentTTL = 30 * 24 * time.Hour

func (s *server) enroll(w http.ResponseWriter, r *http.Request) {
	if s.opts.CA == nil || s.opts.Nodes == nil {
		writeProblem(w, http.StatusNotImplemented, "enrollment_disabled", "enrollment is not configured")
		return
	}

	var req apitypes.EnrollRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 || strings.ContainsAny(req.Name, "\r\n\t") {
		writeProblem(w, http.StatusBadRequest, "invalid_name", "name must be 1-64 characters on one line")
		return
	}

	certPEM, serial, notAfter, err := pki.SignCSR(s.opts.CA, []byte(req.CSR), enrollmentTTL)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_csr", err.Error())
		return
	}

	node, err := s.opts.Nodes.EnrollNode(r.Context(), req.Token, store.Node{
		ID: newNodeID(), Name: req.Name, FSCapability: "copy",
		CertSerial: serial, CertExpiresAt: notAfter,
	})
	switch {
	case errors.Is(err, store.ErrInvalidToken):
		writeProblem(w, http.StatusUnauthorized, "invalid_token", "the enrollment token is not valid")
		return
	case errors.Is(err, store.ErrTokenUsed):
		writeProblem(w, http.StatusConflict, "token_used", "the enrollment token was already used")
		return
	case errors.Is(err, store.ErrTokenExpired):
		writeProblem(w, http.StatusGone, "token_expired", "the enrollment token has expired")
		return
	case err != nil:
		writeProblem(w, http.StatusInternalServerError, "enrollment_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, apitypes.EnrollResponse{
		NodeID: node.ID, Certificate: string(certPEM), NotAfter: node.CertExpiresAt,
	})
}

func toNodeDTO(n store.Node) apitypes.NodeDTO {
	capacity := n.Capacity
	if len(capacity) == 0 {
		capacity = []byte(`{}`)
	}
	return apitypes.NodeDTO{
		ID: n.ID, Name: n.Name, Status: n.Status, AgentVersion: n.AgentVersion,
		FSCapability: n.FSCapability, Capacity: capacity,
		CertExpiresAt: n.CertExpiresAt, LastSeen: n.LastSeen, RevokedAt: n.RevokedAt,
	}
}

func (s *server) fleet(w http.ResponseWriter) bool {
	if s.opts.Services == nil || s.opts.Services.Fleet == nil {
		writeProblem(w, http.StatusNotImplemented, "fleet_disabled", "the fleet service is not configured")
		return false
	}
	return true
}

func (s *server) listNodes(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok || !s.fleet(w) {
		return
	}
	limit, after := pageParams(r)
	nodes, next, err := s.opts.Services.Fleet.ListNodes(r.Context(), principal, limit, after)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	items := make([]apitypes.NodeDTO, 0, len(nodes))
	for _, n := range nodes {
		items = append(items, toNodeDTO(n))
	}
	writeJSON(w, http.StatusOK, apitypes.NodeListDTO{Items: items, NextCursor: cursorPtr(next)})
}

func (s *server) revokeNode(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok || !s.fleet(w) {
		return
	}
	err := s.opts.Services.Fleet.RevokeNode(r.Context(), principal, r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrNoNode):
		writeProblem(w, http.StatusNotFound, "node_not_found", "no such node")
		return
	case err != nil:
		writeCapabilityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) createToken(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok || !s.fleet(w) {
		return
	}

	var body struct {
		TTLSeconds int `json:"ttl_seconds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if body.TTLSeconds == 0 {
		body.TTLSeconds = 900
	}
	if body.TTLSeconds < 60 || body.TTLSeconds > 86400 {
		writeProblem(w, http.StatusBadRequest, "invalid_ttl", "ttl_seconds must be between 60 and 86400")
		return
	}

	token, expires, err := s.opts.Services.Fleet.CreateToken(r.Context(), principal,
		time.Duration(body.TTLSeconds)*time.Second)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.EnrollmentTokenDTO{Token: token, ExpiresAt: expires})
}

func (s *server) agentSocket(w http.ResponseWriter, r *http.Request) {
	if s.opts.Nodes == nil {
		writeProblem(w, http.StatusNotImplemented, "nodes_disabled", "the node registry is not configured")
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 {
		writeProblem(w, http.StatusForbidden, "client_cert_required", "a node certificate is required")
		return
	}

	leaf := r.TLS.PeerCertificates[0]
	node, err := s.opts.Nodes.NodeBySerial(r.Context(), leaf.SerialNumber.String())
	if err != nil || node.RevokedAt != nil {
		writeProblem(w, http.StatusForbidden, "node_unknown", "unknown or revoked node certificate")
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	if err := s.writeFrame(r.Context(), conn, map[string]any{
		"type": wire.TypeHelloAck, "server_time": s.opts.Now().UTC(),
	}); err != nil {
		return
	}
	_ = s.opts.Nodes.SetNodeStatus(r.Context(), node.ID, "healthy")

	var live *sink
	if s.opts.Hub != nil {
		live = s.opts.Hub.Register(node.ID, conn)
		defer s.opts.Hub.Unregister(node.ID, live)
		// A failed initial sync is not fatal: the next mutation re-syncs.
		_ = s.opts.Hub.Sync(r.Context(), node.ID)
	}

	revoked := false
	for {
		readCtx, cancel := context.WithTimeout(r.Context(), 3*s.opts.Heartbeat)
		typ, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			break
		}
		if typ == websocket.MessageBinary {
			if s.opts.Console != nil {
				s.opts.Console.IngestFrame(data)
			}
			continue
		}

		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}

		switch envelope.Type {
		case wire.TypeHello:
			var hello struct {
				AgentVersion string          `json:"agent_version"`
				Protocol     int             `json:"protocol"`
				FSCapability string          `json:"fs_capability"`
				Capacity     json.RawMessage `json:"capacity"`
			}
			if json.Unmarshal(data, &hello) == nil {
				if hello.Protocol > 1 {
					_ = conn.Close(websocket.StatusPolicyViolation, "agent protocol is newer than this panel")
					return
				}
				if err := s.opts.Nodes.UpdateNodeHello(r.Context(), node.ID,
					hello.AgentVersion, hello.FSCapability, hello.Capacity); err != nil {
					revoked = true
					break
				}
			}
			if err := s.opts.Nodes.TouchNode(r.Context(), node.ID, s.opts.Now()); err != nil {
				revoked = true
			}
		case wire.TypeHeartbeat:
			if err := s.opts.Nodes.TouchNode(r.Context(), node.ID, s.opts.Now()); err != nil {
				revoked = errors.Is(err, store.ErrNodeRevoked)
			}
		case wire.TypeFileResult:
			var result wire.FileResult
			if json.Unmarshal(data, &result) == nil && result.RequestID != "" && s.opts.Hub != nil {
				s.opts.Hub.Resolve(result.RequestID, data)
			}
		case wire.TypeBridgeState:
			var frame wire.BridgeStateFrame
			if json.Unmarshal(data, &frame) == nil && frame.ServerID != "" && s.opts.Console != nil {
				s.opts.Console.PublishBridgeState(frame)
			}
		case wire.TypeBridgeEvent:
			var frame wire.BridgeEventFrame
			if json.Unmarshal(data, &frame) == nil && frame.ServerID != "" && s.opts.Console != nil {
				s.opts.Console.PublishBridgeEvent(frame)
			}
		case wire.TypeMetrics:
			var frame wire.MetricsFrame
			if json.Unmarshal(data, &frame) == nil && frame.ServerID != "" && s.opts.Console != nil {
				s.opts.Console.PublishStats(frame)
			}
		case wire.TypeBackupResult, wire.TypeDevCloneResult:
			var frame struct {
				RequestID string `json:"request_id"`
			}
			if json.Unmarshal(data, &frame) == nil && frame.RequestID != "" && s.opts.Hub != nil {
				s.opts.Hub.Resolve(frame.RequestID, data)
			}
		case wire.TypeServerState:
			var frame wire.ServerStateFrame
			if json.Unmarshal(data, &frame) == nil && frame.ServerID != "" &&
				s.opts.Services != nil && s.opts.Services.Servers != nil {
				_ = s.opts.Services.Servers.ReportState(r.Context(), node.ID, frame)
				if s.opts.Alerts != nil {
					s.opts.Alerts.NotifyState(frame.ServerID, frame.State)
				}
			}
		}
		if revoked {
			_ = conn.Close(websocket.StatusPolicyViolation, "node revoked")
			return
		}
	}

	_ = s.opts.Nodes.SetNodeStatus(context.Background(), node.ID, "disconnected")
}

func (s *server) writeFrame(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}

func newNodeID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic("api: crypto/rand failed: " + err.Error())
	}
	return "n_" + hex.EncodeToString(raw)
}
