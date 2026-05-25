package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/adriangitvitz/yoru/tool"
)

const openRouterAPIURL = "https://openrouter.ai/api/v1/chat/completions"

// OpenRouterClient implements LLMClient via the OpenRouter aggregator.
type OpenRouterClient struct {
	apiKey     string
	httpClient *http.Client
	// Referer/Title are OpenRouter-recommended attribution headers; both
	// are optional but improve attribution on the OpenRouter dashboard.
	Referer string
	Title   string
}

// NewOpenRouterClient reads OPENROUTER_API_KEY from the environment.
func NewOpenRouterClient() *OpenRouterClient {
	return &OpenRouterClient{
		apiKey:     os.Getenv("OPENROUTER_API_KEY"),
		httpClient: http.DefaultClient,
		Referer:    "https://github.com/adriangitvitz/yoru",
		Title:      "Yoru",
	}
}

// Complete sends a completion request to OpenRouter, translating between
// Yoru's Anthropic-style envelope and OpenAI's chat-completions format.
func (c *OpenRouterClient) Complete(req CompletionRequest) (CompletionResponse, error) {
	apiReq := buildOpenAIRequest(req)

	body, err := json.Marshal(apiReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openrouter: encode request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", openRouterAPIURL, bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openrouter: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	if c.Referer != "" {
		httpReq.Header.Set("HTTP-Referer", c.Referer)
	}
	if c.Title != "" {
		httpReq.Header.Set("X-Title", c.Title)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openrouter: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openrouter: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("openrouter: %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp openAIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return CompletionResponse{}, fmt.Errorf("openrouter: decode response: %w; body=%s", err, string(respBody))
	}

	return convertOpenAIResponse(apiResp), nil
}

type openAIRequest struct {
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	Tools     []openAITool    `json:"tools,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"` // for role=tool
	Name       string           `json:"name,omitempty"`         // for role=tool
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"` // always "function"
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

type openAITool struct {
	Type     string               `json:"type"` // "function"
	Function openAIToolDefinition `json:"function"`
}

type openAIToolDefinition struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  *tool.InputSchema `json:"parameters"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
}

type openAIChoice struct {
	Message      openAIRespMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAIRespMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls"`
}

func buildOpenAIRequest(req CompletionRequest) openAIRequest {
	out := openAIRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
	}

	if req.System != "" {
		out.Messages = append(out.Messages, openAIMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			out.Messages = append(out.Messages, translateUserMessages(msg)...)
		case "assistant":
			out.Messages = append(out.Messages, translateAssistantMessage(msg))
		}
	}

	for _, t := range req.Tools {
		schema := t.InputSchema
		out.Tools = append(out.Tools, openAITool{
			Type: "function",
			Function: openAIToolDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  &schema,
			},
		})
	}

	return out
}

// translateUserMessages expands one Yoru user message into one or more
// OpenAI messages: each tool_result block becomes a role=tool message
// (OpenAI requires one per tool_call_id), and any text blocks coalesce
// into a single role=user message.
func translateUserMessages(msg Message) []openAIMessage {
	var out []openAIMessage
	var textParts strings.Builder

	for _, b := range msg.Content {
		switch b.Type {
		case "tool_result":
			out = append(out, openAIMessage{
				Role:       "tool",
				ToolCallID: b.ToolResultID,
				Content:    b.Content,
			})
		case "text":
			textParts.WriteString(b.Text)
		}
	}

	if textParts.String() != "" {
		out = append(out, openAIMessage{Role: "user", Content: textParts.String()})
	}
	return out
}

// translateAssistantMessage maps Yoru assistant content (text + tool_use)
// into the OpenAI assistant format (content + tool_calls).
func translateAssistantMessage(msg Message) openAIMessage {
	out := openAIMessage{Role: "assistant"}
	for _, b := range msg.Content {
		switch b.Type {
		case "text":
			out.Content += b.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, openAIToolCall{
				ID:   b.ToolUseID,
				Type: "function",
				Function: openAIToolFunction{
					Name:      b.ToolName,
					Arguments: b.Input,
				},
			})
		}
	}
	return out
}

func convertOpenAIResponse(resp openAIResponse) CompletionResponse {
	if len(resp.Choices) == 0 {
		return CompletionResponse{StopReason: "end_turn"}
	}
	choice := resp.Choices[0]

	var blocks []ContentBlock
	if choice.Message.Content != "" {
		blocks = append(blocks, ContentBlock{Type: "text", Text: choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		blocks = append(blocks, ContentBlock{
			Type:      "tool_use",
			ToolUseID: tc.ID,
			ToolName:  tc.Function.Name,
			Input:     tc.Function.Arguments,
		})
	}

	// Map OpenAI finish_reason to Yoru/Anthropic stop_reason vocabulary.
	stop := "end_turn"
	switch choice.FinishReason {
	case "tool_calls":
		stop = "tool_use"
	case "length":
		stop = "max_tokens"
	}

	return CompletionResponse{Content: blocks, StopReason: stop}
}
