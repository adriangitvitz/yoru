package agent

import (
	"github.com/adriangitvitz/yoru/tool"
)

// LLMClient abstracts communication with an LLM provider.
type LLMClient interface {
	// Complete sends a completion request and returns the response.
	Complete(req CompletionRequest) (CompletionResponse, error)
}

// CompletionRequest is sent to the LLM.
type CompletionRequest struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []*tool.ToolSchema
	MaxTokens int
}

// Message is a conversation message (user, assistant, or tool result).
type Message struct {
	Role    string         // "user", "assistant"
	Content []ContentBlock // text or tool_use or tool_result blocks
}

// ContentBlock is one piece of content in a message.
type ContentBlock struct {
	Type string // "text", "tool_use", "tool_result"

	// For type="text"
	Text string

	// For type="tool_use"
	ToolUseID string
	ToolName  string
	Input     string // JSON string

	// For type="tool_result"
	ToolResultID string
	Content      string
	IsError      bool
}

// CompletionResponse is returned by the LLM.
type CompletionResponse struct {
	Content    []ContentBlock
	StopReason string // "end_turn", "tool_use", "max_tokens"
}

// AgentConfig configures an agent. When OutputSchema is set, the run loop
// asks for JSON matching the schema, validates it, and retries up to
// RetryInvalidOutput times on validation failure before returning
// `agent_output_invalid`.
type AgentConfig struct {
	Model              string
	System             string
	Tools              []*tool.ToolSchema
	MaxTurns           int
	BudgetTokens       int
	Temperature        float64
	OutputSchema       *OutputSchema
	RetryInvalidOutput int // 0 = use default (2); negative disables retries
}

// OutputSchema describes the structured response shape an agent must produce.
// Distinct from tool.OutputSchema so the agent surface can evolve independently.
type OutputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]OutputProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

// OutputProperty is one field in an OutputSchema.
type OutputProperty struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// Agent is an LLM-backed reasoning loop that can call tools.
type Agent struct {
	Config   AgentConfig
	Client   LLMClient
	Registry *tool.Registry
}

// NewAgent creates a new agent with the given config and client.
func NewAgent(config AgentConfig, client LLMClient, registry *tool.Registry) *Agent {
	return &Agent{
		Config:   config,
		Client:   client,
		Registry: registry,
	}
}
