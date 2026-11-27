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
	Executor  ActionExecutor
	Approvals ApprovalStore
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
	Approval       *ApprovalInfo
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

	return s.runLoop(ctx, p, conv, turn, stream)
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

// runLoop drives provider steps until a final answer or an approval pause.
func (s *Service) runLoop(ctx context.Context, p capability.Principal, conv store.Conversation, turn []Message, stream Stream) (Result, error) {
	tools := s.toolsFor(p)
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
		writeCalls := make([]ProposedCall, 0)
		for _, call := range calls {
			tool, ok := findTool(tools, call.Name)
			if !ok {
				text := fmt.Sprintf("tool error: unknown tool %q", call.Name)
				if stream.OnTool != nil {
					stream.OnTool(call.Name, "error", text)
				}
				if _, err := s.Store.AppendMessage(ctx, conv.ID, "tool", text, nil, call.ID); err != nil {
					return result, err
				}
				turn = append(turn, Message{Role: "tool", ToolCallID: call.ID, Content: text})
				continue
			}
			if tool.Write {
				if s.Executor == nil || s.Approvals == nil {
					text := "tool error: write actions are unavailable"
					if _, err := s.Store.AppendMessage(ctx, conv.ID, "tool", text, nil, call.ID); err != nil {
						return result, err
					}
					turn = append(turn, Message{Role: "tool", ToolCallID: call.ID, Content: text})
					continue
				}
				actions, err := s.Executor.Preview(ctx, p, call.Name, json.RawMessage(call.Arguments))
				if err != nil {
					text := "tool error: " + err.Error()
					if stream.OnTool != nil {
						stream.OnTool(call.Name, "error", err.Error())
					}
					if _, err := s.Store.AppendMessage(ctx, conv.ID, "tool", text, nil, call.ID); err != nil {
						return result, err
					}
					turn = append(turn, Message{Role: "tool", ToolCallID: call.ID, Content: text})
					continue
				}
				summary := actionsSummary(call.Name, actions)
				used[call.Name] = true
				writeCalls = append(writeCalls, ProposedCall{
					CallID: call.ID, Tool: call.Name, Summary: summary,
					Arguments: json.RawMessage(call.Arguments), Actions: actions,
				})
				if stream.OnTool != nil {
					stream.OnTool(call.Name, "proposed", summary)
				}
				continue
			}
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
		if len(writeCalls) > 0 {
			rawPlan, err := json.Marshal(Plan{Calls: writeCalls})
			if err != nil {
				return result, err
			}
			approval, err := s.Approvals.CreateApproval(ctx, p.OrgID, p.UserID, conv.ID, rawPlan)
			if err != nil {
				return result, err
			}
			result.Text = lastText
			result.ToolsUsed = toolNames(used)
			result.Approval = &ApprovalInfo{
				ID: approval.ID, Status: approval.Status,
				Calls: writeCalls, CreatedAt: approval.CreatedAt,
			}
			return result, nil
		}
	}
	result.Text = lastText
	result.ChatOnly = len(used) == 0
	result.ToolsUsed = toolNames(used)
	return result, ErrTooManySteps
}

func (s *Service) toolsFor(p capability.Principal) []Tool {
	tools := s.Tools.Tools(p)
	if s.Executor != nil && p.Allows("org:"+p.OrgID+":read") {
		tools = append(tools, writeTools()...)
	}
	return tools
}

// Pending returns the conversation's undecided plans.
func (s *Service) Pending(ctx context.Context, p capability.Principal, conversationID string) ([]ApprovalInfo, error) {
	if s.Approvals == nil {
		return nil, nil
	}
	rows, err := s.Approvals.PendingApprovals(ctx, p.OrgID, p.UserID, conversationID)
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalInfo, 0, len(rows))
	for _, row := range rows {
		var plan Plan
		if json.Unmarshal(row.Plan, &plan) != nil {
			continue
		}
		out = append(out, ApprovalInfo{ID: row.ID, Status: row.Status, Calls: plan.Calls, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

// Approve executes or rejects a pending plan, then resumes the model with the
// results. Execution runs as the requesting user with their current scopes.
func (s *Service) Approve(ctx context.Context, p capability.Principal, approvalID string, approve bool, stream Stream) (Result, error) {
	if s.Approvals == nil || s.Executor == nil {
		return Result{}, fmt.Errorf("approvals are unavailable")
	}
	row, err := s.Approvals.Approval(ctx, p.OrgID, p.UserID, approvalID)
	if err != nil {
		return Result{}, err
	}
	if row.Status != "pending" {
		return Result{}, fmt.Errorf("approval is already %s", row.Status)
	}
	var plan Plan
	if err := json.Unmarshal(row.Plan, &plan); err != nil {
		return Result{}, err
	}
	results := make([]map[string]string, 0, len(plan.Calls))
	for _, call := range plan.Calls {
		if !approve {
			text := "The user rejected this plan. Do not retry it unless they ask again."
			if _, err := s.Store.AppendMessage(ctx, row.ConversationID, "tool", text, nil, call.CallID); err != nil {
				return Result{}, err
			}
			results = append(results, map[string]string{"call_id": call.CallID, "result": "rejected"})
			continue
		}
		lines := make([]string, 0, len(call.Actions))
		for _, action := range call.Actions {
			out, execErr := s.Executor.Execute(ctx, p, action)
			if execErr != nil {
				out = "failed: " + execErr.Error()
			}
			lines = append(lines, action.Summary+" → "+out)
			result := "ok"
			if execErr != nil {
				result = "error: " + execErr.Error()
			}
			results = append(results, map[string]string{
				"call_id": call.CallID, "action": action.Summary, "result": result,
			})
		}
		text := strings.Join(lines, "\n")
		if !approve {
			text = "The user rejected this plan; nothing ran.\n" + text
		} else {
			text = "The user approved this plan. Executed actions:\n" + text
		}
		if _, err := s.Store.AppendMessage(ctx, row.ConversationID, "tool", text, nil, call.CallID); err != nil {
			return Result{}, err
		}
	}
	if raw, err := json.Marshal(results); err == nil {
		_ = s.Approvals.SetApprovalResults(ctx, row.ID, raw)
	}
	status := "executed"
	if !approve {
		status = "rejected"
	}
	if err := s.Approvals.SetApprovalStatus(ctx, row.ID, status); err != nil {
		return Result{}, err
	}
	conv, err := s.Store.Conversation(ctx, p.OrgID, p.UserID, row.ConversationID)
	if err != nil {
		return Result{}, err
	}
	stored, err := s.Store.Messages(ctx, row.ConversationID)
	if err != nil {
		return Result{}, err
	}
	turn := toMessages(stored)
	if len(turn) > 60 {
		turn = turn[len(turn)-60:]
	}
	resumed, err := s.runLoop(ctx, p, conv, turn, stream)
	if err == nil && resumed.Approval == nil {
		// A resumed turn is grounded in the executed actions even when the
		// model only writes a summary and calls no further tools.
		resumed.ChatOnly = false
	}
	return resumed, err
}
