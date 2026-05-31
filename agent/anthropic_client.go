package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/adriangitvitz/yoru/tool"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"

// AnthropicClient implements LLMClient using the Anthropic Messages API via net/http.
type AnthropicClient struct {
	apiKey     string
	httpClient *http.Client
}

// NewAnthropicClient creates a client that reads ANTHROPIC_API_KEY from the environment.
func NewAnthropicClient() *AnthropicClient {
	return &AnthropicClient{
		apiKey:     os.Getenv("ANTHROPIC_API_KEY"),
		httpClient: http.DefaultClient,
	}
}

// Complete sends a completion request to the Anthropic API.
func (c *AnthropicClient) Complete(req CompletionRequest) (CompletionResponse, error) {
	apiReq := buildAPIRequest(req)

	body, err := json.Marshal(apiReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic API error: %w", err)
	}

	httpReq, err := http.NewRequest("POST", anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic API error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic API error: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic API error: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, parseAPIError(resp.StatusCode, respBody)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic API error: decoding response: %w", err)
	}

	return convertAPIResponse(apiResp), nil
}

type apiRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    []apiTextBlock     `json:"system,omitempty"`
	Messages  []apiMessage       `json:"messages"`
	Tools     []*tool.ToolSchema `json:"tools,omitempty"`
}

type apiMessage struct {
	Role    string            `json:"role"`
	Content []apiContentBlock `json:"content"`
}

type apiContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type apiTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type apiResponse struct {
	Content    []apiResponseBlock `json:"content"`
	StopReason string             `json:"stop_reason"`
}

type apiResponseBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type apiErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func buildAPIRequest(req CompletionRequest) apiRequest {
	ar := apiRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
	}

	if req.System != "" {
		ar.System = []apiTextBlock{{Type: "text", Text: req.System}}
	}

	if len(req.Tools) > 0 {
		ar.Tools = req.Tools
	}

	for _, msg := range req.Messages {
		am := apiMessage{Role: msg.Role}
		for _, b := range msg.Content {
			switch b.Type {
			case "text":
				am.Content = append(am.Content, apiContentBlock{
					Type: "text",
					Text: b.Text,
				})
			case "tool_use":
				am.Content = append(am.Content, apiContentBlock{
					Type:  "tool_use",
					ID:    b.ToolUseID,
					Name:  b.ToolName,
					Input: json.RawMessage(b.Input),
				})
			case "tool_result":
				am.Content = append(am.Content, apiContentBlock{
					Type:      "tool_result",
					ToolUseID: b.ToolResultID,
					Content:   b.Content,
					IsError:   b.IsError,
				})
			}
		}
		ar.Messages = append(ar.Messages, am)
	}

	return ar
}

func convertAPIResponse(resp apiResponse) CompletionResponse {
	var blocks []ContentBlock
	for _, b := range resp.Content {
		switch b.Type {
		case "text":
			blocks = append(blocks, ContentBlock{Type: "text", Text: b.Text})
		case "tool_use":
			blocks = append(blocks, ContentBlock{
				Type:      "tool_use",
				ToolUseID: b.ID,
				ToolName:  b.Name,
				Input:     string(b.Input),
			})
		}
	}
	return CompletionResponse{
		Content:    blocks,
		StopReason: resp.StopReason,
	}
}

func parseAPIError(statusCode int, body []byte) error {
	var errResp apiErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return fmt.Errorf("anthropic API error: %d %s: %s", statusCode, errResp.Error.Type, errResp.Error.Message)
	}
	return fmt.Errorf("anthropic API error: %d: %s", statusCode, string(body))
}
