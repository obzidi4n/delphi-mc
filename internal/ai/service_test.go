package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

type fakeStore struct {
	conversation store.Conversation
	messages     []store.StoredMessage
}

func (f *fakeStore) CreateConversation(_ context.Context, orgID, userID, title string) (store.Conversation, error) {
	f.conversation = store.Conversation{ID: "conv_1", OrgID: orgID, UserID: userID, Title: title, CreatedAt: time.Now()}
	return f.conversation, nil
}
func (f *fakeStore) Conversation(_ context.Context, orgID, userID, id string) (store.Conversation, error) {
	if f.conversation.ID != id || f.conversation.OrgID != orgID || f.conversation.UserID != userID {
		return store.Conversation{}, store.ErrNoConversation
	}
	return f.conversation, nil
}
func (f *fakeStore) ListConversations(context.Context, string, string, int) ([]store.Conversation, error) {
	return []store.Conversation{f.conversation}, nil
}
func (f *fakeStore) Messages(context.Context, string) ([]store.StoredMessage, error) {
	return append([]store.StoredMessage(nil), f.messages...), nil
}
func (f *fakeStore) AppendMessage(_ context.Context, convID, role, content string, calls json.RawMessage, callID string) (store.StoredMessage, error) {
	msg := store.StoredMessage{ID: "m", ConversationID: convID, Role: role, Content: content, ToolCalls: calls, ToolCallID: callID, CreatedAt: time.Now()}
	f.messages = append(f.messages, msg)
	return msg, nil
}
func (f *fakeStore) RenameConversation(context.Context, string, string) error { return nil }

type scriptedProvider struct {
	responses [][]Event
	calls     int
}

func (p *scriptedProvider) Chat(context.Context, ChatRequest) (<-chan Event, error) {
	if p.calls >= len(p.responses) {
		return nil, errors.New("no scripted response")
	}
	ch := make(chan Event, len(p.responses[p.calls]))
	for _, event := range p.responses[p.calls] {
		ch <- event
	}
	close(ch)
	p.calls++
	return ch, nil
}

func testService(store ConversationStore, provider Provider, docs func(context.Context, string, int) ([]store.DocChunk, error)) *Service {
	return &Service{
		Store: store, Provider: provider, Model: "test", MaxSteps: 3,
		Assembler: Assembler{MaxChars: 10000, MaxToolChars: 4000},
		Tools:     &Toolbox{SearchDocs: docs},
	}
}

func owner() capability.Principal {
	return capability.Principal{UserID: "u_1", OrgID: "default", Via: "ui", Scopes: []string{"*"}}
}

func TestChatRunsSearchDocsAndCites(t *testing.T) {
	fs := &fakeStore{}
	provider := &scriptedProvider{responses: [][]Event{
		{{Done: true, ToolCalls: []ToolCall{{ID: "c1", Name: "search_docs", Arguments: `{"query":"lag"}`}}}},
		{{Done: true, Delta: "Check TPS first."}},
	}}
	svc := testService(fs, provider, func(context.Context, string, int) ([]store.DocChunk, error) {
		return []store.DocChunk{{Path: "docs/architecture/agent.md", Heading: "Metrics", Content: "TPS is sampled."}}, nil
	})
	var tools, cites int
	result, err := svc.Chat(context.Background(), owner(), "", "Why is Survival-1 lagging?", Stream{
		OnTool:     func(string, string, string) { tools++ },
		OnCitation: func(Citation) { cites++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Check TPS first." || result.ChatOnly || len(result.Citations) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.ToolsUsed) != 1 || result.ToolsUsed[0] != "search_docs" || tools != 1 || cites != 1 {
		t.Fatalf("tools=%v callbacks=%d cites=%d", result.ToolsUsed, tools, cites)
	}
	roles := []string{}
	for _, m := range fs.messages {
		roles = append(roles, m.Role)
	}
	want := []string{"user", "assistant", "tool", "assistant"}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v", roles)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles = %v", roles)
		}
	}
}

func TestChatWithoutToolsIsMarkedChatOnly(t *testing.T) {
	fs := &fakeStore{}
	provider := &scriptedProvider{responses: [][]Event{{{Done: true, Delta: "Plain answer."}}}}
	svc := testService(fs, provider, nil)
	result, err := svc.Chat(context.Background(), owner(), "", "hello", Stream{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ChatOnly || result.Text != "Plain answer." || len(result.Citations) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestChatStopsAtStepLimit(t *testing.T) {
	fs := &fakeStore{}
	call := Event{Done: true, ToolCalls: []ToolCall{{ID: "c", Name: "search_docs", Arguments: `{"query":"x"}`}}}
	provider := &scriptedProvider{responses: [][]Event{{call}, {call}, {call}, {call}}}
	svc := testService(fs, provider, func(context.Context, string, int) ([]store.DocChunk, error) {
		return []store.DocChunk{{Path: "a", Content: "b"}}, nil
	})
	if _, err := svc.Chat(context.Background(), owner(), "", "loop", Stream{}); !errors.Is(err, ErrTooManySteps) {
		t.Fatalf("err = %v", err)
	}
}

type recordingProvider struct {
	requests []ChatRequest
}

func (p *recordingProvider) Chat(_ context.Context, req ChatRequest) (<-chan Event, error) {
	p.requests = append(p.requests, req)
	if len(req.Tools) > 0 {
		return nil, errors.New("model does not support tools")
	}
	ch := make(chan Event, 1)
	ch <- Event{Delta: "Hello from a plain chat model.", Done: true}
	close(ch)
	return ch, nil
}

func TestChatFallsBackWhenModelRejectsTools(t *testing.T) {
	fs := &fakeStore{}
	provider := &recordingProvider{}
	svc := testService(fs, provider, nil)
	result, err := svc.Chat(context.Background(), owner(), "", "hello", Stream{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ChatOnly || result.Text != "Hello from a plain chat model." {
		t.Fatalf("result = %+v", result)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("requests = %d", len(provider.requests))
	}
	if len(provider.requests[0].Tools) == 0 || len(provider.requests[1].Tools) != 0 {
		t.Fatalf("fallback did not drop tools")
	}
}
