// Package ai implements the DelphiMC assistant: an OpenAI-compatible streaming
// provider, the documentation retrieval index, read-only tools, context
// assembly, and the conversation loop.
package ai

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrTooManySteps reports a model that kept calling tools past the limit.
var ErrTooManySteps = errors.New("the model exceeded the tool-step limit")

// Message is one provider message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is one requested function call.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSpec describes a tool to the model.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Usage is provider-reported token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// ChatRequest is one streaming completion request.
type ChatRequest struct {
	Messages    []Message
	Tools       []ToolSpec
	Temperature float64
}

// Event is one provider stream event. Text deltas arrive as they do; tool calls
// and usage arrive with Done.
type Event struct {
	Delta     string
	ToolCalls []ToolCall
	Usage     *Usage
	Done      bool
	Err       error
}

// Provider streams one completion.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (<-chan Event, error)
}
