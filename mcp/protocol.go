package mcp

import "encoding/json"

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC   string          `json:"jsonrpc"`
	ID        any             `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	AuthToken string          `json:"-"` // set by transport layer, not serialized
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no ID).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// InitializeParams is sent by the client in the initialize request.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      Implementation `json:"clientInfo"`
}

// InitializeResult is returned by the server for initialize.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    ServerCaps     `json:"capabilities"`
	ServerInfo      Implementation `json:"serverInfo"`
}

// Implementation identifies a client or server by name and version.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerCaps describes the server's capabilities.
type ServerCaps struct {
	Tools     *ToolsCap     `json:"tools,omitempty"`
	Resources *ResourcesCap `json:"resources,omitempty"`
}

// ToolsCap indicates the server supports tools.
type ToolsCap struct{}

// ToolDefinition describes a tool in tools/list. OutputSchema (optional, MCP
// 2024-11-05) is emitted when the Yoru tool has an `output { ... }` block.
type ToolDefinition struct {
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	InputSchema  InputSchema   `json:"inputSchema"`
	OutputSchema *OutputSchema `json:"outputSchema,omitempty"`
}

// InputSchema is the JSON Schema for a tool's input parameters.
type InputSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
}

// OutputSchema is the JSON Schema for a tool's structured return shape.
// Distinct from InputSchema so the two can evolve independently.
type OutputSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
}

// ToolsListResult is the result of tools/list.
type ToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
}

// ToolCallParams is the params of tools/call.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResult is the result of tools/call.
type ToolCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a content block in a tool result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ResourceDefinition describes a resource in resources/list response.
type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourcesListResult is the result of resources/list.
type ResourcesListResult struct {
	Resources []ResourceDefinition `json:"resources"`
}

// ResourceReadParams is the params of resources/read.
type ResourceReadParams struct {
	URI string `json:"uri"`
}

// ResourceReadResult is the result of resources/read.
type ResourceReadResult struct {
	Contents []ResourceContent `json:"contents"`
}

// ResourceContent is a content block in a resource read result.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// ResourcesCap indicates the server supports resources.
type ResourcesCap struct{}
