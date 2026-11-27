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
	writeJSON(w, http.StatusOK, apitypes.ConversationDetailDTO{
		Conversation: apitypes.ConversationDTO{
			ID: conv.ID, Title: conv.Title, CreatedAt: conv.CreatedAt, UpdatedAt: conv.UpdatedAt,
		},
		Messages: items,
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
	var body apitypes.ChatRequestDTO
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
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
	convID := ""
	if body.ConversationID != nil {
		convID = *body.ConversationID
	}
	stream := ai.Stream{
		OnDelta: func(text string) {
			_ = send(map[string]any{"type": "delta", "text": text})
		},
		OnTool: func(name, status, summary string) {
			_ = send(map[string]any{"type": "tool", "name": name, "status": status, "summary": summary})
		},
		OnCitation: func(c ai.Citation) {
			_ = send(map[string]any{"type": "citation", "citation": c})
		},
	}
	result, err := s.opts.AI.Chat(r.Context(), principal, convID, body.Message, stream)
	if err != nil {
		_ = send(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	_ = send(map[string]any{
		"type": "done", "conversation_id": result.ConversationID,
		"chat_only": result.ChatOnly, "tools_used": result.ToolsUsed,
		"citations": result.Citations, "text": result.Text,
	})
}
