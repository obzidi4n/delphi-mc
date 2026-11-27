package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI talks to any OpenAI-compatible /chat/completions endpoint. M6 sends
// one completion per step and emits it as a single Event; the channel API can
// carry token deltas later without changing callers.
type OpenAI struct {
	BaseURL     string
	APIKey      string
	Model       string
	HTTP        *http.Client
	Temperature float64
}

// NewOpenAI returns a provider rooted at baseURL (e.g. http://127.0.0.1:11434/v1).
func NewOpenAI(baseURL, apiKey, model string) *OpenAI {
	return &OpenAI{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 3 * time.Minute},
	}
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireToolSpec `json:"function"`
}

type wireToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatPayload struct {
	Model       string        `json:"model"`
	Messages    []wireMessage `json:"messages"`
	Tools       []wireTool    `json:"tools,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type responseMessage struct {
	Content   string `json:"content"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type completionResponse struct {
	Choices []struct {
		Message      responseMessage `json:"message"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Chat sends one completion and emits it as a single Done event.
func (o *OpenAI) Chat(ctx context.Context, req ChatRequest) (<-chan Event, error) {
	messages := make([]wireMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		wm := wireMessage{Role: m.Role, Content: m.Content, Name: m.Name, ToolCallID: m.ToolCallID}
		for _, call := range m.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID: call.ID, Type: "function",
				Function: wireFunction{Name: call.Name, Arguments: call.Arguments},
			})
		}
		messages = append(messages, wm)
	}
	payload := chatPayload{
		Model: o.Model, Messages: messages, Temperature: req.Temperature,
	}
	if payload.Temperature == 0 {
		payload.Temperature = o.Temperature
	}
	for _, spec := range req.Tools {
		payload.Tools = append(payload.Tools, wireTool{
			Type:     "function",
			Function: wireToolSpec{Name: spec.Name, Description: spec.Description, Parameters: spec.Parameters},
		})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	client := o.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var completion completionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&completion); err != nil {
		return nil, fmt.Errorf("decode completion: %w", err)
	}
	event := Event{Done: true}
	if completion.Usage != nil {
		event.Usage = &Usage{PromptTokens: completion.Usage.PromptTokens, CompletionTokens: completion.Usage.CompletionTokens}
	}
	if len(completion.Choices) > 0 {
		message := completion.Choices[0].Message
		event.Delta = message.Content
		for _, call := range message.ToolCalls {
			event.ToolCalls = append(event.ToolCalls, ToolCall{
				ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments,
			})
		}
	}
	ch := make(chan Event, 1)
	ch <- event
	close(ch)
	return ch, nil
}
