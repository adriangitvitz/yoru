package tool

import (
	"encoding/json"
	"fmt"
	"sync"
)

// InvokeFunc is the callback the interpreter provides to execute a tool.
// It receives JSON-encoded arguments and returns a string result or error.
type InvokeFunc func(argsJSON json.RawMessage) (string, error)

// RegisteredTool holds a tool's schema and its invoke callback.
type RegisteredTool struct {
	Schema    *ToolSchema
	InvokeFn InvokeFunc
}

// Registry stores tool schemas and invoke callbacks.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*RegisteredTool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]*RegisteredTool)}
}

// Register adds a tool to the registry. Returns error if name is already taken.
func (r *Registry) Register(schema *ToolSchema, fn InvokeFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[schema.Name]; exists {
		return fmt.Errorf("tool '%s' already registered", schema.Name)
	}

	r.tools[schema.Name] = &RegisteredTool{Schema: schema, InvokeFn: fn}
	return nil
}

// Lookup returns a registered tool by name, or nil if not found.
func (r *Registry) Lookup(name string) *RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// Invoke calls a tool by name with JSON-encoded arguments.
func (r *Registry) Invoke(name string, argsJSON json.RawMessage) (string, error) {
	r.mu.RLock()
	tool, exists := r.tools[name]
	r.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("unknown tool '%s'", name)
	}

	if !json.Valid(argsJSON) {
		return "", fmt.Errorf("invalid JSON")
	}

	return tool.InvokeFn(argsJSON)
}

// Schemas returns all registered tool schemas.
func (r *Registry) Schemas() []*ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemas := make([]*ToolSchema, 0, len(r.tools))
	for _, t := range r.tools {
		schemas = append(schemas, t.Schema)
	}
	return schemas
}
