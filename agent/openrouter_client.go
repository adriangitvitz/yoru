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

type OpenRouterClient struct {
	apiKey     string
	httpClient *http.Client
	Referer    string
	Title      string
}

func NewOpenRouterClient() *OpenRouterClient {
	return &OpenRouterClient{
		apiKey:     os.Getenv("OPENROUTER_API_KEY"),
		httpClient: http.DefaultClient,
		Referer:    "https://github.com/adriangitvitz/yoru",
		Title:      "Yoru",
	}
}

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
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string               `json:"type"`
	Function openAIToolDefinition `json:"function"`
}

type openAIToolDefinition struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  *tool.InputSchema `json:"parameters"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
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

	stop := "end_turn"
	switch choice.FinishReason {
	case "tool_calls":
		stop = "tool_use"
	case "length":
		stop = "max_tokens"
	}

	return CompletionResponse{
		Content:    blocks,
		StopReason: stop,
		Usage: TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}
