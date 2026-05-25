package interpreter

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/adriangitvitz/yoru/parser"
)

// Value is the interface all runtime values implement.
type Value interface {
	Type() string
	Inspect() string
	Equals(other Value) bool
}

// ---------------------------------------------------------------------------
// Primitives
// ---------------------------------------------------------------------------

// IntVal represents a runtime integer.
type IntVal struct{ V int64 }

func (v *IntVal) Type() string    { return "Int" }
func (v *IntVal) Inspect() string { return fmt.Sprintf("%d", v.V) }
func (v *IntVal) Equals(o Value) bool {
	if ov, ok := o.(*IntVal); ok {
		return v.V == ov.V
	}
	return false
}

// FloatVal represents a runtime float.
type FloatVal struct{ V float64 }

func (v *FloatVal) Type() string    { return "Float" }
func (v *FloatVal) Inspect() string { return fmt.Sprintf("%g", v.V) }
func (v *FloatVal) Equals(o Value) bool {
	if ov, ok := o.(*FloatVal); ok {
		return v.V == ov.V
	}
	return false
}

// StringVal represents a runtime string.
type StringVal struct{ V string }

func (v *StringVal) Type() string    { return "String" }
func (v *StringVal) Inspect() string { return v.V }
func (v *StringVal) Equals(o Value) bool {
	if ov, ok := o.(*StringVal); ok {
		return v.V == ov.V
	}
	return false
}

// BoolVal represents a runtime boolean.
type BoolVal struct{ V bool }

func (v *BoolVal) Type() string    { return "Bool" }
func (v *BoolVal) Inspect() string { return fmt.Sprintf("%t", v.V) }
func (v *BoolVal) Equals(o Value) bool {
	if ov, ok := o.(*BoolVal); ok {
		return v.V == ov.V
	}
	return false
}

// NilVal represents the nil value.
type NilVal struct{}

func (v *NilVal) Type() string         { return "Nil" }
func (v *NilVal) Inspect() string      { return "nil" }
func (v *NilVal) Equals(o Value) bool  { _, ok := o.(*NilVal); return ok }

// ---------------------------------------------------------------------------
// Collections
// ---------------------------------------------------------------------------

// ListVal represents a runtime list.
type ListVal struct{ Elements []Value }

func (v *ListVal) Type() string { return "List" }
func (v *ListVal) Inspect() string {
	elems := make([]string, len(v.Elements))
	for i, e := range v.Elements {
		elems[i] = e.Inspect()
	}
	return "[" + strings.Join(elems, ", ") + "]"
}
func (v *ListVal) Equals(o Value) bool {
	ov, ok := o.(*ListVal)
	if !ok || len(v.Elements) != len(ov.Elements) {
		return false
	}
	for i, e := range v.Elements {
		if !e.Equals(ov.Elements[i]) {
			return false
		}
	}
	return true
}

// ObjectVal represents a runtime object instance.
type ObjectVal struct {
	TypeName string
	Fields   map[string]Value
}

