package agent

import (
	"github.com/adriangitvitz/yoru/tool"
)

type LLMClient interface {
	Complete(req CompletionRequest) (CompletionResponse, error)
}

type CompletionRequest struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []*tool.ToolSchema
	MaxTokens int
}

type Message struct {
	Role    string
	Content []ContentBlock
}

type ContentBlock struct {
	Type string

	Text string

	ToolUseID string
	ToolName  string
	Input     string

	ToolResultID string
	Content      string
	IsError      bool
}

type CompletionResponse struct {
	Content    []ContentBlock
	StopReason string
	Usage      TokenUsage
}

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}

// AgentConfig configures an agent.
// RetryInvalidOutput: 0 uses the default (2); negative disables retries.
// RefreshTools, when set, is invoked before each LLM request; a non-nil
// return replaces Tools for that request.
type AgentConfig struct {
	Model              string
	System             string
	Tools              []*tool.ToolSchema
	MaxTurns           int
	BudgetTokens       int
	Temperature        float64
	OutputSchema       *OutputSchema
	RetryInvalidOutput int
	RefreshTools       func() []*tool.ToolSchema
}

type OutputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]OutputProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

type OutputProperty struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type Agent struct {
	Config    AgentConfig
	Client    LLMClient
	Registry  *tool.Registry
	Debug     bool
	LastUsage TokenUsage
	LastTurns int
}

func NewAgent(config AgentConfig, client LLMClient, registry *tool.Registry) *Agent {
	return &Agent{
		Config:   config,
		Client:   client,
		Registry: registry,
	}
}
