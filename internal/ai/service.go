package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

// ConversationStore is the persistence the assistant needs.
type ConversationStore interface {
	CreateConversation(ctx context.Context, orgID, userID, title string) (store.Conversation, error)
	Conversation(ctx context.Context, orgID, userID, id string) (store.Conversation, error)
	ListConversations(ctx context.Context, orgID, userID string, limit int) ([]store.Conversation, error)
	Messages(ctx context.Context, conversationID string) ([]store.StoredMessage, error)
	AppendMessage(ctx context.Context, conversationID, role, content string, toolCalls json.RawMessage, toolCallID string) (store.StoredMessage, error)
	RenameConversation(ctx context.Context, conversationID, title string) error
}

// Service runs the tool loop and persists the thread.
type Service struct {
	Store     ConversationStore
	Provider  Provider
	Model     string
	Tools     *Toolbox
	Assembler Assembler
	MaxSteps  int
}

// Stream receives progress while the answer is produced.
type Stream struct {
	OnDelta    func(text string)
	OnTool     func(name, status, summary string)
	OnCitation func(citation Citation)
}

// Result is the finished turn.
type Result struct {
	ConversationID string
	Text           string
	Citations      []Citation
	ToolsUsed      []string
	ChatOnly       bool
}

const maxUserChars = 8000

// List returns the user's recent conversations.
func (s *Service) List(ctx context.Context, p capability.Principal, limit int) ([]store.Conversation, error) {
	return s.Store.ListConversations(ctx, p.OrgID, p.UserID, limit)
}

// History returns one owned conversation and its messages.
func (s *Service) History(ctx context.Context, p capability.Principal, id string) (store.Conversation, []store.StoredMessage, error) {
	conv, err := s.Store.Conversation(ctx, p.OrgID, p.UserID, id)
	if err != nil {
		return store.Conversation{}, nil, err
	}
	messages, err := s.Store.Messages(ctx, id)
	return conv, messages, err
}

// Chat answers one user turn, calling tools as the model asks.
func (s *Service) Chat(ctx context.Context, p capability.Principal, conversationID, text string, stream Stream) (Result, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxUserChars {
		return Result{}, fmt.Errorf("message must be 1-%d characters", maxUserChars)
	}
	if s.Provider == nil {
		return Result{}, fmt.Errorf("no model provider is configured")
	}
	conv, err := s.conversation(ctx, p, conversationID, text)
	if err != nil {
		return Result{}, err
	}
	if _, err := s.Store.AppendMessage(ctx, conv.ID, "user", text, nil, ""); err != nil {
		return Result{}, err
	}
	stored, err := s.Store.Messages(ctx, conv.ID)
	if err != nil {
		return Result{}, err
	}
	turn := toMessages(stored)
	if len(turn) > 60 {
		turn = turn[len(turn)-60:]
	}

	tools := s.Tools.Tools(p)
	specs := make([]ToolSpec, 0, len(tools))
	for _, tool := range tools {
		specs = append(specs, ToolSpec{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters})
	}
	assembler := s.Assembler
	if assembler.MaxChars == 0 {
		assembler = DefaultAssembler()
	}
	steps := s.MaxSteps
	if steps <= 0 {
		steps = 4
	}
	seen := map[string]bool{}
	result := Result{ConversationID: conv.ID}
	used := map[string]bool{}

	var lastText string
	degraded := false
	for step := 0; step < steps; step++ {
		assembled := assembler.Assemble(SystemPrompt, turn)
		events, err := s.Provider.Chat(ctx, ChatRequest{Messages: assembled, Tools: specs})
		if err != nil && len(specs) > 0 && toolsUnsupported(err) {
			// A model without tool support is a visible degradation, not a
			// failure: retry once without tools and mark the answer chat-only.
			degraded = true
			specs = nil
			events, err = s.Provider.Chat(ctx, ChatRequest{Messages: assembled})
		}
		if err != nil {
			return result, err
		}
		var content strings.Builder
		var calls []ToolCall
		for event := range events {
			if event.Err != nil {
				return result, event.Err
			}
			if event.Delta != "" {
				content.WriteString(event.Delta)
				if stream.OnDelta != nil {
					stream.OnDelta(event.Delta)
				}
			}
			if event.Done {
				calls = event.ToolCalls
			}
		}
		lastText = content.String()
		if len(calls) == 0 {
			if _, err := s.Store.AppendMessage(ctx, conv.ID, "assistant", lastText, nil, ""); err != nil {
				return result, err
			}
			result.Text = lastText
			result.ChatOnly = degraded || len(used) == 0
			result.ToolsUsed = toolNames(used)
			return result, nil
		}
		rawCalls, _ := json.Marshal(calls)
		if _, err := s.Store.AppendMessage(ctx, conv.ID, "assistant", lastText, rawCalls, ""); err != nil {
			return result, err
		}
		turn = append(turn, Message{Role: "assistant", Content: lastText, ToolCalls: calls})
		for _, call := range calls {
			storedResult := s.runTool(ctx, p, tools, call, stream, used)
			for _, cite := range storedResult.Citations {
				key := cite.Kind + "|" + cite.Ref
				if seen[key] {
					continue
				}
				seen[key] = true
				result.Citations = append(result.Citations, cite)
				if stream.OnCitation != nil {
					stream.OnCitation(cite)
				}
			}
			if _, err := s.Store.AppendMessage(ctx, conv.ID, "tool", storedResult.Text, nil, call.ID); err != nil {
				return result, err
			}
			turn = append(turn, Message{Role: "tool", ToolCallID: call.ID, Content: storedResult.Text})
		}
	}
	result.Text = lastText
	result.ChatOnly = len(used) == 0
	result.ToolsUsed = toolNames(used)
	return result, ErrTooManySteps
}

