package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/adriangitvitz/yoru/tool"
)

const protocolVersion = "2024-11-05"

// MCPResourceEntry holds a static resource registered with the server.
type MCPResourceEntry struct {
	URI      string
	Name     string
	Content  string
	MimeType string
}

// ServerConfig holds the configuration for an MCP server.
type ServerConfig struct {
	Name      string
	Version   string
	Tools     *tool.Registry
	Resources []MCPResourceEntry
	Auth      string // "api_key", "jwt", ""
}

// Server implements the MCP protocol (JSON-RPC 2.0 over stdio).
type Server struct {
	config      ServerConfig
	initialized bool
	apiKey      string // set via SetAPIKey for api_key auth mode
}

// NewServer creates a new MCP server from config.
func NewServer(config ServerConfig) *Server {
	return &Server{config: config}
}

// SetAPIKey sets the API key for authentication.
func (s *Server) SetAPIKey(key string) {
	s.apiKey = key
}

// HandleRequest dispatches a JSON-RPC request; returns nil for notifications.
func (s *Server) HandleRequest(req *Request) *Response {
	if req.JSONRPC != "2.0" {
		return s.errorResponse(req.ID, CodeInvalidRequest, "invalid jsonrpc version")
	}

	if req.Method == "" {
		return s.errorResponse(req.ID, CodeInvalidRequest, "missing method")
	}

	// Skip auth on initialize/notifications so the handshake can establish
	// capabilities before the client knows to send a token.
	if s.config.Auth == "api_key" && s.apiKey != "" {
		if req.Method != "initialize" && !strings.HasPrefix(req.Method, "notifications/") {
			if req.AuthToken != s.apiKey {
				return s.errorResponse(req.ID, CodeInvalidRequest, "authentication required")
			}
		}
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return nil
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	default:
		return s.errorResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func (s *Server) handleInitialize(req *Request) *Response {
	s.initialized = true

	caps := ServerCaps{Tools: &ToolsCap{}}
	if len(s.config.Resources) > 0 {
		caps.Resources = &ResourcesCap{}
	}

	result := InitializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities:    caps,
		ServerInfo: Implementation{
			Name:    s.config.Name,
			Version: s.config.Version,
		},
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) handleToolsList(req *Request) *Response {
	if !s.initialized {
		return s.errorResponse(req.ID, CodeInvalidRequest, "server not initialized")
	}

	schemas := s.config.Tools.Schemas()

	// Sort for deterministic tools/list output (clients cache by order).
	sort.Slice(schemas, func(i, j int) bool {
		return schemas[i].Name < schemas[j].Name
	})

	tools := make([]ToolDefinition, len(schemas))
	for i, schema := range schemas {
		tools[i] = schemaToToolDef(schema)
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  ToolsListResult{Tools: tools},
	}
}

func (s *Server) handleToolsCall(req *Request) *Response {
	if !s.initialized {
		return s.errorResponse(req.ID, CodeInvalidRequest, "server not initialized")
	}

	var params ToolCallParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, CodeInvalidParams, "invalid params: "+err.Error())
		}
	}

	if params.Name == "" {
		return s.errorResponse(req.ID, CodeInvalidParams, "missing tool name")
	}

	t := s.config.Tools.Lookup(params.Name)
	if t == nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolCallResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("unknown tool: %s", params.Name)}},
				IsError: true,
			},
		}
	}

	args := params.Arguments
	if args == nil {
		args = json.RawMessage("{}")
	}

	result, err := t.InvokeFn(args)
	if err != nil {
		errMsg := err.Error()
		// MCP spec: validation failures return a result with IsError=true,
		// not a JSON-RPC error envelope.
		if strings.Contains(errMsg, "missing required") ||
			strings.Contains(errMsg, "type mismatch") {
			return &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: ToolCallResult{
					Content: []ToolContent{{Type: "text", Text: errMsg}},
					IsError: true,
				},
			}
		}
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolCallResult{
				Content: []ToolContent{{Type: "text", Text: errMsg}},
				IsError: true,
			},
		}
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: ToolCallResult{
			Content: []ToolContent{{Type: "text", Text: result}},
		},
	}
}

func (s *Server) handleResourcesList(req *Request) *Response {
	if !s.initialized {
		return s.errorResponse(req.ID, CodeInvalidRequest, "server not initialized")
	}

	resources := make([]ResourceDefinition, len(s.config.Resources))
	for i, r := range s.config.Resources {
		resources[i] = ResourceDefinition{
			URI:      r.URI,
			Name:     r.Name,
			MimeType: r.MimeType,
		}
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  ResourcesListResult{Resources: resources},
	}
}

func (s *Server) handleResourcesRead(req *Request) *Response {
	if !s.initialized {
		return s.errorResponse(req.ID, CodeInvalidRequest, "server not initialized")
	}

	var params ResourceReadParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, CodeInvalidParams, "invalid params: "+err.Error())
		}
	}

	if params.URI == "" {
		return s.errorResponse(req.ID, CodeInvalidParams, "missing resource URI")
	}

	for _, r := range s.config.Resources {
		if r.URI == params.URI {
			return &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: ResourceReadResult{
					Contents: []ResourceContent{{
						URI:      r.URI,
						MimeType: r.MimeType,
						Text:     r.Content,
					}},
				},
			}
		}
	}

	return s.errorResponse(req.ID, CodeInvalidParams, fmt.Sprintf("resource not found: %s", params.URI))
}

func (s *Server) errorResponse(id any, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}

// schemaToToolDef converts a tool.ToolSchema to a ToolDefinition,
// populating OutputSchema when the Yoru tool declared an `output { ... }`.
func schemaToToolDef(schema *tool.ToolSchema) ToolDefinition {
	def := ToolDefinition{
		Name:        schema.Name,
		Description: schema.Description,
		InputSchema: InputSchema{
			Type:       "object",
			Properties: propsToMCP(schema.InputSchema.Properties),
			Required:   schema.InputSchema.Required,
		},
	}
	if schema.OutputSchema != nil {
		def.OutputSchema = &OutputSchema{
			Type:       "object",
			Properties: propsToMCP(schema.OutputSchema.Properties),
			Required:   schema.OutputSchema.Required,
		}
	}
	return def
}

// propsToMCP flattens typed properties into MCP's loose map[string]any shape.
func propsToMCP(in map[string]tool.PropertySchema) map[string]any {
	out := make(map[string]any, len(in))
	for name, prop := range in {
		p := map[string]any{
			"type": prop.Type,
		}
		if prop.Description != "" {
			p["description"] = prop.Description
		}
		if prop.Items != nil {
			p["items"] = map[string]any{"type": prop.Items.Type}
		}
		out[name] = p
	}
	return out
}
