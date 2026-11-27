package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"delphi.dev/delphi/internal/ai"
	"delphi.dev/delphi/internal/apitypes"
)

func (s *server) aiStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	model := ""
	if s.opts.AI != nil {
		model = s.opts.AI.Model
	}
	writeJSON(w, http.StatusOK, apitypes.AIStatusDTO{Enabled: s.opts.AI != nil, Model: model})
}

func (s *server) listAIConversations(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.opts.AI == nil {
		writeProblem(w, http.StatusServiceUnavailable, "ai_disabled", "no model provider is configured")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	conversations, err := s.opts.AI.List(r.Context(), principal, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "ai_store", err.Error())
		return
	}
	items := make([]apitypes.ConversationDTO, 0, len(conversations))
	for _, c := range conversations {
		items = append(items, apitypes.ConversationDTO{
			ID: c.ID, Title: c.Title, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, apitypes.ConversationListDTO{Items: items})
}

func (s *server) getAIConversation(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.opts.AI == nil {
		writeProblem(w, http.StatusServiceUnavailable, "ai_disabled", "no model provider is configured")
		return
	}
	conv, messages, err := s.opts.AI.History(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "conversation_not_found", "no such conversation")
		return
	}
	items := make([]apitypes.AIMessageDTO, 0, len(messages))
	for _, m := range messages {
		items = append(items, apitypes.AIMessageDTO{ID: m.ID, Role: m.Role, Content: m.Content, CreatedAt: m.CreatedAt})
	}
	approvals := make([]apitypes.AIApprovalDTO, 0)
	if infos, err := s.opts.AI.Pending(r.Context(), principal, conv.ID); err == nil {
		for _, info := range infos {
			approvals = append(approvals, toAIApprovalDTO(info))
		}
	}
	writeJSON(w, http.StatusOK, apitypes.ConversationDetailDTO{
		Conversation: apitypes.ConversationDTO{
			ID: conv.ID, Title: conv.Title, CreatedAt: conv.CreatedAt, UpdatedAt: conv.UpdatedAt,
		},
		Messages: items, Approvals: approvals,
	})
}

func (s *server) aiChat(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.opts.AI == nil {
		writeProblem(w, http.StatusServiceUnavailable, "ai_disabled", "no model provider is configured")
		return
	}
	if principal.UserID == "" {
		writeProblem(w, http.StatusBadRequest, "user_token_required",
			"AI chat needs a user-scoped API key or a session; the admin token has no conversation owner")
		return
	}
	var body apitypes.ChatRequestDTO
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	convID := ""
	if body.ConversationID != nil {
		convID = *body.ConversationID
	}
	s.serveAIStream(w, r, func(stream ai.Stream) (ai.Result, error) {
		return s.opts.AI.Chat(r.Context(), principal, convID, body.Message, stream)
	})
}

func (s *server) serveAIStream(w http.ResponseWriter, r *http.Request, run func(ai.Stream) (ai.Result, error)) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, http.StatusInternalServerError, "streaming_unsupported", "streaming is not available")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	stream := ai.Stream{
		OnDelta: func(text string) { _ = send(map[string]any{"type": "delta", "text": text}) },
		OnTool: func(name, status, summary string) {
			_ = send(map[string]any{"type": "tool", "name": name, "status": status, "summary": summary})
		},
		OnCitation: func(c ai.Citation) { _ = send(map[string]any{"type": "citation", "citation": c}) },
	}
	result, err := run(stream)
	if err != nil {
		_ = send(map[string]any{"type": "error", "message": err.Error(), "conversation_id": result.ConversationID})
		return
	}
	done := map[string]any{
		"type": "done", "conversation_id": result.ConversationID,
		"chat_only": result.ChatOnly, "tools_used": result.ToolsUsed,
		"citations": result.Citations, "text": result.Text,
	}
	if result.Approval != nil {
		done["approval"] = toAIApprovalDTO(*result.Approval)
	}
	_ = send(done)
}

func (s *server) approveAIPlan(w http.ResponseWriter, r *http.Request) {
	s.decideAIPlan(w, r, true)
}

func (s *server) rejectAIPlan(w http.ResponseWriter, r *http.Request) {
	s.decideAIPlan(w, r, false)
}

func (s *server) decideAIPlan(w http.ResponseWriter, r *http.Request, approve bool) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.opts.AI == nil {
		writeProblem(w, http.StatusServiceUnavailable, "ai_disabled", "no model provider is configured")
		return
	}
	s.serveAIStream(w, r, func(stream ai.Stream) (ai.Result, error) {
		return s.opts.AI.Approve(r.Context(), principal, r.PathValue("id"), approve, stream)
	})
}

func (s *server) listAIApprovals(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.opts.AI == nil {
		writeProblem(w, http.StatusServiceUnavailable, "ai_disabled", "no model provider is configured")
		return
	}
	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		writeProblem(w, http.StatusBadRequest, "invalid_request", "conversation_id is required")
		return
	}
	infos, err := s.opts.AI.Pending(r.Context(), principal, convID)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "ai_store", err.Error())
		return
	}
	items := make([]apitypes.AIApprovalDTO, 0, len(infos))
	for _, info := range infos {
		items = append(items, toAIApprovalDTO(info))
	}
	writeJSON(w, http.StatusOK, apitypes.AIApprovalListDTO{Items: items})
}

func toAIApprovalDTO(info ai.ApprovalInfo) apitypes.AIApprovalDTO {
	calls := make([]apitypes.AIProposedCallDTO, 0, len(info.Calls))
	for _, call := range info.Calls {
		actions := make([]apitypes.AIActionDTO, 0, len(call.Actions))
		for _, action := range call.Actions {
			dto := apitypes.AIActionDTO{Tool: action.Tool, Summary: action.Summary}
			setString := func(field **string, value string) {
				if value != "" {
					v := value
					*field = &v
				}
			}
			setString(&dto.ServerID, action.ServerID)
			setString(&dto.ServerName, action.ServerName)
			setString(&dto.Action, action.Action)
			setString(&dto.Command, action.Command)
			setString(&dto.Player, action.Player)
			setString(&dto.Reason, action.Reason)
			setString(&dto.TemplateID, action.TemplateID)
			setString(&dto.NewName, action.NewName)
			setString(&dto.NodeID, action.NodeID)
			setString(&dto.DesiredState, action.Desired)
			if action.MemoryMB > 0 {
				v := action.MemoryMB
				dto.MemoryMB = &v
			}
			if action.Port > 0 {
				v := action.Port
				dto.Port = &v
			}
			actions = append(actions, dto)
		}
		calls = append(calls, apitypes.AIProposedCallDTO{
			CallID: call.CallID, Tool: call.Tool, Summary: call.Summary, Actions: actions,
		})
	}
	return apitypes.AIApprovalDTO{
		ID: info.ID, Status: info.Status, Calls: calls, CreatedAt: info.CreatedAt,
	}
}
