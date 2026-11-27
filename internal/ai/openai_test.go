package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIChatSendsToolsAndParsesCalls(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"search_docs","arguments":"{\"query\":\"lag\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
	}))
	defer srv.Close()
	client := NewOpenAI(srv.URL, "key", "test-model")
	events, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools:    []ToolSpec{{Name: "search_docs", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ev Event
	for event := range events {
		if event.Done {
			ev = event
		}
	}
	if len(ev.ToolCalls) != 1 || ev.ToolCalls[0].Name != "search_docs" || ev.ToolCalls[0].Arguments != `{"query":"lag"}` {
		t.Fatalf("calls = %+v", ev.ToolCalls)
	}
	if ev.Usage == nil || ev.Usage.PromptTokens != 10 {
		t.Fatalf("usage = %+v", ev.Usage)
	}
	if got["model"] != "test-model" {
		t.Fatalf("model = %v", got["model"])
	}
	tools, _ := got["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", got["tools"])
	}
}

func TestOpenAIChatReportsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model missing", http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := NewOpenAI(srv.URL, "", "x").Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected an error")
	}
}
