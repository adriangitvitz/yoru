package interpreter

import "strings"

// Environment is a scoped variable store with parent chain for lexical scoping.
type Environment struct {
	store map[string]Value
	outer *Environment
}

// NewEnvironment creates a new top-level environment.
func NewEnvironment() *Environment {
	return &Environment{store: make(map[string]Value)}
}

// NewEnclosedEnvironment creates a child scope.
func NewEnclosedEnvironment(outer *Environment) *Environment {
	return &Environment{store: make(map[string]Value), outer: outer}
}

// Get looks up a variable, walking the scope chain.
func (e *Environment) Get(name string) (Value, bool) {
	val, ok := e.store[name]
	if !ok && e.outer != nil {
		return e.outer.Get(name)
	}
	return val, ok
}

// Set creates a new binding in the current scope.
func (e *Environment) Set(name string, val Value) Value {
	e.store[name] = val
	return val
}

// Update modifies an existing binding anywhere in the scope chain.
// Returns true if the binding was found and updated.
func (e *Environment) Update(name string, val Value) bool {
	if _, ok := e.store[name]; ok {
		e.store[name] = val
		return true
	}
	if e.outer != nil {
		return e.outer.Update(name, val)
	}
	return false
}

// Exports returns all non-private (not _-prefixed) bindings in the current scope.
func (e *Environment) Exports() map[string]Value {
	result := make(map[string]Value)
	for name, val := range e.store {
		if !strings.HasPrefix(name, "_") {
			result[name] = val
		}
	}
	return result
}

// ExportsFiltered returns only bindings whose names are in the given set.
func (e *Environment) ExportsFiltered(names map[string]bool) map[string]Value {
	result := make(map[string]Value)
	for name, val := range e.store {
		if names[name] {
			result[name] = val
		}
	}
	return result
}