func (s *Service) conversation(ctx context.Context, p capability.Principal, id, text string) (store.Conversation, error) {
	if id != "" {
		return s.Store.Conversation(ctx, p.OrgID, p.UserID, id)
	}
	title := []rune(strings.TrimSpace(text))
	if len(title) > 60 {
		title = title[:60]
	}
	return s.Store.CreateConversation(ctx, p.OrgID, p.UserID, string(title))
}

func (s *Service) runTool(ctx context.Context, p capability.Principal, tools []Tool, call ToolCall, stream Stream, used map[string]bool) ToolResult {
	tool, ok := findTool(tools, call.Name)
	if !ok {
		text := fmt.Sprintf("tool error: unknown tool %q", call.Name)
		if stream.OnTool != nil {
			stream.OnTool(call.Name, "error", text)
		}
		return ToolResult{Text: text}
	}
	res, err := tool.Run(ctx, p, json.RawMessage(call.Arguments))
	if err != nil {
		text := "tool error: " + err.Error()
		if stream.OnTool != nil {
			stream.OnTool(call.Name, "error", err.Error())
		}
		return ToolResult{Text: text}
	}
	used[call.Name] = true
	if stream.OnTool != nil {
		stream.OnTool(call.Name, "ok", firstLine(res.Text))
	}
	return res
}

func findTool(tools []Tool, name string) (Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

func toolsUnsupported(err error) bool {
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "tool") {
		return false
	}
	return strings.Contains(message, "not support") || strings.Contains(message, "unsupported")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

func toolNames(used map[string]bool) []string {
	out := make([]string, 0, len(used))
	for name := range used {
		out = append(out, name)
	}
	return out
}

func toMessages(stored []store.StoredMessage) []Message {
	out := make([]Message, 0, len(stored))
	for _, m := range stored {
		msg := Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if len(m.ToolCalls) > 4 {
			var calls []ToolCall
			if json.Unmarshal(m.ToolCalls, &calls) == nil {
				msg.ToolCalls = calls
			}
		}
		out = append(out, msg)
	}
	return out
}