func (v *ObjectVal) Type() string    { return v.TypeName }
func (v *ObjectVal) Inspect() string {
	pairs := make([]string, 0, len(v.Fields))
	for k, val := range v.Fields {
		pairs = append(pairs, k+": "+val.Inspect())
	}
	return v.TypeName + " { " + strings.Join(pairs, ", ") + " }"
}
func (v *ObjectVal) Equals(o Value) bool {
	ov, ok := o.(*ObjectVal)
	if !ok || v.TypeName != ov.TypeName {
		return false
	}
	for k, val := range v.Fields {
		if oval, exists := ov.Fields[k]; !exists || !val.Equals(oval) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Enum
// ---------------------------------------------------------------------------

// EnumVal represents a runtime enum variant, optionally with payload fields.
type EnumVal struct {
	TypeName string
	Variant  string
	Fields   map[string]Value
}

func (v *EnumVal) Type() string    { return v.TypeName }
func (v *EnumVal) Inspect() string {
	if len(v.Fields) == 0 {
		return v.TypeName + "." + v.Variant
	}
	pairs := make([]string, 0, len(v.Fields))
	for k, val := range v.Fields {
		pairs = append(pairs, k+": "+val.Inspect())
	}
	return v.TypeName + "." + v.Variant + "(" + strings.Join(pairs, ", ") + ")"
}
func (v *EnumVal) Equals(o Value) bool {
	ov, ok := o.(*EnumVal)
	if !ok || v.TypeName != ov.TypeName || v.Variant != ov.Variant {
		return false
	}
	for k, val := range v.Fields {
		if oval, exists := ov.Fields[k]; !exists || !val.Equals(oval) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Callables
// ---------------------------------------------------------------------------

// FunctionVal represents a user-defined function or lambda (captures its defining env).
type FunctionVal struct {
	Name    string
	Params  []parser.Param
	Body    *parser.BlockExpression
	Env     *Environment
	Effects []string
	Self    Value // bound receiver for impl methods
}

func (v *FunctionVal) Type() string    { return "Function" }
func (v *FunctionVal) Inspect() string { return "<fn " + v.Name + ">" }
func (v *FunctionVal) Equals(o Value) bool { return v == o }

// BuiltinVal represents a built-in function.
type BuiltinVal struct {
	Name string
	Fn   func(args []Value) (Value, error)
}

func (v *BuiltinVal) Type() string         { return "Builtin" }
func (v *BuiltinVal) Inspect() string      { return "<builtin " + v.Name + ">" }
func (v *BuiltinVal) Equals(o Value) bool  { return v == o }

// ---------------------------------------------------------------------------
// Actor
// ---------------------------------------------------------------------------

// ActorRef is a handle to a running actor goroutine.
type ActorRef struct {
	Name    string
	Mailbox chan ActorMessage
	Done    chan struct{}
}

func (v *ActorRef) Type() string         { return "Actor" }
func (v *ActorRef) Inspect() string      { return "<actor " + v.Name + ">" }
func (v *ActorRef) Equals(o Value) bool  { return v == o }

// ---------------------------------------------------------------------------
// Tool (first-class tool reference)
// ---------------------------------------------------------------------------

// ToolVal is a first-class reference to a `tool` declaration. The interpreter
// handle is required so .run() can resolve the body and apply capability checks.
type ToolVal struct {
	Name   string
	interp *Interpreter
}

func (v *ToolVal) Type() string         { return "Tool" }
func (v *ToolVal) Inspect() string      { return "<tool " + v.Name + ">" }
func (v *ToolVal) Equals(o Value) bool {
	other, ok := o.(*ToolVal)
	return ok && other.Name == v.Name
}

// ---------------------------------------------------------------------------
// Supervisor (Yoru-facing handle around interpreter.Supervisor)
// ---------------------------------------------------------------------------

// SupervisorVal wraps a *Supervisor so user code can hold it as a value and
// call .start() / .stop() / .children() / .add_child(name).
type SupervisorVal struct {
	Sup        *Supervisor
	ChildNames []string // names of declared children in declaration order
	interp     *Interpreter
}

func (v *SupervisorVal) Type() string         { return "Supervisor" }
func (v *SupervisorVal) Inspect() string      { return "<supervisor>" }
func (v *SupervisorVal) Equals(o Value) bool  { return v == o }

// ---------------------------------------------------------------------------
// Effect
// ---------------------------------------------------------------------------

// EffectNamespaceVal is a sentinel value representing an effect namespace (HTTP, DB, etc.).
type EffectNamespaceVal struct {
	Name string
}

func (v *EffectNamespaceVal) Type() string         { return "EffectNamespace" }
func (v *EffectNamespaceVal) Inspect() string      { return "<effect " + v.Name + ">" }
func (v *EffectNamespaceVal) Equals(o Value) bool  { return false }

// ---------------------------------------------------------------------------
// Error construction helpers
// ---------------------------------------------------------------------------

// MakeErrResult is the exported alias for stdlib providers to construct
// the canonical Result.Err shape.
func MakeErrResult(kind, message string) *EnumVal {
	return makeErrResult(kind, message)
}

// makeOkResult wraps any value as Result.Ok(value).
func makeOkResult(v Value) *EnumVal {
	return &EnumVal{
		TypeName: "Result",
		Variant:  "Ok",
		Fields:   map[string]Value{"value": v},
	}
}

// MakeOkResult is the exported alias for stdlib providers.
func MakeOkResult(v Value) *EnumVal {
	return makeOkResult(v)
}

// makeErrResult builds Result.Err(Error { kind, message }) — the canonical
// shape for runtime failures, consumed by the ? and ?? operators.
func makeErrResult(kind, message string) *EnumVal {
	errObj := &ObjectVal{
		TypeName: "Error",
		Fields: map[string]Value{
			"kind":    &StringVal{V: kind},
			"message": &StringVal{V: message},
		},
	}
	return &EnumVal{
		TypeName: "Result",
		Variant:  "Err",
		Fields:   map[string]Value{"error": errObj},
	}
}

// ---------------------------------------------------------------------------
// Internal signals
// ---------------------------------------------------------------------------

// ReturnSignal unwinds out of a function call. Not a normal value.
type ReturnSignal struct {
	Value Value
}

func (v *ReturnSignal) Type() string         { return "ReturnSignal" }
func (v *ReturnSignal) Inspect() string      { return "<return " + v.Value.Inspect() + ">" }
func (v *ReturnSignal) Equals(o Value) bool  { return false }

// BreakSignal unwinds out of the innermost enclosing loop.
type BreakSignal struct{}

func (b *BreakSignal) Type() string         { return "BreakSignal" }
func (b *BreakSignal) Inspect() string      { return "<break>" }
func (b *BreakSignal) Equals(o Value) bool  { return false }

// ContinueSignal skips to the next iteration of the innermost enclosing loop.
type ContinueSignal struct{}

func (c *ContinueSignal) Type() string         { return "ContinueSignal" }
func (c *ContinueSignal) Inspect() string      { return "<continue>" }
func (c *ContinueSignal) Equals(o Value) bool  { return false }

// ---------------------------------------------------------------------------
// HTTP Response
// ---------------------------------------------------------------------------

// ResponseVal represents an HTTP response with status code and body.
type ResponseVal struct {
	Status int
	Body   Value
}

func (v *ResponseVal) Type() string    { return "Response" }
func (v *ResponseVal) Inspect() string {
	return fmt.Sprintf("Response(%d, %s)", v.Status, v.Body.Inspect())
}
func (v *ResponseVal) Equals(o Value) bool {
	ov, ok := o.(*ResponseVal)
	if !ok {
		return false
	}
	return v.Status == ov.Status && v.Body.Equals(ov.Body)
}

// ---------------------------------------------------------------------------
// Bytes
// ---------------------------------------------------------------------------

// BytesVal represents a runtime byte array.
type BytesVal struct{ V []byte }

func (v *BytesVal) Type() string    { return "Bytes" }
func (v *BytesVal) Inspect() string { return fmt.Sprintf("Bytes(%d)", len(v.V)) }
func (v *BytesVal) Equals(o Value) bool {
	ov, ok := o.(*BytesVal)
	if !ok || len(v.V) != len(ov.V) {
		return false
	}
	for i, b := range v.V {
		if b != ov.V[i] {
			return false
		}
	}
	return true
}

// ToHex returns the hex-encoded string.
func (v *BytesVal) ToHex() string { return hex.EncodeToString(v.V) }

// ---------------------------------------------------------------------------
// Map
// ---------------------------------------------------------------------------

// MapVal represents a runtime ordered map with string keys.
type MapVal struct {
	Entries map[string]Value
	Order   []string
}

func (v *MapVal) Type() string { return "Map" }
func (v *MapVal) Inspect() string {
	pairs := make([]string, 0, len(v.Order))
	for _, k := range v.Order {
		pairs = append(pairs, k+": "+v.Entries[k].Inspect())
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}
func (v *MapVal) Equals(o Value) bool {
	ov, ok := o.(*MapVal)
	if !ok || len(v.Entries) != len(ov.Entries) {
		return false
	}
	for k, val := range v.Entries {
		if oval, exists := ov.Entries[k]; !exists || !val.Equals(oval) {
			return false
		}
	}
	return true
}
