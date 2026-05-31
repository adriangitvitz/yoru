package interpreter

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adriangitvitz/yoru/agent"
	"github.com/adriangitvitz/yoru/lexer"
	"github.com/adriangitvitz/yoru/parser"
	"github.com/adriangitvitz/yoru/tool"
	"github.com/adriangitvitz/yoru/typechecker"
)

// EvalResult holds the final value produced by evaluation.
type EvalResult struct {
	LastValue Value
}

// Interpreter evaluates a type-checked Yoru AST.
type Interpreter struct {
	env                *Environment
	effectStack        *EffectStack
	runtimeEffects     map[string]bool
	objectDecls        map[string]*parser.ObjectDecl
	enumDecls          map[string]*parser.EnumDecl
	actorDecls         map[string]*parser.ActorDecl
	pipelineDecls      map[string]*parser.PipelineDecl
	toolDecls          map[string]*parser.ToolDecl
	agentDecls         map[string]*parser.AgentDecl
	mcpDecls           map[string]*parser.MCPDecl
	serviceDecls       map[string]*parser.ServiceDecl
	protocolDecls      map[string]*parser.ProtocolDecl
	implDecls          []*parser.ImplDecl
	fileReader         FileReader
	baseDir            string
	moduleCache        map[string]*Module
	importStack        []string
	hasExplicitExports bool
	exportedNames      map[string]bool
	llmClient          agent.LLMClient
	capabilityStack    []string
	fsSessionStack     []*FSSession
	scriptArgs         []string
}

// SetScriptArgs records the CLI arguments that follow the script filename.
// Yoru code reads them via the `args()` builtin.
func (interp *Interpreter) SetScriptArgs(a []string) {
	interp.scriptArgs = a
}

func defaultRuntimeEffects() map[string]bool {
	return map[string]bool{
		"HTTP": true, "DB": true, "IO": true, "LLM": true, "Log": true,
		"Agent": true, "Stream": true, "Spawn": true, "Metric": true, "Clock": true,
		"Crypto": true, "Time": true, "JSON": true, "Redis": true, "Rabbit": true, "SQS": true, "Kafka": true,
		"Subprocess": true, "FS": true, "Path": true, "Fuzzy": true, "Diff": true,
	}
}

// EvalSource is the primary entry point: lex -> parse -> typecheck -> eval.
func EvalSource(src string) (*EvalResult, error) {
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parse error: %s", strings.Join(p.Errors(), "; "))
	}

	res := typechecker.CheckSource(src)
	if len(res.Errors) > 0 {
		return nil, fmt.Errorf("type error: %s", strings.Join(res.Errors, "; "))
	}

	return Eval(prog)
}

// EvalSourceInto is like EvalSource but uses an existing interpreter.
func (interp *Interpreter) EvalSourceInto(src string) (*EvalResult, error) {
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parse error: %s", strings.Join(p.Errors(), "; "))
	}

	res := typechecker.CheckSource(src)
	if len(res.Errors) > 0 {
		return nil, fmt.Errorf("type error: %s", strings.Join(res.Errors, "; "))
	}

	return interp.EvalProgram(prog)
}

// EvalSourceRaw is like EvalSource but skips typechecking.
func (interp *Interpreter) EvalSourceRaw(src string) (*EvalResult, error) {
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parse error: %s", strings.Join(p.Errors(), "; "))
	}
	return interp.EvalProgram(prog)
}

// Eval evaluates a parsed program (assumes it's already type-checked).
func Eval(prog *parser.Program) (*EvalResult, error) {
	interp := NewInterpreter()
	return interp.EvalProgram(prog)
}

// NewInterpreter creates a fresh interpreter with builtins registered.
func NewInterpreter() *Interpreter {
	interp := &Interpreter{
		env:            NewEnvironment(),
		effectStack:    NewEffectStack(),
		runtimeEffects: defaultRuntimeEffects(),
		objectDecls:    make(map[string]*parser.ObjectDecl),
		enumDecls:      make(map[string]*parser.EnumDecl),
		actorDecls:     make(map[string]*parser.ActorDecl),
		pipelineDecls:  make(map[string]*parser.PipelineDecl),
		toolDecls:      make(map[string]*parser.ToolDecl),
		agentDecls:     make(map[string]*parser.AgentDecl),
		mcpDecls:       make(map[string]*parser.MCPDecl),
		serviceDecls:   make(map[string]*parser.ServiceDecl),
		protocolDecls:  make(map[string]*parser.ProtocolDecl),
		moduleCache:    make(map[string]*Module),
		exportedNames:  make(map[string]bool),
	}
	interp.registerBuiltins()
	interp.registerBuiltinEnums()
	return interp
}

// EvalProgram evaluates a program in the interpreter's existing environment.
func (interp *Interpreter) EvalProgram(prog *parser.Program) (*EvalResult, error) {
	interp.collectDeclarations(prog)

	for _, agentDecl := range interp.agentDecls {
		for _, toolRef := range agentDecl.Tools {
			if _, exists := interp.toolDecls[toolRef]; !exists {
				return nil, fmt.Errorf("agent '%s' references undeclared tool '%s'", agentDecl.Name, toolRef)
			}
		}
	}

	for _, mcpDecl := range interp.mcpDecls {
		for _, toolRef := range mcpDecl.Tools {
			if _, exists := interp.toolDecls[toolRef]; !exists {
				return nil, fmt.Errorf("mcp '%s' references undeclared tool '%s'", mcpDecl.Name, toolRef)
			}
		}
	}

	var lastVal Value = &NilVal{}
	for _, stmt := range prog.Statements {
		val := interp.evalStatement(stmt)
		if val != nil {
			lastVal = val
		}
	}

	return &EvalResult{LastValue: lastVal}, nil
}

// Reset clears the environment and re-registers builtins.
func (interp *Interpreter) Reset() {
	interp.env = NewEnvironment()
	interp.effectStack = NewEffectStack()
	interp.runtimeEffects = defaultRuntimeEffects()
	interp.objectDecls = make(map[string]*parser.ObjectDecl)
	interp.enumDecls = make(map[string]*parser.EnumDecl)
	interp.actorDecls = make(map[string]*parser.ActorDecl)
	interp.pipelineDecls = make(map[string]*parser.PipelineDecl)
	interp.toolDecls = make(map[string]*parser.ToolDecl)
	interp.agentDecls = make(map[string]*parser.AgentDecl)
	interp.mcpDecls = make(map[string]*parser.MCPDecl)
	interp.serviceDecls = make(map[string]*parser.ServiceDecl)
	interp.protocolDecls = make(map[string]*parser.ProtocolDecl)
	interp.implDecls = nil
	interp.hasExplicitExports = false
	interp.exportedNames = make(map[string]bool)
	interp.registerBuiltins()
	interp.registerBuiltinEnums()
}

func (interp *Interpreter) collectDeclarations(prog *parser.Program) {
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.ObjectDecl:
			interp.objectDecls[s.Name] = s
		case *parser.EnumDecl:
			interp.enumDecls[s.Name] = s
		case *parser.FnDecl:
			fn := &FunctionVal{
				Name:    s.Name,
				Params:  s.Params,
				Body:    s.Body,
				Env:     interp.env,
				Effects: s.Effects,
			}
			interp.env.Set(s.Name, fn)
		case *parser.ActorDecl:
			interp.actorDecls[s.Name] = s
		case *parser.PipelineDecl:
			interp.pipelineDecls[s.Name] = s
		case *parser.ToolDecl:
			interp.toolDecls[s.Name] = s
		case *parser.AgentDecl:
			interp.agentDecls[s.Name] = s
		case *parser.MCPDecl:
			interp.mcpDecls[s.Name] = s
		case *parser.ServiceDecl:
			interp.serviceDecls[s.Name] = s
		case *parser.ProtocolDecl:
			interp.protocolDecls[s.Name] = s
		case *parser.ImplDecl:
			interp.implDecls = append(interp.implDecls, s)
		case *parser.EffectDecl:
			interp.runtimeEffects[s.Name] = true
		case *parser.ExportStatement:
			interp.hasExplicitExports = true
			if name := nameOfDecl(s.Inner); name != "" {
				interp.exportedNames[name] = true
			}
			interp.collectSingleDeclaration(s.Inner)
		case *parser.ImportStatement:
		}
	}
}

// collectSingleDeclaration processes a single statement as a declaration.
func (interp *Interpreter) collectSingleDeclaration(stmt parser.Statement) {
	switch s := stmt.(type) {
	case *parser.ObjectDecl:
		interp.objectDecls[s.Name] = s
	case *parser.EnumDecl:
		interp.enumDecls[s.Name] = s
	case *parser.FnDecl:
		fn := &FunctionVal{
			Name:    s.Name,
			Params:  s.Params,
			Body:    s.Body,
			Env:     interp.env,
			Effects: s.Effects,
		}
		interp.env.Set(s.Name, fn)
	case *parser.ActorDecl:
		interp.actorDecls[s.Name] = s
	case *parser.PipelineDecl:
		interp.pipelineDecls[s.Name] = s
	case *parser.ToolDecl:
		interp.toolDecls[s.Name] = s
	case *parser.AgentDecl:
		interp.agentDecls[s.Name] = s
	case *parser.MCPDecl:
		interp.mcpDecls[s.Name] = s
	case *parser.ServiceDecl:
		interp.serviceDecls[s.Name] = s
	case *parser.ProtocolDecl:
		interp.protocolDecls[s.Name] = s
	case *parser.ImplDecl:
		interp.implDecls = append(interp.implDecls, s)
	case *parser.EffectDecl:
		interp.runtimeEffects[s.Name] = true
	}
}

// nameOfDecl extracts the declaration name from a statement, if applicable.
func nameOfDecl(stmt parser.Statement) string {
	switch s := stmt.(type) {
	case *parser.FnDecl:
		return s.Name
	case *parser.ObjectDecl:
		return s.Name
	case *parser.EnumDecl:
		return s.Name
	case *parser.LetStatement:
		return s.Name
	case *parser.MutStatement:
		return s.Name
	case *parser.ActorDecl:
		return s.Name
	case *parser.PipelineDecl:
		return s.Name
	case *parser.ToolDecl:
		return s.Name
	case *parser.AgentDecl:
		return s.Name
	case *parser.MCPDecl:
		return s.Name
	case *parser.ServiceDecl:
		return s.Name
	case *parser.ProtocolDecl:
		return s.Name
	case *parser.ImplDecl:
		return s.Target
	case *parser.EffectDecl:
		return s.Name
	case *parser.TypeAliasDecl:
		return s.Name
	}
	return ""
}

func (interp *Interpreter) registerBuiltins() {
	interp.env.Set("print", &BuiltinVal{Name: "print", Fn: func(args []Value) (Value, error) {
		if len(args) > 0 {
			fmt.Print(args[0].Inspect())
		}
		return &NilVal{}, nil
	}})
	interp.env.Set("println", &BuiltinVal{Name: "println", Fn: func(args []Value) (Value, error) {
		if len(args) > 0 {
			fmt.Println(args[0].Inspect())
		} else {
			fmt.Println()
		}
		return &NilVal{}, nil
	}})
	interp.env.Set("len", &BuiltinVal{Name: "len", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("len() takes 1 argument, got %d", len(args))
		}
		switch v := args[0].(type) {
		case *ListVal:
			return &IntVal{V: int64(len(v.Elements))}, nil
		case *StringVal:
			return &IntVal{V: int64(len(v.V))}, nil
		case *BytesVal:
			return &IntVal{V: int64(len(v.V))}, nil
		case *MapVal:
			return &IntVal{V: int64(len(v.Entries))}, nil
		default:
			return nil, fmt.Errorf("len() not supported for %s", v.Type())
		}
	}})
	interp.env.Set("range", &BuiltinVal{Name: "range", Fn: func(args []Value) (Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("range() takes 1 or 2 arguments, got %d", len(args))
		}
		var lo, hi int64
		switch len(args) {
		case 1:
			n, ok := args[0].(*IntVal)
			if !ok {
				return nil, fmt.Errorf("range() requires Int argument")
			}
			lo, hi = 0, n.V
		case 2:
			a, ok := args[0].(*IntVal)
			if !ok {
				return nil, fmt.Errorf("range() requires Int arguments")
			}
			b, ok := args[1].(*IntVal)
			if !ok {
				return nil, fmt.Errorf("range() requires Int arguments")
			}
			lo, hi = a.V, b.V
		}
		if hi <= lo {
			return &ListVal{Elements: []Value{}}, nil
		}
		elems := make([]Value, 0, hi-lo)
		for i := lo; i < hi; i++ {
			elems = append(elems, &IntVal{V: i})
		}
		return &ListVal{Elements: elems}, nil
	}})
	interp.env.Set("assert", &BuiltinVal{Name: "assert", Fn: func(args []Value) (Value, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("assert() requires at least 1 argument")
		}
		b, ok := args[0].(*BoolVal)
		if !ok || !b.V {
			msg := "assertion failed"
			if len(args) >= 2 {
				if s, ok := args[1].(*StringVal); ok {
					msg = s.V
				}
			}
			return nil, fmt.Errorf("%s", msg)
		}
		return &NilVal{}, nil
	}})
	interp.env.Set("to_string", &BuiltinVal{Name: "to_string", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("to_string() takes 1 argument")
		}
		return &StringVal{V: args[0].Inspect()}, nil
	}})
	interp.env.Set("int", &BuiltinVal{Name: "int", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("int() takes 1 argument")
		}
		switch v := args[0].(type) {
		case *IntVal:
			return v, nil
		case *FloatVal:
			return &IntVal{V: int64(v.V)}, nil
		case *StringVal:
			var n int64
			_, err := fmt.Sscanf(v.V, "%d", &n)
			if err != nil {
				return nil, fmt.Errorf("cannot convert %q to Int", v.V)
			}
			return &IntVal{V: n}, nil
		default:
			return nil, fmt.Errorf("int() not supported for %s", v.Type())
		}
	}})
	interp.env.Set("float", &BuiltinVal{Name: "float", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("float() takes 1 argument")
		}
		switch v := args[0].(type) {
		case *FloatVal:
			return v, nil
		case *IntVal:
			return &FloatVal{V: float64(v.V)}, nil
		case *StringVal:
			var f float64
			_, err := fmt.Sscanf(v.V, "%f", &f)
			if err != nil {
				return nil, fmt.Errorf("cannot convert %q to Float", v.V)
			}
			return &FloatVal{V: f}, nil
		default:
			return nil, fmt.Errorf("float() not supported for %s", v.Type())
		}
	}})
	interp.env.Set("ok", &BuiltinVal{Name: "ok", Fn: func(args []Value) (Value, error) {
		body := Value(&NilVal{})
		if len(args) > 0 {
			body = args[0]
		}
		return &ResponseVal{Status: 200, Body: body}, nil
	}})
	interp.env.Set("created", &BuiltinVal{Name: "created", Fn: func(args []Value) (Value, error) {
		body := Value(&NilVal{})
		if len(args) > 0 {
			body = args[0]
		}
		return &ResponseVal{Status: 201, Body: body}, nil
	}})
	interp.env.Set("no_content", &BuiltinVal{Name: "no_content", Fn: func(args []Value) (Value, error) {
		return &ResponseVal{Status: 204, Body: &NilVal{}}, nil
	}})
	interp.env.Set("bad_request", &BuiltinVal{Name: "bad_request", Fn: func(args []Value) (Value, error) {
		body := Value(&StringVal{V: "bad request"})
		if len(args) > 0 {
			body = args[0]
		}
		return &ResponseVal{Status: 400, Body: body}, nil
	}})
	interp.env.Set("not_found", &BuiltinVal{Name: "not_found", Fn: func(args []Value) (Value, error) {
		body := Value(&StringVal{V: "not found"})
		if len(args) > 0 {
			body = args[0]
		}
		return &ResponseVal{Status: 404, Body: body}, nil
	}})
	interp.env.Set("internal_error", &BuiltinVal{Name: "internal_error", Fn: func(args []Value) (Value, error) {
		body := Value(&StringVal{V: "internal error"})
		if len(args) > 0 {
			body = args[0]
		}
		return &ResponseVal{Status: 500, Body: body}, nil
	}})
	interp.env.Set("unauthorized", &BuiltinVal{Name: "unauthorized", Fn: func(args []Value) (Value, error) {
		body := Value(&StringVal{V: "unauthorized"})
		if len(args) > 0 {
			body = args[0]
		}
		return &ResponseVal{Status: 401, Body: body}, nil
	}})
	interp.env.Set("forbidden", &BuiltinVal{Name: "forbidden", Fn: func(args []Value) (Value, error) {
		body := Value(&StringVal{V: "forbidden"})
		if len(args) > 0 {
			body = args[0]
		}
		return &ResponseVal{Status: 403, Body: body}, nil
	}})
	interp.env.Set("split", &BuiltinVal{Name: "split", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("split() takes 2 arguments, got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("split() first argument must be String")
		}
		sep, ok := args[1].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("split() second argument must be String")
		}
		parts := strings.Split(s.V, sep.V)
		elems := make([]Value, len(parts))
		for i, p := range parts {
			elems[i] = &StringVal{V: p}
		}
		return &ListVal{Elements: elems}, nil
	}})
	interp.env.Set("min", &BuiltinVal{Name: "min", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("min() takes 2 arguments, got %d", len(args))
		}
		a, aOk := args[0].(*IntVal)
		b, bOk := args[1].(*IntVal)
		if aOk && bOk {
			if a.V < b.V {
				return a, nil
			}
			return b, nil
		}
		return nil, fmt.Errorf("min() requires Int arguments")
	}})
	interp.env.Set("max", &BuiltinVal{Name: "max", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("max() takes 2 arguments, got %d", len(args))
		}
		a, aOk := args[0].(*IntVal)
		b, bOk := args[1].(*IntVal)
		if aOk && bOk {
			if a.V > b.V {
				return a, nil
			}
			return b, nil
		}
		return nil, fmt.Errorf("max() requires Int arguments")
	}})
	interp.env.Set("slice", &BuiltinVal{Name: "slice", Fn: func(args []Value) (Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("slice() takes 2-3 arguments, got %d", len(args))
		}
		switch v := args[0].(type) {
		case *StringVal:
			start, ok := args[1].(*IntVal)
			if !ok {
				return nil, fmt.Errorf("slice() second argument must be Int")
			}
			end := int64(len(v.V))
			if len(args) == 3 {
				if e, ok := args[2].(*IntVal); ok {
					end = e.V
				}
			}
			s := int(start.V)
			e := int(end)
			if s < 0 {
				s = 0
			}
			if e > len(v.V) {
				e = len(v.V)
			}
			if s > e {
				return &StringVal{V: ""}, nil
			}
			return &StringVal{V: v.V[s:e]}, nil
		case *ListVal:
			start, ok := args[1].(*IntVal)
			if !ok {
				return nil, fmt.Errorf("slice() second argument must be Int")
			}
			end := int64(len(v.Elements))
			if len(args) == 3 {
				if e, ok := args[2].(*IntVal); ok {
					end = e.V
				}
			}
			s := int(start.V)
			e := int(end)
			if s < 0 {
				s = 0
			}
			if e > len(v.Elements) {
				e = len(v.Elements)
			}
			if s > e {
				return &ListVal{Elements: nil}, nil
			}
			copied := make([]Value, e-s)
			copy(copied, v.Elements[s:e])
			return &ListVal{Elements: copied}, nil
		default:
			return nil, fmt.Errorf("slice() not supported for %s", v.Type())
		}
	}})
	interp.env.Set("char_at", &BuiltinVal{Name: "char_at", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("char_at() takes 2 arguments, got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("char_at() first argument must be String")
		}
		i, ok := args[1].(*IntVal)
		if !ok {
			return nil, fmt.Errorf("char_at() second argument must be Int")
		}
		if i.V >= 0 && int(i.V) < len(s.V) {
			return &StringVal{V: string(s.V[i.V])}, nil
		}
		return &NilVal{}, nil
	}})
	interp.env.Set("contains_key", &BuiltinVal{Name: "contains_key", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("contains_key() takes 2 arguments, got %d", len(args))
		}
		obj, ok := args[0].(*ObjectVal)
		if !ok {
			return nil, fmt.Errorf("contains_key() first argument must be an Object")
		}
		key, ok := args[1].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("contains_key() second argument must be String")
		}
		_, exists := obj.Fields[key.V]
		return &BoolVal{V: exists}, nil
	}})
	interp.env.Set("keys", &BuiltinVal{Name: "keys", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("keys() takes 1 argument, got %d", len(args))
		}
		obj, ok := args[0].(*ObjectVal)
		if !ok {
			return nil, fmt.Errorf("keys() argument must be an Object")
		}
		var elems []Value
		for k := range obj.Fields {
			elems = append(elems, &StringVal{V: k})
		}
		sort.Slice(elems, func(i, j int) bool {
			return elems[i].(*StringVal).V < elems[j].(*StringVal).V
		})
		return &ListVal{Elements: elems}, nil
	}})
	interp.env.Set("append", &BuiltinVal{Name: "append", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("append() takes 2 arguments, got %d", len(args))
		}
		list, ok := args[0].(*ListVal)
		if !ok {
			return nil, fmt.Errorf("append() first argument must be a List")
		}
		newElems := make([]Value, len(list.Elements)+1)
		copy(newElems, list.Elements)
		newElems[len(list.Elements)] = args[1]
		return &ListVal{Elements: newElems}, nil
	}})
	interp.env.Set("type_of", &BuiltinVal{Name: "type_of", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("type_of() takes 1 argument, got %d", len(args))
		}
		return &StringVal{V: args[0].Type()}, nil
	}})
	interp.env.Set("hash", &BuiltinVal{Name: "hash", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("hash() takes 1 argument, got %d", len(args))
		}
		s := args[0].Inspect()
		var h int64
		for _, c := range s {
			h = h*31 + int64(c)
		}
		if h < 0 {
			h = -h
		}
		return &IntVal{V: h}, nil
	}})
	interp.env.Set("abs", &BuiltinVal{Name: "abs", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("abs() takes 1 argument, got %d", len(args))
		}
		if i, ok := args[0].(*IntVal); ok {
			if i.V < 0 {
				return &IntVal{V: -i.V}, nil
			}
			return i, nil
		}
		return nil, fmt.Errorf("abs() requires Int argument")
	}})
	interp.env.Set("List", &ObjectVal{
		TypeName: "List",
		Fields: map[string]Value{
			"of": &BuiltinVal{Name: "List.of", Fn: func(args []Value) (Value, error) {
				if len(args) == 1 {
					if l, ok := args[0].(*ListVal); ok {
						return l, nil
					}
				}
				return &ListVal{Elements: args}, nil
			}},
		},
	})

	interp.env.Set("Bytes", &ObjectVal{
		TypeName: "Bytes",
		Fields: map[string]Value{
			"from": &BuiltinVal{Name: "Bytes.from", Fn: func(args []Value) (Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("Bytes.from() takes 1 argument")
				}
				s, ok := args[0].(*StringVal)
				if !ok {
					return nil, fmt.Errorf("Bytes.from() requires String argument")
				}
				return &BytesVal{V: []byte(s.V)}, nil
			}},
			"from_hex": &BuiltinVal{Name: "Bytes.from_hex", Fn: func(args []Value) (Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("Bytes.from_hex() takes 1 argument")
				}
				s, ok := args[0].(*StringVal)
				if !ok {
					return nil, fmt.Errorf("Bytes.from_hex() requires String argument")
				}
				decoded, err := hex.DecodeString(s.V)
				if err != nil {
					return nil, fmt.Errorf("invalid hex string: %s", err)
				}
				return &BytesVal{V: decoded}, nil
			}},
			"new": &BuiltinVal{Name: "Bytes.new", Fn: func(args []Value) (Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("Bytes.new() takes 1 argument")
				}
				n, ok := args[0].(*IntVal)
				if !ok {
					return nil, fmt.Errorf("Bytes.new() requires Int argument")
				}
				return &BytesVal{V: make([]byte, n.V)}, nil
			}},
		},
	})
	interp.env.Set("Collector", &ObjectVal{
		TypeName: "Collector",
		Fields: map[string]Value{
			"collect": &BuiltinVal{Name: "Collector.collect", Fn: func(args []Value) (Value, error) {
				return &BuiltinVal{Name: "collector_sink", Fn: func(items []Value) (Value, error) {
					return &ListVal{Elements: items}, nil
				}}, nil
			}},
		},
	})

	interp.env.Set("join", &BuiltinVal{Name: "join", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("join() takes 2 arguments, got %d", len(args))
		}
		list, ok := args[0].(*ListVal)
		if !ok {
			return nil, fmt.Errorf("join() first argument must be a List")
		}
		sep, ok := args[1].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("join() second argument must be String")
		}
		parts := make([]string, len(list.Elements))
		for i, e := range list.Elements {
			parts[i] = e.Inspect()
		}
		return &StringVal{V: strings.Join(parts, sep.V)}, nil
	}})
	interp.env.Set("trim", &BuiltinVal{Name: "trim", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("trim() takes 1 argument, got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("trim() requires String argument")
		}
		return &StringVal{V: strings.TrimSpace(s.V)}, nil
	}})
	interp.env.Set("replace", &BuiltinVal{Name: "replace", Fn: func(args []Value) (Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("replace() takes 3 arguments, got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("replace() first argument must be String")
		}
		old, ok := args[1].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("replace() second argument must be String")
		}
		newStr, ok := args[2].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("replace() third argument must be String")
		}
		return &StringVal{V: strings.ReplaceAll(s.V, old.V, newStr.V)}, nil
	}})
	interp.env.Set("replace_regex", &BuiltinVal{Name: "replace_regex", Fn: func(args []Value) (Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("replace_regex() takes 3 arguments (content, pattern, replacement), got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("replace_regex() first argument must be String")
		}
		pattern, ok := args[1].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("replace_regex() second argument must be String")
		}
		repl, ok := args[2].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("replace_regex() third argument must be String")
		}
		re, err := regexp.Compile(pattern.V)
		if err != nil {
			return makeErrResult("regex_invalid", err.Error()), nil
		}
		return &StringVal{V: re.ReplaceAllString(s.V, repl.V)}, nil
	}})
	interp.env.Set("contains", &BuiltinVal{Name: "contains", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("contains() takes 2 arguments, got %d", len(args))
		}
		switch v := args[0].(type) {
		case *StringVal:
			sub, ok := args[1].(*StringVal)
			if !ok {
				return nil, fmt.Errorf("contains() second argument must be String")
			}
			return &BoolVal{V: strings.Contains(v.V, sub.V)}, nil
		case *ListVal:
			for _, elem := range v.Elements {
				if elem.Equals(args[1]) {
					return &BoolVal{V: true}, nil
				}
			}
			return &BoolVal{V: false}, nil
		default:
			return nil, fmt.Errorf("contains() first argument must be String or List")
		}
	}})
	interp.env.Set("uppercase", &BuiltinVal{Name: "uppercase", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("uppercase() takes 1 argument, got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("uppercase() requires String argument")
		}
		return &StringVal{V: strings.ToUpper(s.V)}, nil
	}})
	interp.env.Set("lowercase", &BuiltinVal{Name: "lowercase", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("lowercase() takes 1 argument, got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("lowercase() requires String argument")
		}
		return &StringVal{V: strings.ToLower(s.V)}, nil
	}})
	interp.env.Set("starts_with", &BuiltinVal{Name: "starts_with", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("starts_with() takes 2 arguments, got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("starts_with() first argument must be String")
		}
		prefix, ok := args[1].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("starts_with() second argument must be String")
		}
		return &BoolVal{V: strings.HasPrefix(s.V, prefix.V)}, nil
	}})
	interp.env.Set("ends_with", &BuiltinVal{Name: "ends_with", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("ends_with() takes 2 arguments, got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("ends_with() first argument must be String")
		}
		suffix, ok := args[1].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("ends_with() second argument must be String")
		}
		return &BoolVal{V: strings.HasSuffix(s.V, suffix.V)}, nil
	}})
	interp.env.Set("index_of", &BuiltinVal{Name: "index_of", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("index_of() takes 2 arguments, got %d", len(args))
		}
		switch v := args[0].(type) {
		case *StringVal:
			sub, ok := args[1].(*StringVal)
			if !ok {
				return nil, fmt.Errorf("index_of() second argument must be String")
			}
			return &IntVal{V: int64(strings.Index(v.V, sub.V))}, nil
		case *ListVal:
			for i, elem := range v.Elements {
				if elem.Equals(args[1]) {
					return &IntVal{V: int64(i)}, nil
				}
			}
			return &IntVal{V: -1}, nil
		default:
			return nil, fmt.Errorf("index_of() first argument must be String or List")
		}
	}})
	interp.env.Set("reverse", &BuiltinVal{Name: "reverse", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("reverse() takes 1 argument, got %d", len(args))
		}
		switch v := args[0].(type) {
		case *StringVal:
			runes := []rune(v.V)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return &StringVal{V: string(runes)}, nil
		case *ListVal:
			n := len(v.Elements)
			result := make([]Value, n)
			for i, e := range v.Elements {
				result[n-1-i] = e
			}
			return &ListVal{Elements: result}, nil
		default:
			return nil, fmt.Errorf("reverse() requires String or List argument")
		}
	}})
	interp.env.Set("repeat", &BuiltinVal{Name: "repeat", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("repeat() takes 2 arguments, got %d", len(args))
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("repeat() first argument must be String")
		}
		n, ok := args[1].(*IntVal)
		if !ok {
			return nil, fmt.Errorf("repeat() second argument must be Int")
		}
		return &StringVal{V: strings.Repeat(s.V, int(n.V))}, nil
	}})
	interp.env.Set("map", &BuiltinVal{Name: "map", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("map() takes 2 arguments, got %d", len(args))
		}
		list, ok := args[0].(*ListVal)
		if !ok {
			return nil, fmt.Errorf("map() first argument must be a List")
		}
		result := make([]Value, len(list.Elements))
		for i, elem := range list.Elements {
			result[i] = interp.applyCallback(args[1], []Value{elem})
		}
		return &ListVal{Elements: result}, nil
	}})
	interp.env.Set("filter", &BuiltinVal{Name: "filter", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("filter() takes 2 arguments, got %d", len(args))
		}
		list, ok := args[0].(*ListVal)
		if !ok {
			return nil, fmt.Errorf("filter() first argument must be a List")
		}
		var result []Value
		for _, elem := range list.Elements {
			v := interp.applyCallback(args[1], []Value{elem})
			if b, ok := v.(*BoolVal); ok && b.V {
				result = append(result, elem)
			}
		}
		return &ListVal{Elements: result}, nil
	}})
	interp.env.Set("reduce", &BuiltinVal{Name: "reduce", Fn: func(args []Value) (Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("reduce() takes 3 arguments, got %d", len(args))
		}
		list, ok := args[0].(*ListVal)
		if !ok {
			return nil, fmt.Errorf("reduce() first argument must be a List")
		}
		acc := args[1]
		for _, elem := range list.Elements {
			acc = interp.applyCallback(args[2], []Value{acc, elem})
		}
		return acc, nil
	}})
	interp.env.Set("sort", &BuiltinVal{Name: "sort", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("sort() takes 1 argument, got %d", len(args))
		}
		list, ok := args[0].(*ListVal)
		if !ok {
			return nil, fmt.Errorf("sort() argument must be a List")
		}
		copied := make([]Value, len(list.Elements))
		copy(copied, list.Elements)
		sort.Slice(copied, func(i, j int) bool {
			if a, ok := copied[i].(*IntVal); ok {
				if b, ok := copied[j].(*IntVal); ok {
					return a.V < b.V
				}
			}
			if a, ok := copied[i].(*StringVal); ok {
				if b, ok := copied[j].(*StringVal); ok {
					return a.V < b.V
				}
			}
			return false
		})
		return &ListVal{Elements: copied}, nil
	}})
	interp.env.Set("find", &BuiltinVal{Name: "find", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("find() takes 2 arguments, got %d", len(args))
		}
		list, ok := args[0].(*ListVal)
		if !ok {
			return nil, fmt.Errorf("find() first argument must be a List")
		}
		for _, elem := range list.Elements {
			v := interp.applyCallback(args[1], []Value{elem})
			if b, ok := v.(*BoolVal); ok && b.V {
				return elem, nil
			}
		}
		return &NilVal{}, nil
	}})
	interp.env.Set("flatten", &BuiltinVal{Name: "flatten", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("flatten() takes 1 argument, got %d", len(args))
		}
		list, ok := args[0].(*ListVal)
		if !ok {
			return nil, fmt.Errorf("flatten() argument must be a List")
		}
		var result []Value
		for _, elem := range list.Elements {
			if inner, ok := elem.(*ListVal); ok {
				result = append(result, inner.Elements...)
			} else {
				result = append(result, elem)
			}
		}
		return &ListVal{Elements: result}, nil
	}})
	interp.env.Set("zip", &BuiltinVal{Name: "zip", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("zip() takes 2 arguments, got %d", len(args))
		}
		a, aOk := args[0].(*ListVal)
		b, bOk := args[1].(*ListVal)
		if !aOk || !bOk {
			return nil, fmt.Errorf("zip() arguments must be Lists")
		}
		minLen := min(len(b.Elements), len(a.Elements))
		result := make([]Value, minLen)
		for i := range minLen {
			result[i] = &ListVal{Elements: []Value{a.Elements[i], b.Elements[i]}}
		}
		return &ListVal{Elements: result}, nil
	}})

	interp.env.Set("floor", &BuiltinVal{Name: "floor", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("floor() takes 1 argument, got %d", len(args))
		}
		switch v := args[0].(type) {
		case *FloatVal:
			return &IntVal{V: int64(math.Floor(v.V))}, nil
		case *IntVal:
			return v, nil
		default:
			return nil, fmt.Errorf("floor() requires Float or Int argument")
		}
	}})
	interp.env.Set("ceil", &BuiltinVal{Name: "ceil", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("ceil() takes 1 argument, got %d", len(args))
		}
		switch v := args[0].(type) {
		case *FloatVal:
			return &IntVal{V: int64(math.Ceil(v.V))}, nil
		case *IntVal:
			return v, nil
		default:
			return nil, fmt.Errorf("ceil() requires Float or Int argument")
		}
	}})
	interp.env.Set("sqrt", &BuiltinVal{Name: "sqrt", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("sqrt() takes 1 argument, got %d", len(args))
		}
		switch v := args[0].(type) {
		case *FloatVal:
			return &FloatVal{V: math.Sqrt(v.V)}, nil
		case *IntVal:
			return &FloatVal{V: math.Sqrt(float64(v.V))}, nil
		default:
			return nil, fmt.Errorf("sqrt() requires numeric argument")
		}
	}})
	interp.env.Set("pow", &BuiltinVal{Name: "pow", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("pow() takes 2 arguments, got %d", len(args))
		}
		toFloat := func(v Value) (float64, bool) {
			if f, ok := v.(*FloatVal); ok {
				return f.V, true
			}
			if i, ok := v.(*IntVal); ok {
				return float64(i.V), true
			}
			return 0, false
		}
		base, ok1 := toFloat(args[0])
		exp, ok2 := toFloat(args[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("pow() requires numeric arguments")
		}
		return &FloatVal{V: math.Pow(base, exp)}, nil
	}})
	interp.env.Set("round", &BuiltinVal{Name: "round", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("round() takes 1 argument, got %d", len(args))
		}
		switch v := args[0].(type) {
		case *FloatVal:
			return &IntVal{V: int64(math.Round(v.V))}, nil
		case *IntVal:
			return v, nil
		default:
			return nil, fmt.Errorf("round() requires Float or Int argument")
		}
	}})
	interp.env.Set("Map", &ObjectVal{
		TypeName: "Map",
		Fields: map[string]Value{
			"new": &BuiltinVal{Name: "Map.new", Fn: func(args []Value) (Value, error) {
				return &MapVal{Entries: make(map[string]Value), Order: nil}, nil
			}},
			"of": &BuiltinVal{Name: "Map.of", Fn: func(args []Value) (Value, error) {
				if len(args) == 1 {
					if ov, ok := args[0].(*ObjectVal); ok {
						m := &MapVal{Entries: make(map[string]Value, len(ov.Fields)), Order: nil}
						for k, v := range ov.Fields {
							m.Order = append(m.Order, k)
							m.Entries[k] = v
						}
						return m, nil
					}
				}
				if len(args)%2 != 0 {
					return nil, fmt.Errorf("expected even number of arguments to Map.of (k1, v1, k2, v2, ...) or a single Object, got %d", len(args))
				}
				m := &MapVal{Entries: make(map[string]Value, len(args)/2), Order: nil}
				for i := 0; i < len(args); i += 2 {
					k, ok := args[i].(*StringVal)
					if !ok {
						return nil, fmt.Errorf("expected String key for Map.of, got %T at position %d", args[i], i)
					}
					if _, exists := m.Entries[k.V]; !exists {
						m.Order = append(m.Order, k.V)
					}
					m.Entries[k.V] = args[i+1]
				}
				return m, nil
			}},
		},
	})
	interp.env.Set("Math", &ObjectVal{
		TypeName: "Math",
		Fields: map[string]Value{
			"PI": &FloatVal{V: math.Pi},
			"E":  &FloatVal{V: math.E},
		},
	})
	interp.env.Set("env", &BuiltinVal{Name: "env", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("env() takes 1 argument, got %d", len(args))
		}
		key, ok := args[0].(*StringVal)
		if !ok {
			return nil, fmt.Errorf("env() requires String argument")
		}
		val := os.Getenv(key.V)
		if val == "" {
			if _, exists := os.LookupEnv(key.V); !exists {
				return &NilVal{}, nil
			}
		}
		return &StringVal{V: val}, nil
	}})
	interp.env.Set("args", &BuiltinVal{Name: "args", Fn: func(_ []Value) (Value, error) {
		elems := make([]Value, len(interp.scriptArgs))
		for i, a := range interp.scriptArgs {
			elems[i] = &StringVal{V: a}
		}
		return &ListVal{Elements: elems}, nil
	}})
	interp.env.Set("define_tool", &BuiltinVal{Name: "define_tool", Fn: func(args []Value) (Value, error) {
		if len(args) != 1 {
			return makeErrResult("define_tool_bad_args",
				fmt.Sprintf("define_tool(source) takes 1 argument, got %d", len(args))), nil
		}
		s, ok := args[0].(*StringVal)
		if !ok {
			return makeErrResult("define_tool_bad_args", "source must be a String"), nil
		}
		l := lexer.New(s.V)
		p := parser.New(l)
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			return makeErrResult("define_tool_parse_failed", strings.Join(errs, "; ")), nil
		}
		var registered []Value
		for _, stmt := range prog.Statements {
			switch d := stmt.(type) {
			case *parser.ObjectDecl:
				interp.objectDecls[d.Name] = d
			case *parser.EnumDecl:
				interp.enumDecls[d.Name] = d
			case *parser.ToolDecl:
				interp.toolDecls[d.Name] = d
				registered = append(registered, &StringVal{V: d.Name})
			}
		}
		return &ListVal{Elements: registered}, nil
	}})

	supervisorNS := &ObjectVal{
		TypeName: "Supervisor",
		Fields:   map[string]Value{},
	}
	supervisorNS.Fields["new"] = &BuiltinVal{Name: "Supervisor.new", Fn: func(args []Value) (Value, error) {
		if len(args) < 1 {
			return makeErrResult("supervisor_bad_args",
				"Supervisor.new(names, strategy, max_restarts, window_seconds) requires at least 1 argument"), nil
		}
		nameList, ok := args[0].(*ListVal)
		if !ok {
			return makeErrResult("supervisor_bad_args",
				"first argument must be a list of actor/agent name strings"), nil
		}
		strategy := OneForOne
		if len(args) >= 2 {
			s, ok := args[1].(*StringVal)
			if !ok {
				return makeErrResult("supervisor_bad_args", "strategy must be a String"), nil
			}
			switch s.V {
			case "one_for_one":
				strategy = OneForOne
			case "one_for_all":
				strategy = OneForAll
			case "rest_for_one":
				strategy = RestForOne
			default:
				return makeErrResult("supervisor_bad_args", "unknown strategy: "+s.V), nil
			}
		}
		maxRestarts := 3
		if len(args) >= 3 {
			n, ok := args[2].(*IntVal)
			if !ok {
				return makeErrResult("supervisor_bad_args", "max_restarts must be an Int"), nil
			}
			maxRestarts = int(n.V)
		}
		windowSeconds := 60
		if len(args) >= 4 {
			n, ok := args[3].(*IntVal)
			if !ok {
				return makeErrResult("supervisor_bad_args", "window_seconds must be an Int"), nil
			}
			windowSeconds = int(n.V)
		}

		specs := make([]ChildSpec, 0, len(nameList.Elements))
		childNames := make([]string, 0, len(nameList.Elements))
		for _, elem := range nameList.Elements {
			name, ok := elem.(*StringVal)
			if !ok {
				return makeErrResult("supervisor_bad_args", "child names must be Strings"), nil
			}
			if decl, exists := interp.actorDecls[name.V]; exists {
				specs = append(specs, ChildSpec{Name: name.V, ActorDecl: decl})
			} else if decl, exists := interp.agentDecls[name.V]; exists {
				specs = append(specs, ChildSpec{Name: name.V, AgentDecl: decl})
			} else {
				return makeErrResult("supervisor_bad_args",
					"unknown actor or agent: "+name.V), nil
			}
			childNames = append(childNames, name.V)
		}

		sup := NewSupervisor(interp, SupervisorConfig{
			Strategy:      strategy,
			MaxRestarts:   maxRestarts,
			WindowSeconds: windowSeconds,
		}, specs)
		return &SupervisorVal{Sup: sup, ChildNames: childNames, interp: interp}, nil
	}}
	interp.env.Set("Supervisor", supervisorNS)

	interp.env.Set("supervise_agents", &BuiltinVal{Name: "supervise_agents", Fn: func(args []Value) (Value, error) {
		if len(args) < 1 {
			return makeErrResult("supervisor_bad_args", "supervise_agents(names, strategy, max_restarts, window_seconds) requires at least 1 argument"), nil
		}
		nameList, ok := args[0].(*ListVal)
		if !ok {
			return makeErrResult("supervisor_bad_args", "first argument must be a list of agent name strings"), nil
		}
		strategy := OneForOne
		if len(args) >= 2 {
			s, ok := args[1].(*StringVal)
			if !ok {
				return makeErrResult("supervisor_bad_args", "strategy must be a String"), nil
			}
			switch s.V {
			case "one_for_one":
				strategy = OneForOne
			case "one_for_all":
				strategy = OneForAll
			case "rest_for_one":
				strategy = RestForOne
			default:
				return makeErrResult("supervisor_bad_args", "unknown strategy: "+s.V), nil
			}
		}
		maxRestarts := 3
		if len(args) >= 3 {
			n, ok := args[2].(*IntVal)
			if !ok {
				return makeErrResult("supervisor_bad_args", "max_restarts must be an Int"), nil
			}
			maxRestarts = int(n.V)
		}
		windowSeconds := 60
		if len(args) >= 4 {
			n, ok := args[3].(*IntVal)
			if !ok {
				return makeErrResult("supervisor_bad_args", "window_seconds must be an Int"), nil
			}
			windowSeconds = int(n.V)
		}

		specs := make([]ChildSpec, 0, len(nameList.Elements))
		for _, elem := range nameList.Elements {
			name, ok := elem.(*StringVal)
			if !ok {
				return makeErrResult("supervisor_bad_args", "agent names must be Strings"), nil
			}
			decl, exists := interp.agentDecls[name.V]
			if !exists {
				return makeErrResult("supervisor_bad_args", "unknown agent: "+name.V), nil
			}
			specs = append(specs, ChildSpec{
				Name:      name.V,
				AgentDecl: decl,
			})
		}

		sup := NewSupervisor(interp, SupervisorConfig{
			Strategy:      strategy,
			MaxRestarts:   maxRestarts,
			WindowSeconds: windowSeconds,
		}, specs)
		if err := sup.Start(); err != nil {
			return makeErrResult("supervisor_start_failed", err.Error()), nil
		}

		result := &MapVal{Entries: make(map[string]Value)}
		refs := sup.Children()
		for i, child := range specs {
			result.Entries[child.Name] = refs[i]
			result.Order = append(result.Order, child.Name)
		}
		return result, nil
	}})

	interp.env.Set("with_capability", &BuiltinVal{Name: "with_capability", Fn: func(args []Value) (Value, error) {
		if len(args) != 2 {
			return makeErrResult("capability_bad_args",
				fmt.Sprintf("with_capability(name, fn) takes 2 arguments, got %d", len(args))), nil
		}
		name, ok := args[0].(*StringVal)
		if !ok {
			return makeErrResult("capability_bad_args", "with_capability name must be a String"), nil
		}
		switch args[1].(type) {
		case *FunctionVal, *BuiltinVal:
		default:
			return makeErrResult("capability_bad_args", "with_capability body must be a function"), nil
		}
		interp.capabilityStack = append(interp.capabilityStack, name.V)
		defer func() {
			interp.capabilityStack = interp.capabilityStack[:len(interp.capabilityStack)-1]
		}()
		return interp.applyCallback(args[1], nil), nil
	}})
}

// hasCapability returns true when the named capability is currently in scope.
func (interp *Interpreter) hasCapability(name string) bool {
	return slices.Contains(interp.capabilityStack, name)
}

// ApplyCallback invokes a FunctionVal or BuiltinVal callback from Go code.
func (interp *Interpreter) ApplyCallback(fn Value, args []Value) Value {
	return interp.applyCallback(fn, args)
}

// applyCallback calls a FunctionVal or BuiltinVal with the given arguments.
func (interp *Interpreter) applyCallback(fn Value, args []Value) Value {
	switch f := fn.(type) {
	case *FunctionVal:
		prev := interp.env
		interp.env = NewEnclosedEnvironment(f.Env)
		for i, p := range f.Params {
			if i < len(args) {
				interp.env.Set(p.Name, args[i])
			}
		}
		result := interp.evalBlock(f.Body)
		interp.env = prev
		if rs, ok := result.(*ReturnSignal); ok {
			return rs.Value
		}
		return result
	case *BuiltinVal:
		val, err := f.Fn(args)
		if err != nil {
			panic("runtime error: " + err.Error())
		}
		return val
	default:
		panic("runtime error: not a callable value")
	}
}

func (interp *Interpreter) registerBuiltinEnums() {
	interp.enumDecls["Option"] = &parser.EnumDecl{
		Name: "Option",
		Variants: []parser.EnumVariant{
			{Name: "Some", Fields: []parser.Field{{Name: "value"}}},
			{Name: "None"},
		},
	}
	interp.enumDecls["Result"] = &parser.EnumDecl{
		Name: "Result",
		Variants: []parser.EnumVariant{
			{Name: "Ok", Fields: []parser.Field{{Name: "value"}}},
			{Name: "Err", Fields: []parser.Field{{Name: "error"}}},
		},
	}
}

func (interp *Interpreter) evalStatement(stmt parser.Statement) Value {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		val := interp.evalExpression(s.Value)
		if rs, ok := val.(*ReturnSignal); ok {
			return rs
		}
		val = interp.maybeCoerceByLetAnnotation(s.TypeExpr, val)
		interp.env.Set(s.Name, val)
		return val
	case *parser.MutStatement:
		val := interp.evalExpression(s.Value)
		if rs, ok := val.(*ReturnSignal); ok {
			return rs
		}
		interp.env.Set(s.Name, val)
		return val
	case *parser.ReturnStatement:
		val := interp.evalExpression(s.Value)
		return &ReturnSignal{Value: val}
	case *parser.BreakStatement:
		return &BreakSignal{}
	case *parser.ContinueStatement:
		return &ContinueSignal{}
	case *parser.ExpressionStatement:
		return interp.evalExpression(s.Expression)
	case *parser.FnDecl:
		return nil
	case *parser.ObjectDecl:
		return nil
	case *parser.EnumDecl:
		return nil
	case *parser.ActorDecl:
		return nil
	case *parser.PipelineDecl:
		return nil
	case *parser.ToolDecl:
		return nil
	case *parser.AgentDecl:
		return nil
	case *parser.MCPDecl:
		return nil
	case *parser.ServiceDecl:
		return nil
	case *parser.ProtocolDecl:
		return nil
	case *parser.ImplDecl:
		return nil
	case *parser.EffectDecl:
		return nil
	case *parser.ImportStatement:
		return nil
	case *parser.LetDestructureStatement:
		return interp.evalLetDestructure(s)
	case *parser.ExportStatement:
		return interp.evalStatement(s.Inner)
	case *parser.TypeAliasDecl:
		return nil
	}
	return &NilVal{}
}

func (interp *Interpreter) EvalExpressionPublic(expr parser.Expression) Value {
	return interp.evalExpression(expr)
}

func (interp *Interpreter) evalExpression(expr parser.Expression) Value {
	if expr == nil {
		return &NilVal{}
	}

	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return &IntVal{V: e.Value}
	case *parser.FloatLiteral:
		return &FloatVal{V: e.Value}
	case *parser.StringLiteral:
		return &StringVal{V: e.Value}
	case *parser.BooleanLiteral:
		return &BoolVal{V: e.Value}
	case *parser.NilLiteral:
		return &NilVal{}
	case *parser.LeadingDotExpression:
		return interp.evalLeadingDot(e)
	case *parser.Identifier:
		return interp.evalIdentifier(e)
	case *parser.SelfExpression:
		if val, ok := interp.env.Get("self"); ok {
			return val
		}
		return &NilVal{}
	case *parser.PrefixExpression:
		return interp.evalPrefix(e)
	case *parser.InfixExpression:
		return interp.evalInfix(e)
	case *parser.AssignmentExpression:
		return interp.evalAssignment(e)
	case *parser.IfExpression:
		return interp.evalIf(e)
	case *parser.BlockExpression:
		return interp.evalBlockScoped(e)
	case *parser.MatchExpression:
		return interp.evalMatch(e)
	case *parser.CallExpression:
		return interp.evalCall(e)
	case *parser.FieldAccessExpression:
		return interp.evalFieldAccess(e)
	case *parser.IndexExpression:
		return interp.evalIndex(e)
	case *parser.ListLiteral:
		return interp.evalList(e)
	case *parser.ObjectLiteral:
		return interp.evalObjectLiteral(e)
	case *parser.LambdaExpression:
		return interp.evalLambda(e)
	case *parser.SpawnExpression:
		return interp.evalSpawn(e)
	case *parser.HandleExpression:
		return interp.evalHandle(e)
	case *parser.ForInExpression:
		return interp.evalForIn(e)
	case *parser.WhileExpression:
		return interp.evalWhile(e)
	case *parser.PostfixExpression:
		return interp.evalPostfix(e)
	case *parser.SuperExpression:
		return interp.evalSuper()
	case *parser.SpreadExpression:
		return &NilVal{}
	}

	return &NilVal{}
}

func (interp *Interpreter) evalIdentifier(ident *parser.Identifier) Value {
	if interp.runtimeEffects[ident.Value] {
		return &EffectNamespaceVal{Name: ident.Value}
	}
	if val, ok := interp.env.Get(ident.Value); ok {
		return val
	}
	if _, ok := interp.toolDecls[ident.Value]; ok {
		return &ToolVal{Name: ident.Value, interp: interp}
	}
	if _, ok := interp.enumDecls[ident.Value]; ok {
		return &StringVal{V: "__enum__" + ident.Value}
	}
	if _, ok := interp.pipelineDecls[ident.Value]; ok {
		return &ObjectVal{
			TypeName: "__pipeline__",
			Fields: map[string]Value{
				"__name__": &StringVal{V: ident.Value},
			},
		}
	}
	return &NilVal{}
}

func (interp *Interpreter) evalPrefix(expr *parser.PrefixExpression) Value {
	right := interp.evalExpression(expr.Right)
	switch expr.Operator {
	case "!":
		if b, ok := right.(*BoolVal); ok {
			return &BoolVal{V: !b.V}
		}
	case "-":
		switch v := right.(type) {
		case *IntVal:
			return &IntVal{V: -v.V}
		case *FloatVal:
			return &FloatVal{V: -v.V}
		}
	}
	return &NilVal{}
}

func (interp *Interpreter) evalPostfix(expr *parser.PostfixExpression) Value {
	if expr.Operator == "?" {
		val := interp.evalExpression(expr.Left)
		if rs, ok := val.(*ReturnSignal); ok {
			return rs
		}
		ev, ok := val.(*EnumVal)
		if !ok || ev.TypeName != "Result" {
			return val
		}
		if ev.Variant == "Ok" {
			return ev.Fields["value"]
		}
		return &ReturnSignal{Value: val}
	}
	return &NilVal{}
}

func (interp *Interpreter) evalInfix(expr *parser.InfixExpression) Value {
	if expr.Operator == "<-" {
		return interp.evalSend(expr)
	}

	if expr.Operator == "??" {
		left := interp.evalExpression(expr.Left)
		if rs, ok := left.(*ReturnSignal); ok {
			return rs
		}
		if ev, ok := left.(*EnumVal); ok {
			if ev.TypeName == "Result" && ev.Variant == "Ok" {
				return ev.Fields["value"]
			}
			if ev.TypeName == "Result" && ev.Variant == "Err" {
				return interp.evalExpression(expr.Right)
			}
			if ev.TypeName == "Option" && ev.Variant == "Some" {
				return ev.Fields["value"]
			}
			if ev.TypeName == "Option" && ev.Variant == "None" {
				return interp.evalExpression(expr.Right)
			}
		}
		if _, ok := left.(*NilVal); ok {
			return interp.evalExpression(expr.Right)
		}
		return left
	}

	left := interp.evalExpression(expr.Left)
	if rs, ok := left.(*ReturnSignal); ok {
		return rs
	}

	if expr.Operator == "&&" {
		if lb, ok := left.(*BoolVal); ok {
			if !lb.V {
				return &BoolVal{V: false}
			}
			right := interp.evalExpression(expr.Right)
			if rb, ok := right.(*BoolVal); ok {
				return &BoolVal{V: rb.V}
			}
		}
		return &BoolVal{V: false}
	}
	if expr.Operator == "||" {
		if lb, ok := left.(*BoolVal); ok {
			if lb.V {
				return &BoolVal{V: true}
			}
			right := interp.evalExpression(expr.Right)
			if rb, ok := right.(*BoolVal); ok {
				return &BoolVal{V: rb.V}
			}
		}
		return &BoolVal{V: false}
	}

	right := interp.evalExpression(expr.Right)
	if rs, ok := right.(*ReturnSignal); ok {
		return rs
	}

	if li, ok := left.(*IntVal); ok {
		if ri, ok := right.(*IntVal); ok {
			return interp.evalIntInfix(expr.Operator, li.V, ri.V)
		}
	}

	if lf, ok := left.(*FloatVal); ok {
		if rf, ok := right.(*FloatVal); ok {
			return interp.evalFloatInfix(expr.Operator, lf.V, rf.V)
		}
	}

	if lb, ok := left.(*BytesVal); ok {
		if rb, ok := right.(*BytesVal); ok {
			if expr.Operator == "+" {
				combined := make([]byte, len(lb.V)+len(rb.V))
				copy(combined, lb.V)
				copy(combined[len(lb.V):], rb.V)
				return &BytesVal{V: combined}
			}
			if expr.Operator == "==" {
				return &BoolVal{V: lb.Equals(rb)}
			}
			if expr.Operator == "!=" {
				return &BoolVal{V: !lb.Equals(rb)}
			}
		}
	}

	if ls, ok := left.(*StringVal); ok {
		if rs, ok := right.(*StringVal); ok {
			if expr.Operator == "+" {
				return &StringVal{V: ls.V + rs.V}
			}
			if expr.Operator == "==" {
				return &BoolVal{V: ls.V == rs.V}
			}
			if expr.Operator == "!=" {
				return &BoolVal{V: ls.V != rs.V}
			}
		}
	}

	if lb, ok := left.(*BoolVal); ok {
		if rb, ok := right.(*BoolVal); ok {
			if expr.Operator == "==" {
				return &BoolVal{V: lb.V == rb.V}
			}
			if expr.Operator == "!=" {
				return &BoolVal{V: lb.V != rb.V}
			}
		}
	}

	if expr.Operator == "==" {
		return &BoolVal{V: left.Equals(right)}
	}
	if expr.Operator == "!=" {
		return &BoolVal{V: !left.Equals(right)}
	}

	return &NilVal{}
}

func (interp *Interpreter) evalIntInfix(op string, l, r int64) Value {
	switch op {
	case "+":
		return &IntVal{V: l + r}
	case "-":
		return &IntVal{V: l - r}
	case "*":
		return &IntVal{V: l * r}
	case "/":
		if r == 0 {
			return makeErrResult("div_by_zero", "integer division by zero")
		}
		return &IntVal{V: l / r}
	case "%":
		if r == 0 {
			return makeErrResult("div_by_zero", "integer modulo by zero")
		}
		return &IntVal{V: l % r}
	case "<":
		return &BoolVal{V: l < r}
	case ">":
		return &BoolVal{V: l > r}
	case "<=":
		return &BoolVal{V: l <= r}
	case ">=":
		return &BoolVal{V: l >= r}
	case "==":
		return &BoolVal{V: l == r}
	case "!=":
		return &BoolVal{V: l != r}
	}
	return &NilVal{}
}

func (interp *Interpreter) evalFloatInfix(op string, l, r float64) Value {
	switch op {
	case "+":
		return &FloatVal{V: l + r}
	case "-":
		return &FloatVal{V: l - r}
	case "*":
		return &FloatVal{V: l * r}
	case "/":
		if r == 0 {
			return makeErrResult("div_by_zero", "float division by zero")
		}
		return &FloatVal{V: l / r}
	case "<":
		return &BoolVal{V: l < r}
	case ">":
		return &BoolVal{V: l > r}
	case "==":
		return &BoolVal{V: l == r}
	case "!=":
		return &BoolVal{V: l != r}
	}
	return &NilVal{}
}

func (interp *Interpreter) evalSend(expr *parser.InfixExpression) Value {
	target := interp.evalExpression(expr.Left)
	ref, ok := target.(*ActorRef)
	if !ok {
		return &NilVal{}
	}

	msg := ActorMessage{Args: make(map[string]Value)}

	switch r := expr.Right.(type) {
	case *parser.Identifier:
		msg.Method = r.Value
	case *parser.CallExpression:
		if ident, ok := r.Function.(*parser.Identifier); ok {
			msg.Method = ident.Value
		}
		for _, arg := range r.Args {
			val := interp.evalExpression(arg.Value)
			if arg.Name != "" {
				msg.Args[arg.Name] = val
			} else {
				msg.Args[arg.Name] = val
			}
		}
	}

	ref.Mailbox <- msg
	return &NilVal{}
}

func (interp *Interpreter) evalAssignment(expr *parser.AssignmentExpression) Value {
	val := interp.evalExpression(expr.Value)

	switch target := expr.Left.(type) {
	case *parser.Identifier:
		switch expr.Operator {
		case "=":
			interp.env.Update(target.Value, val)
		case "+=":
			cur, _ := interp.env.Get(target.Value)
			newVal := interp.addValues(cur, val)
			interp.env.Update(target.Value, newVal)
		case "-=":
			cur, _ := interp.env.Get(target.Value)
			newVal := interp.subValues(cur, val)
			interp.env.Update(target.Value, newVal)
		}
	case *parser.FieldAccessExpression:
		leftVal := interp.evalExpression(target.Left)
		if selfProxy, ok := leftVal.(*actorSelf); ok {
			switch expr.Operator {
			case "=":
				selfProxy.SetField(target.Field, val)
			case "+=":
				cur, _ := selfProxy.GetField(target.Field)
				selfProxy.SetField(target.Field, interp.addValues(cur, val))
			case "-=":
				cur, _ := selfProxy.GetField(target.Field)
				selfProxy.SetField(target.Field, interp.subValues(cur, val))
			}
		} else if obj, ok := leftVal.(*ObjectVal); ok {
			switch expr.Operator {
			case "=":
				obj.Fields[target.Field] = val
			}
		}
	}

	return val
}

func (interp *Interpreter) addValues(a, b Value) Value {
	if ai, ok := a.(*IntVal); ok {
		if bi, ok := b.(*IntVal); ok {
			return &IntVal{V: ai.V + bi.V}
		}
	}
	if af, ok := a.(*FloatVal); ok {
		if bf, ok := b.(*FloatVal); ok {
			return &FloatVal{V: af.V + bf.V}
		}
	}
	if as, ok := a.(*StringVal); ok {
		if bs, ok := b.(*StringVal); ok {
			return &StringVal{V: as.V + bs.V}
		}
	}
	return a
}

func (interp *Interpreter) subValues(a, b Value) Value {
	if ai, ok := a.(*IntVal); ok {
		if bi, ok := b.(*IntVal); ok {
			return &IntVal{V: ai.V - bi.V}
		}
	}
	if af, ok := a.(*FloatVal); ok {
		if bf, ok := b.(*FloatVal); ok {
			return &FloatVal{V: af.V - bf.V}
		}
	}
	return a
}

func (interp *Interpreter) evalIf(expr *parser.IfExpression) Value {
	cond := interp.evalExpression(expr.Condition)
	if isTruthy(cond) {
		return interp.evalBlockScoped(expr.Consequence)
	}
	if expr.Alternative != nil {
		return interp.evalBlockScoped(expr.Alternative)
	}
	return &NilVal{}
}

func isTruthy(v Value) bool {
	switch val := v.(type) {
	case *BoolVal:
		return val.V
	case *NilVal:
		return false
	default:
		return true
	}
}

func (interp *Interpreter) evalBlock(block *parser.BlockExpression) Value {
	if block == nil {
		return &NilVal{}
	}
	var result Value = &NilVal{}
	for _, stmt := range block.Statements {
		result = interp.evalStatement(stmt)
		switch result.(type) {
		case *ReturnSignal, *BreakSignal, *ContinueSignal:
			return result
		}
	}
	return result
}

func (interp *Interpreter) evalBlockScoped(block *parser.BlockExpression) Value {
	prev := interp.env
	interp.env = NewEnclosedEnvironment(interp.env)
	result := interp.evalBlock(block)
	interp.env = prev
	return result
}

func (interp *Interpreter) evalMatch(expr *parser.MatchExpression) Value {
	subject := interp.evalExpression(expr.Subject)

	for _, arm := range expr.Arms {
		matched, bindings := interp.matchPattern(subject, arm.Pattern)
		if !matched {
			continue
		}
		prev := interp.env
		interp.env = NewEnclosedEnvironment(interp.env)
		for k, v := range bindings {
			interp.env.Set(k, v)
		}
		if arm.Guard != nil {
			cond := interp.evalExpression(arm.Guard)
			interp.env = prev
			b, ok := cond.(*BoolVal)
			if !ok || !b.V {
				continue
			}
			interp.env = NewEnclosedEnvironment(prev)
			for k, v := range bindings {
				interp.env.Set(k, v)
			}
		}
		result := interp.evalExpression(arm.Body)
		interp.env = prev
		return result
	}
	return &NilVal{}
}

func (interp *Interpreter) matchPattern(subject Value, pat parser.MatchPattern) (bool, map[string]Value) {
	bindings := make(map[string]Value)

	switch pat.Kind {
	case "wildcard":
		return true, bindings

	case "literal":
		switch sv := subject.(type) {
		case *IntVal:
			return fmt.Sprintf("%d", sv.V) == pat.Value, bindings
		case *FloatVal:
			return fmt.Sprintf("%g", sv.V) == pat.Value, bindings
		case *StringVal:
			return sv.V == pat.Value, bindings
		}
		return false, bindings

	case "identifier":
		if pat.Value == "true" {
			if bv, ok := subject.(*BoolVal); ok {
				return bv.V, bindings
			}
			return false, bindings
		}
		if pat.Value == "false" {
			if bv, ok := subject.(*BoolVal); ok {
				return !bv.V, bindings
			}
			return false, bindings
		}

		if strings.Contains(pat.Value, ".") {
			if ev, ok := subject.(*EnumVal); ok {
				parts := strings.SplitN(pat.Value, ".", 2)
				return ev.TypeName == parts[0] && ev.Variant == parts[1], bindings
			}
			return false, bindings
		}

		if len(pat.Value) > 0 && pat.Value[0] >= 'A' && pat.Value[0] <= 'Z' {
			if ev, ok := subject.(*EnumVal); ok {
				return ev.Variant == pat.Value, bindings
			}
			if iv, ok := subject.(*IntVal); ok {
				return fmt.Sprintf("%d", iv.V) == pat.Value, bindings
			}
			return false, bindings
		}

		bindings[pat.Value] = subject
		return true, bindings

	case "destructure":
		ev, ok := subject.(*EnumVal)
		if !ok {
			return false, bindings
		}

		var variantMatch bool
		if strings.Contains(pat.Value, ".") {
			parts := strings.SplitN(pat.Value, ".", 2)
			variantMatch = ev.TypeName == parts[0] && ev.Variant == parts[1]
		} else {
			variantMatch = ev.Variant == pat.Value
		}

		if !variantMatch {
			return false, bindings
		}

		if decl, ok := interp.enumDecls[ev.TypeName]; ok {
			for _, variant := range decl.Variants {
				if variant.Name == ev.Variant {
					for i, binding := range pat.Bindings {
						if i < len(variant.Fields) {
							fieldName := variant.Fields[i].Name
							if val, exists := ev.Fields[fieldName]; exists {
								bindings[binding] = val
							}
						}
					}
					break
				}
			}
		}

		return true, bindings

	case "object_destructure":
		ov, ok := subject.(*ObjectVal)
		if !ok {
			return false, bindings
		}
		if ov.TypeName != pat.Value {
			return false, bindings
		}
		for _, of := range pat.ObjectFields {
			fieldVal, exists := ov.Fields[of.Field]
			if !exists {
				return false, bindings
			}
			if of.Subpattern == nil {
				bindings[of.Field] = fieldVal
				continue
			}
			ok, sub := interp.matchPattern(fieldVal, *of.Subpattern)
			if !ok {
				return false, bindings
			}
			maps.Copy(bindings, sub)
		}
		return true, bindings
	}

	return false, bindings
}

func (interp *Interpreter) evalCall(expr *parser.CallExpression) Value {
	if fa, ok := expr.Function.(*parser.FieldAccessExpression); ok {
		if ident, ok := fa.Left.(*parser.Identifier); ok {
			if _, isEnum := interp.enumDecls[ident.Value]; isEnum {
				return interp.evalEnumConstruction(ident.Value, fa.Field, expr.Args)
			}
		}
	}

	if fa, ok := expr.Function.(*parser.FieldAccessExpression); ok {
		if fa.Field == "ask" {
			left := interp.evalExpression(fa.Left)
			if ref, ok := left.(*ActorRef); ok {
				return interp.evalActorAskFromAST(ref, expr.Args)
			}
		}
	}

	if fa, ok := expr.Function.(*parser.FieldAccessExpression); ok {
		if fa.Field == "chat" {
			left := interp.evalExpression(fa.Left)
			if ref, ok := left.(*ActorRef); ok {
				return interp.evalAgentChat(ref, expr.Args)
			}
		}
	}

	if fa, ok := expr.Function.(*parser.FieldAccessExpression); ok {
		if fa.Field == "run" {
			if ident, ok := fa.Left.(*parser.Identifier); ok {
				if _, isPipeline := interp.pipelineDecls[ident.Value]; isPipeline {
					return interp.evalPipeline(ident.Value)
				}
			}
		}
	}

	if fa, ok := expr.Function.(*parser.FieldAccessExpression); ok {
		if ident, ok := fa.Left.(*parser.Identifier); ok {
			if _, isTool := interp.toolDecls[ident.Value]; isTool {
				switch fa.Field {
				case "run":
					return interp.evalToolRun(ident.Value, expr.Args)
				case "schema":
					return interp.evalToolSchema(ident.Value)
				}
			}
		}
	}

	fn := interp.evalExpression(expr.Function)
	if rs, ok := fn.(*ReturnSignal); ok {
		return rs
	}

	args := make([]Value, len(expr.Args))
	for i, a := range expr.Args {
		args[i] = interp.evalExpression(a.Value)
	}

	switch f := fn.(type) {
	case *FunctionVal:
		return interp.callFunction(f, expr.Args)
	case *BuiltinVal:
		result, err := f.Fn(args)
		if err != nil {
			panic("runtime error: " + err.Error())
		}
		return result
	}

	return &NilVal{}
}

func (interp *Interpreter) callFunction(fn *FunctionVal, callArgs []parser.CallArg) Value {
	fnEnv := NewEnclosedEnvironment(fn.Env)

	if fn.Self != nil {
		fnEnv.Set("self", fn.Self)
	}

	argIdx := 0
	for _, param := range fn.Params {
		if param.Name == "self" {
			continue
		}
		var val Value = &NilVal{}

		for _, arg := range callArgs {
			if arg.Name == param.Name {
				val = interp.evalExpression(arg.Value)
				break
			}
		}
		if _, ok := val.(*NilVal); ok && argIdx < len(callArgs) && callArgs[argIdx].Name == "" {
			val = interp.evalExpression(callArgs[argIdx].Value)
		}
		argIdx++

		if _, ok := val.(*NilVal); ok && param.Default != nil {
			val = interp.evalExpression(param.Default)
		}

		fnEnv.Set(param.Name, val)
	}

	prev := interp.env
	interp.env = fnEnv
	result := interp.evalBlock(fn.Body)
	interp.env = prev

	if rs, ok := result.(*ReturnSignal); ok {
		return rs.Value
	}
	if _, ok := result.(*BreakSignal); ok {
		panic("runtime error: 'break' outside loop")
	}
	if _, ok := result.(*ContinueSignal); ok {
		panic("runtime error: 'continue' outside loop")
	}
	return result
}

func (interp *Interpreter) evalFieldAccess(expr *parser.FieldAccessExpression) Value {
	if ident, ok := expr.Left.(*parser.Identifier); ok {
		if _, isEnum := interp.enumDecls[ident.Value]; isEnum {
			return &EnumVal{TypeName: ident.Value, Variant: expr.Field}
		}
	}

	left := interp.evalExpression(expr.Left)

	if ns, ok := left.(*EffectNamespaceVal); ok {
		handler, found := interp.effectStack.Resolve(ns.Name)
		if !found {
			panic(fmt.Sprintf("runtime error: effect '%s' used but no handler installed", ns.Name))
		}
		if obj, ok := handler.(*ObjectVal); ok {
			if method, exists := obj.Fields[expr.Field]; exists {
				return method
			}
		}
		return &NilVal{}
	}

	if _, ok := left.(*ActorRef); ok {
		if expr.Field == "ask" {
			ref := left.(*ActorRef)
			return &BuiltinVal{Name: "__actor_ask__", Fn: func(args []Value) (Value, error) {
				_ = ref
				return &NilVal{}, nil
			}}
		}
	}

	if selfProxy, ok := left.(*actorSelf); ok {
		if val, found := selfProxy.GetField(expr.Field); found {
			return val
		}
		return &NilVal{}
	}

	if bv, ok := left.(*BytesVal); ok {
		switch expr.Field {
		case "len":
			return &BuiltinVal{Name: "Bytes.len", Fn: func(args []Value) (Value, error) {
				return &IntVal{V: int64(len(bv.V))}, nil
			}}
		case "to_string":
			return &BuiltinVal{Name: "Bytes.to_string", Fn: func(args []Value) (Value, error) {
				return &StringVal{V: string(bv.V)}, nil
			}}
		case "to_hex":
			return &BuiltinVal{Name: "Bytes.to_hex", Fn: func(args []Value) (Value, error) {
				return &StringVal{V: bv.ToHex()}, nil
			}}
		case "slice":
			return &BuiltinVal{Name: "Bytes.slice", Fn: func(args []Value) (Value, error) {
				if len(args) != 2 {
					return nil, fmt.Errorf("Bytes.slice() takes 2 arguments")
				}
				start, ok1 := args[0].(*IntVal)
				end, ok2 := args[1].(*IntVal)
				if !ok1 || !ok2 {
					return nil, fmt.Errorf("Bytes.slice() requires Int arguments")
				}
				s, e := int(start.V), int(end.V)
				if s < 0 {
					s = 0
				}
				if e > len(bv.V) {
					e = len(bv.V)
				}
				if s > e {
					return &BytesVal{V: nil}, nil
				}
				sliced := make([]byte, e-s)
				copy(sliced, bv.V[s:e])
				return &BytesVal{V: sliced}, nil
			}}
		}
		return &NilVal{}
	}

	if mv, ok := left.(*MapVal); ok {
		switch expr.Field {
		case "get":
			return &BuiltinVal{Name: "Map.get", Fn: func(args []Value) (Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("Map.get() takes 1 argument")
				}
				k, ok := args[0].(*StringVal)
				if !ok {
					return nil, fmt.Errorf("Map.get() key must be String")
				}
				if v, exists := mv.Entries[k.V]; exists {
					return v, nil
				}
				return &NilVal{}, nil
			}}
		case "set":
			return &BuiltinVal{Name: "Map.set", Fn: func(args []Value) (Value, error) {
				if len(args) != 2 {
					return nil, fmt.Errorf("Map.set() takes 2 arguments")
				}
				k, ok := args[0].(*StringVal)
				if !ok {
					return nil, fmt.Errorf("Map.set() key must be String")
				}
				newEntries := make(map[string]Value, len(mv.Entries)+1)
				maps.Copy(newEntries, mv.Entries)
				newOrder := make([]string, len(mv.Order))
				copy(newOrder, mv.Order)
				if _, exists := newEntries[k.V]; !exists {
					newOrder = append(newOrder, k.V)
				}
				newEntries[k.V] = args[1]
				return &MapVal{Entries: newEntries, Order: newOrder}, nil
			}}
		case "has":
			return &BuiltinVal{Name: "Map.has", Fn: func(args []Value) (Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("Map.has() takes 1 argument")
				}
				k, ok := args[0].(*StringVal)
				if !ok {
					return nil, fmt.Errorf("Map.has() key must be String")
				}
				_, exists := mv.Entries[k.V]
				return &BoolVal{V: exists}, nil
			}}
		case "delete":
			return &BuiltinVal{Name: "Map.delete", Fn: func(args []Value) (Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("Map.delete() takes 1 argument")
				}
				k, ok := args[0].(*StringVal)
				if !ok {
					return nil, fmt.Errorf("Map.delete() key must be String")
				}
				newEntries := make(map[string]Value, len(mv.Entries))
				for kk, vv := range mv.Entries {
					if kk != k.V {
						newEntries[kk] = vv
					}
				}
				newOrder := make([]string, 0, len(mv.Order))
				for _, key := range mv.Order {
					if key != k.V {
						newOrder = append(newOrder, key)
					}
				}
				return &MapVal{Entries: newEntries, Order: newOrder}, nil
			}}
		case "keys":
			return &BuiltinVal{Name: "Map.keys", Fn: func(args []Value) (Value, error) {
				elems := make([]Value, len(mv.Order))
				for i, k := range mv.Order {
					elems[i] = &StringVal{V: k}
				}
				return &ListVal{Elements: elems}, nil
			}}
		case "values":
			return &BuiltinVal{Name: "Map.values", Fn: func(args []Value) (Value, error) {
				elems := make([]Value, len(mv.Order))
				for i, k := range mv.Order {
					elems[i] = mv.Entries[k]
				}
				return &ListVal{Elements: elems}, nil
			}}
		case "entries":
			return &BuiltinVal{Name: "Map.entries", Fn: func(args []Value) (Value, error) {
				elems := make([]Value, len(mv.Order))
				for i, k := range mv.Order {
					elems[i] = &ListVal{Elements: []Value{&StringVal{V: k}, mv.Entries[k]}}
				}
				return &ListVal{Elements: elems}, nil
			}}
		case "size":
			return &IntVal{V: int64(len(mv.Entries))}
		}
		return &NilVal{}
	}

	if tv, ok := left.(*ToolVal); ok {
		switch expr.Field {
		case "run":
			return &BuiltinVal{Name: tv.Name + ".run", Fn: func(args []Value) (Value, error) {
				return tv.interp.evalToolRunFromValues(tv.Name, args), nil
			}}
		case "schema":
			return &BuiltinVal{Name: tv.Name + ".schema", Fn: func(args []Value) (Value, error) {
				return tv.interp.evalToolSchema(tv.Name), nil
			}}
		case "name":
			return &BuiltinVal{Name: tv.Name + ".name", Fn: func(args []Value) (Value, error) {
				return &StringVal{V: tv.Name}, nil
			}}
		case "description":
			return &BuiltinVal{Name: tv.Name + ".description", Fn: func(args []Value) (Value, error) {
				if decl, ok := tv.interp.toolDecls[tv.Name]; ok {
					return &StringVal{V: decl.Description}, nil
				}
				return &StringVal{V: ""}, nil
			}}
		case "input_schema":
			return &BuiltinVal{Name: tv.Name + ".input_schema", Fn: func(args []Value) (Value, error) {
				return tv.interp.evalToolInputSchema(tv.Name), nil
			}}
		case "output_schema":
			return &BuiltinVal{Name: tv.Name + ".output_schema", Fn: func(args []Value) (Value, error) {
				return tv.interp.evalToolOutputSchema(tv.Name), nil
			}}
		}
		return &NilVal{}
	}

	if sv, ok := left.(*SupervisorVal); ok {
		switch expr.Field {
		case "start":
			return &BuiltinVal{Name: "Supervisor.start", Fn: func(args []Value) (Value, error) {
				if err := sv.Sup.Start(); err != nil {
					return makeErrResult("supervisor_start_failed", err.Error()), nil
				}
				return makeOkResult(&NilVal{}), nil
			}}
		case "stop":
			return &BuiltinVal{Name: "Supervisor.stop", Fn: func(args []Value) (Value, error) {
				sv.Sup.Stop()
				return &NilVal{}, nil
			}}
		case "children":
			return &BuiltinVal{Name: "Supervisor.children", Fn: func(args []Value) (Value, error) {
				refs := sv.Sup.Children()
				entries := make(map[string]Value)
				var order []string
				for i, ref := range refs {
					if ref == nil {
						continue
					}
					name := ""
					if i < len(sv.ChildNames) {
						name = sv.ChildNames[i]
					}
					if name == "" {
						continue
					}
					if _, exists := entries[name]; !exists {
						order = append(order, name)
					}
					entries[name] = ref
				}
				return &MapVal{Entries: entries, Order: order}, nil
			}}
		case "add_child":
			return &BuiltinVal{Name: "Supervisor.add_child", Fn: func(args []Value) (Value, error) {
				if len(args) < 1 {
					return makeErrResult("supervisor_bad_args",
						"add_child(name) requires the actor/agent name as a String"), nil
				}
				name, ok := args[0].(*StringVal)
				if !ok {
					return makeErrResult("supervisor_bad_args", "name must be a String"), nil
				}
				var spec ChildSpec
				if decl, exists := sv.interp.actorDecls[name.V]; exists {
					spec = ChildSpec{Name: name.V, ActorDecl: decl}
				} else if decl, exists := sv.interp.agentDecls[name.V]; exists {
					spec = ChildSpec{Name: name.V, AgentDecl: decl}
				} else {
					return makeErrResult("supervisor_bad_args",
						"unknown actor or agent: "+name.V), nil
				}
				ref := sv.Sup.AddChild(spec)
				sv.ChildNames = append(sv.ChildNames, name.V)
				if ref == nil {
					return makeErrResult("supervisor_start_failed",
						"failed to spawn child "+name.V), nil
				}
				return makeOkResult(ref), nil
			}}
		}
		return &NilVal{}
	}

	if obj, ok := left.(*ObjectVal); ok {
		if val, exists := obj.Fields[expr.Field]; exists {
			return val
		}
		if method := interp.resolveImplMethod(obj.TypeName, expr.Field); method != nil {
			return &FunctionVal{
				Name:   method.Name,
				Params: method.Params,
				Body:   method.Body,
				Env:    interp.env,
				Self:   obj,
			}
		}
		if result := interp.resolveFieldOnDelegate(obj, expr.Field, make(map[string]bool)); result != nil {
			return result
		}
		return &NilVal{}
	}

	return &NilVal{}
}

// evalLeadingDot materializes `.name(args...)` as a tagged ObjectVal with
// named args under `named` and positional args under `args`.
func (interp *Interpreter) evalLeadingDot(expr *parser.LeadingDotExpression) Value {
	positional := make([]Value, 0, len(expr.Args))
	named := make(map[string]Value, len(expr.Args))
	var namedOrder []string
	for _, a := range expr.Args {
		v := interp.evalExpression(a.Value)
		if a.Name == "" {
			positional = append(positional, v)
		} else {
			if _, exists := named[a.Name]; !exists {
				namedOrder = append(namedOrder, a.Name)
			}
			named[a.Name] = v
		}
	}
	return &ObjectVal{
		TypeName: "Policy",
		Fields: map[string]Value{
			"tag":   &StringVal{V: expr.Name},
			"args":  &ListVal{Elements: positional},
			"named": &MapVal{Entries: named, Order: namedOrder},
		},
	}
}

// maybeCoerceByLetAnnotation re-tags a generic ObjectVal as the declared
// type from the let annotation, validating that required fields are present.
func (interp *Interpreter) maybeCoerceByLetAnnotation(typeExpr string, val Value) Value {
	if typeExpr == "" {
		return val
	}
	ov, ok := val.(*ObjectVal)
	if !ok || ov.TypeName != "Object" {
		return val
	}
	decl, exists := interp.objectDecls[typeExpr]
	if !exists {
		return val
	}
	for _, f := range decl.Fields {
		if _, present := ov.Fields[f.Name]; !present {
			return makeErrResult("json_decode_failed",
				"let "+typeExpr+": missing required field '"+f.Name+"'")
		}
	}
	ov.TypeName = typeExpr
	return ov
}

// GetObjectFields returns the declared fields of a named object type.
func (interp *Interpreter) GetObjectFields(typeName string) ([]parser.Field, bool) {
	decl, ok := interp.objectDecls[typeName]
	if !ok {
		return nil, false
	}
	return decl.Fields, true
}

// resolveImplMethod searches all impl declarations for a method on the given type.
func (interp *Interpreter) resolveImplMethod(typeName, methodName string) *parser.FnDecl {
	for _, impl := range interp.implDecls {
		if impl.Target == typeName {
			for _, m := range impl.Methods {
				if m.Name == methodName {
					return m
				}
			}
		}
	}
	return nil
}

// resolveFieldOnDelegate recursively searches delegate chains for a field or method.
// visited prevents infinite loops from circular delegation.
func (interp *Interpreter) resolveFieldOnDelegate(obj *ObjectVal, field string, visited map[string]bool) Value {
	if visited[obj.TypeName] {
		return nil
	}
	visited[obj.TypeName] = true

	decl, ok := interp.objectDecls[obj.TypeName]
	if !ok {
		return nil
	}

	for _, del := range decl.Delegates {
		delegateVal, exists := obj.Fields[del.Name]
		if !exists {
			continue
		}
		delegateObj, ok := delegateVal.(*ObjectVal)
		if !ok {
			continue
		}
		if val, exists := delegateObj.Fields[field]; exists {
			return val
		}
		if method := interp.resolveImplMethod(delegateObj.TypeName, field); method != nil {
			return &FunctionVal{
				Name:   method.Name,
				Params: method.Params,
				Body:   method.Body,
				Env:    interp.env,
				Self:   delegateObj,
			}
		}
		if result := interp.resolveFieldOnDelegate(delegateObj, field, visited); result != nil {
			return result
		}
	}
	return nil
}

func (interp *Interpreter) evalActorAskFromAST(ref *ActorRef, callArgs []parser.CallArg) Value {
	msg := ActorMessage{
		Args:    make(map[string]Value),
		ReplyCh: make(chan Value, 1),
	}

	if len(callArgs) > 0 {
		argExpr := callArgs[0].Value
		switch a := argExpr.(type) {
		case *parser.Identifier:
			msg.Method = a.Value
		case *parser.CallExpression:
			if ident, ok := a.Function.(*parser.Identifier); ok {
				msg.Method = ident.Value
			}
			for _, arg := range a.Args {
				val := interp.evalExpression(arg.Value)
				if arg.Name != "" {
					msg.Args[arg.Name] = val
				}
			}
		}
	}

	if sendErr := safeMailboxSend(ref.Mailbox, msg); sendErr != nil {
		return makeErrResult("actor_stopped", "actor mailbox is closed")
	}

	select {
	case result := <-msg.ReplyCh:
		if ev, isResult := result.(*EnumVal); isResult && ev.TypeName == "Result" {
			return ev
		}
		return makeOkResult(result)
	case <-time.After(AskTimeout):
		return makeErrResult("ask_timeout", "actor ask exceeded "+AskTimeout.String())
	case <-ref.Done:
		return makeErrResult("actor_stopped", "actor stopped before replying")
	}
}

// safeMailboxSend recovers send-on-closed so writes to a stopped actor
// surface as an error instead of panicking.
func safeMailboxSend(mailbox chan ActorMessage, msg ActorMessage) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("send on closed mailbox: %v", r)
		}
	}()
	mailbox <- msg
	return nil
}

func (interp *Interpreter) evalIndex(expr *parser.IndexExpression) Value {
	left := interp.evalExpression(expr.Left)
	idx := interp.evalExpression(expr.Index)

	if list, ok := left.(*ListVal); ok {
		if i, ok := idx.(*IntVal); ok {
			if i.V >= 0 && int(i.V) < len(list.Elements) {
				return list.Elements[i.V]
			}
			return makeErrResult("index_out_of_bounds",
				fmt.Sprintf("list index %d out of range (len %d)", i.V, len(list.Elements)))
		}
	}
	if str, ok := left.(*StringVal); ok {
		if i, ok := idx.(*IntVal); ok {
			if i.V >= 0 && int(i.V) < len(str.V) {
				return &StringVal{V: string(str.V[i.V])}
			}
			return makeErrResult("index_out_of_bounds",
				fmt.Sprintf("string index %d out of range (len %d)", i.V, len(str.V)))
		}
	}
	if bv, ok := left.(*BytesVal); ok {
		if i, ok := idx.(*IntVal); ok {
			if i.V >= 0 && int(i.V) < len(bv.V) {
				return &IntVal{V: int64(bv.V[i.V])}
			}
			return makeErrResult("index_out_of_bounds",
				fmt.Sprintf("bytes index %d out of range (len %d)", i.V, len(bv.V)))
		}
	}
	if obj, ok := left.(*ObjectVal); ok {
		if s, ok := idx.(*StringVal); ok {
			if val, exists := obj.Fields[s.V]; exists {
				return val
			}
			return &NilVal{}
		}
	}
	return &NilVal{}
}

func (interp *Interpreter) evalList(expr *parser.ListLiteral) Value {
	var elems []Value
	for _, e := range expr.Elements {
		if spread, ok := e.(*parser.SpreadExpression); ok {
			val := interp.evalExpression(spread.Value)
			if list, ok := val.(*ListVal); ok {
				elems = append(elems, list.Elements...)
			} else {
				panic("runtime error: spread requires a list value, got " + val.Type())
			}
			continue
		}
		elems = append(elems, interp.evalExpression(e))
	}
	return &ListVal{Elements: elems}
}

func (interp *Interpreter) evalObjectLiteral(expr *parser.ObjectLiteral) Value {
	fields := make(map[string]Value)
	for _, f := range expr.Fields {
		fields[f.Name] = interp.evalExpression(f.Value)
	}
	typeName := expr.Name
	if typeName == "" {
		typeName = "Object"
	}
	return &ObjectVal{TypeName: typeName, Fields: fields}
}

func (interp *Interpreter) evalLambda(expr *parser.LambdaExpression) Value {
	return &FunctionVal{
		Name:   "<lambda>",
		Params: expr.Params,
		Body:   expr.Body,
		Env:    interp.env,
	}
}

func (interp *Interpreter) evalSpawn(expr *parser.SpawnExpression) Value {
	var actorName string
	args := make(map[string]Value)

	switch call := expr.Call.(type) {
	case *parser.CallExpression:
		if ident, ok := call.Function.(*parser.Identifier); ok {
			actorName = ident.Value
		}
		for _, arg := range call.Args {
			val := interp.evalExpression(arg.Value)
			if arg.Name != "" {
				args[arg.Name] = val
			}
		}
	case *parser.Identifier:
		actorName = call.Value
	}

	if agentDecl, ok := interp.agentDecls[actorName]; ok {
		return interp.spawnAgent(agentDecl)
	}

	decl, ok := interp.actorDecls[actorName]
	if !ok {
		return &NilVal{}
	}

	return interp.spawnActor(decl, args)
}

func (interp *Interpreter) evalHandle(expr *parser.HandleExpression) Value {
	handler := interp.evalExpression(expr.Handler)

	interp.effectStack.Push(EffectHandler{
		EffectName: expr.EffectName,
		Provider:   handler,
	})

	result := interp.evalBlockScoped(expr.Body)

	interp.effectStack.Pop()

	if rs, ok := result.(*ReturnSignal); ok {
		return rs.Value
	}
	return result
}

func (interp *Interpreter) evalForIn(expr *parser.ForInExpression) Value {
	iterable := interp.evalExpression(expr.Iterable)
	list, ok := iterable.(*ListVal)
	if !ok {
		panic("runtime error: for-in requires a list")
	}

	var result Value = &NilVal{}
	for _, elem := range list.Elements {
		prev := interp.env
		interp.env = NewEnclosedEnvironment(interp.env)
		if expr.Variable != "_" {
			interp.env.Set(expr.Variable, elem)
		}
		result = interp.evalBlock(expr.Body)
		interp.env = prev
		switch result.(type) {
		case *ReturnSignal:
			return result
		case *BreakSignal:
			return &NilVal{}
		case *ContinueSignal:
			result = &NilVal{}
		}
	}
	return result
}

func (interp *Interpreter) evalWhile(expr *parser.WhileExpression) Value {
	var result Value = &NilVal{}
	for {
		cond := interp.evalExpression(expr.Condition)
		b, ok := cond.(*BoolVal)
		if !ok || !b.V {
			break
		}
		prev := interp.env
		interp.env = NewEnclosedEnvironment(interp.env)
		result = interp.evalBlock(expr.Body)
		interp.env = prev
		switch result.(type) {
		case *ReturnSignal:
			return result
		case *BreakSignal:
			return &NilVal{}
		case *ContinueSignal:
			result = &NilVal{}
		}
	}
	return result
}

func (interp *Interpreter) evalLetDestructure(stmt *parser.LetDestructureStatement) Value {
	val := interp.evalExpression(stmt.Value)
	if rs, ok := val.(*ReturnSignal); ok {
		return rs
	}

	list, ok := val.(*ListVal)
	if !ok {
		panic("runtime error: list destructuring requires a list value, got " + val.Type())
	}

	for i, name := range stmt.Pattern.Names {
		if i < len(list.Elements) {
			interp.env.Set(name, list.Elements[i])
		} else {
			interp.env.Set(name, &NilVal{})
		}
	}

	if stmt.Pattern.RestName != "" {
		start := stmt.Pattern.RestIdx
		if start < len(list.Elements) {
			rest := make([]Value, len(list.Elements)-start)
			copy(rest, list.Elements[start:])
			interp.env.Set(stmt.Pattern.RestName, &ListVal{Elements: rest})
		} else {
			interp.env.Set(stmt.Pattern.RestName, &ListVal{Elements: nil})
		}
	}

	return val
}

func (interp *Interpreter) evalSuper() Value {
	selfVal, ok := interp.env.Get("self")
	if !ok {
		return &NilVal{}
	}
	obj, ok := selfVal.(*ObjectVal)
	if !ok {
		return &NilVal{}
	}
	decl, ok := interp.objectDecls[obj.TypeName]
	if !ok || len(decl.Delegates) == 0 {
		return &NilVal{}
	}
	delegateField := decl.Delegates[0].Name
	if delegateVal, exists := obj.Fields[delegateField]; exists {
		return delegateVal
	}
	return &NilVal{}
}

func (interp *Interpreter) evalEnumConstruction(typeName, variant string, callArgs []parser.CallArg) Value {
	fields := make(map[string]Value)

	decl, ok := interp.enumDecls[typeName]
	if !ok {
		return &EnumVal{TypeName: typeName, Variant: variant, Fields: fields}
	}

	for _, v := range decl.Variants {
		if v.Name == variant {
			for i, arg := range callArgs {
				val := interp.evalExpression(arg.Value)
				if arg.Name != "" {
					fields[arg.Name] = val
				} else if i < len(v.Fields) {
					fields[v.Fields[i].Name] = val
				}
			}
			break
		}
	}

	return &EnumVal{TypeName: typeName, Variant: variant, Fields: fields}
}

func (interp *Interpreter) evalPipeline(name string) Value {
	decl, ok := interp.pipelineDecls[name]
	if !ok {
		return &NilVal{}
	}

	var sourceExpr parser.Expression
	var transforms []parser.Expression
	var sinkExpr parser.Expression
	var partitionCount int64
	hasOnError := false

	for _, stage := range decl.Stages {
		switch stage.Kind {
		case "source":
			sourceExpr = stage.Expr
		case "transform":
			transforms = append(transforms, stage.Expr)
		case "sink":
			sinkExpr = stage.Expr
		case "partition":
			pVal := interp.evalExpression(stage.Expr)
			if iv, ok := pVal.(*IntVal); ok {
				partitionCount = iv.V
			}
		case "on_error":
			hasOnError = true
		case "back_pressure":
		case "checkpoint":
		}
	}

	sourceVal := interp.evalExpression(sourceExpr)
	var items []Value
	if list, ok := sourceVal.(*ListVal); ok {
		items = list.Elements
	}

	if partitionCount > 1 && len(transforms) > 0 {
		items = interp.evalPartitionedPipeline(items, transforms, partitionCount)
	} else {
		for _, txExpr := range transforms {
			txFn := interp.evalExpression(txExpr)
			if hasOnError {
				items = interp.applyTransformWithErrorPolicy(items, txFn)
			} else {
				items = interp.applyTransform(items, txFn)
			}
		}
	}

	if sinkExpr != nil {
		sinkVal := interp.evalExpression(sinkExpr)
		if sink, ok := sinkVal.(*BuiltinVal); ok {
			result, err := sink.Fn(items)
			if err != nil {
				panic("runtime error: " + err.Error())
			}
			return result
		}
	}

	return &ListVal{Elements: items}
}

// applyTransformWithErrorPolicy unwraps Result.Ok and drops Result.Err (dead-letter).
func (interp *Interpreter) applyTransformWithErrorPolicy(items []Value, txFn Value) []Value {
	var result []Value
	for _, item := range items {
		val := interp.callTransformFn(txFn, item)
		if ev, ok := val.(*EnumVal); ok && ev.TypeName == "Result" {
			if ev.Variant == "Ok" {
				result = append(result, ev.Fields["value"])
			}
			continue
		}
		result = append(result, val)
	}
	return result
}

func (interp *Interpreter) applyTransform(items []Value, txFn Value) []Value {
	var result []Value
	for _, item := range items {
		val := interp.callTransformFn(txFn, item)
		if ev, ok := val.(*EnumVal); ok {
			if ev.TypeName == "Option" && ev.Variant == "None" {
				continue
			}
			if ev.TypeName == "Option" && ev.Variant == "Some" {
				if inner, exists := ev.Fields["value"]; exists {
					result = append(result, inner)
					continue
				}
			}
		}
		if _, ok := val.(*NilVal); ok {
			continue
		}
		result = append(result, val)
	}
	return result
}

func (interp *Interpreter) callTransformFn(fn Value, arg Value) Value {
	switch f := fn.(type) {
	case *FunctionVal:
		child := &Interpreter{
			env:             NewEnclosedEnvironment(f.Env),
			effectStack:     interp.effectStack,
			runtimeEffects:  interp.runtimeEffects,
			objectDecls:     interp.objectDecls,
			enumDecls:       interp.enumDecls,
			actorDecls:      interp.actorDecls,
			pipelineDecls:   interp.pipelineDecls,
			toolDecls:       interp.toolDecls,
			agentDecls:      interp.agentDecls,
			mcpDecls:        interp.mcpDecls,
			serviceDecls:    interp.serviceDecls,
			llmClient:       interp.llmClient,
			capabilityStack: interp.capabilityStack,
			fsSessionStack:  interp.fsSessionStack,
		}
		if len(f.Params) > 0 {
			child.env.Set(f.Params[0].Name, arg)
		}
		result := child.evalBlock(f.Body)
		if rs, ok := result.(*ReturnSignal); ok {
			return rs.Value
		}
		return result
	case *BuiltinVal:
		result, err := f.Fn([]Value{arg})
		if err != nil {
			panic("runtime error: " + err.Error())
		}
		return result
	}
	return &NilVal{}
}

func (interp *Interpreter) evalPartitionedPipeline(items []Value, transforms []parser.Expression, n int64) []Value {
	txFns := make([]Value, len(transforms))
	for i, txExpr := range transforms {
		txFns[i] = interp.evalExpression(txExpr)
	}

	inCh := make(chan Value, len(items))
	outCh := make(chan Value, len(items))

	for _, item := range items {
		inCh <- item
	}
	close(inCh)

	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			for item := range inCh {
				result := item
				for _, txFn := range txFns {
					result = interp.callTransformFn(txFn, result)
				}
				if ev, ok := result.(*EnumVal); ok {
					if ev.TypeName == "Option" && ev.Variant == "None" {
						continue
					}
					if ev.TypeName == "Option" && ev.Variant == "Some" {
						if inner, exists := ev.Fields["value"]; exists {
							outCh <- inner
							continue
						}
					}
				}
				if _, ok := result.(*NilVal); ok {
					continue
				}
				outCh <- result
			}
		})
	}

	go func() {
		wg.Wait()
		close(outCh)
	}()

	var results []Value
	for val := range outCh {
		results = append(results, val)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Inspect() < results[j].Inspect()
	})

	return results
}

func (interp *Interpreter) evalToolRun(name string, callArgs []parser.CallArg) Value {
	decl, ok := interp.toolDecls[name]
	if !ok || decl.RunFn == nil {
		return &NilVal{}
	}

	if decl.Capability != "" && !interp.hasCapability(decl.Capability) {
		return makeErrResult("capability_denied",
			"tool '"+name+"' requires capability '"+decl.Capability+"'")
	}

	inputFields := make(map[string]Value)
	for idx, input := range decl.Inputs {
		var val Value = &NilVal{}
		for _, arg := range callArgs {
			if arg.Name == input.Name {
				val = interp.evalExpression(arg.Value)
				break
			}
		}
		if _, isNil := val.(*NilVal); isNil && idx < len(callArgs) && callArgs[idx].Name == "" {
			val = interp.evalExpression(callArgs[idx].Value)
		}
		if _, isNil := val.(*NilVal); isNil && input.Default != nil {
			val = interp.evalExpression(input.Default)
		}
		inputFields[input.Name] = val
	}
	inputObj := &ObjectVal{TypeName: "Input", Fields: inputFields}

	fnEnv := NewEnclosedEnvironment(interp.env)
	fnEnv.Set("self", &NilVal{})
	fnEnv.Set("i", inputObj)

	prev := interp.env
	interp.env = fnEnv
	result := interp.evalBlock(decl.RunFn.Body)
	interp.env = prev

	if rs, ok := result.(*ReturnSignal); ok {
		result = rs.Value
	}
	return interp.validateToolOutput(decl, result)
}

func (interp *Interpreter) validateToolOutput(decl *parser.ToolDecl, result Value) Value {
	if len(decl.Outputs) == 0 {
		return result
	}
	if ev, ok := result.(*EnumVal); ok && ev.TypeName == "Result" && ev.Variant == "Err" {
		return ev
	}
	if ev, ok := result.(*EnumVal); ok && ev.TypeName == "Result" && ev.Variant == "Ok" {
		if inner, ok := ev.Fields["value"]; ok {
			result = inner
		}
	}
	ov, ok := result.(*ObjectVal)
	if !ok {
		return makeErrResult("tool_output_invalid",
			"tool '"+decl.Name+"' declared an output block but returned "+result.Type())
	}
	for _, f := range decl.Outputs {
		if _, present := ov.Fields[f.Name]; !present {
			return makeErrResult("tool_output_invalid",
				"tool '"+decl.Name+"' output is missing required field '"+f.Name+"'")
		}
	}
	ov.TypeName = decl.Name + ".Output"
	return ov
}

// evalToolRunFromValues is the value-call variant of evalToolRun.
// Args arrive pre-evaluated, so positional dispatch only.
func (interp *Interpreter) evalToolRunFromValues(name string, args []Value) Value {
	decl, ok := interp.toolDecls[name]
	if !ok || decl.RunFn == nil {
		return &NilVal{}
	}
	if decl.Capability != "" && !interp.hasCapability(decl.Capability) {
		return makeErrResult("capability_denied",
			"tool '"+name+"' requires capability '"+decl.Capability+"'")
	}

	inputFields := make(map[string]Value)
	for idx, input := range decl.Inputs {
		var val Value = &NilVal{}
		if idx < len(args) {
			val = args[idx]
		}
		if _, isNil := val.(*NilVal); isNil && input.Default != nil {
			val = interp.evalExpression(input.Default)
		}
		inputFields[input.Name] = val
	}
	inputObj := &ObjectVal{TypeName: "Input", Fields: inputFields}

	fnEnv := NewEnclosedEnvironment(interp.env)
	fnEnv.Set("self", &NilVal{})
	fnEnv.Set("i", inputObj)

	prev := interp.env
	interp.env = fnEnv
	result := interp.evalBlock(decl.RunFn.Body)
	interp.env = prev

	if rs, ok := result.(*ReturnSignal); ok {
		result = rs.Value
	}
	return interp.validateToolOutput(decl, result)
}

// evalToolOutputSchema returns the outputSchema JSON for `output { ... }`
// tools, or NilVal for `output: Type` tools.
func (interp *Interpreter) evalToolOutputSchema(name string) Value {
	decl, ok := interp.toolDecls[name]
	if !ok {
		return &NilVal{}
	}
	if len(decl.Outputs) == 0 {
		return &NilVal{}
	}
	adapter := &toolDeclAdapter{decl: decl, interp: interp}
	schema, err := tool.FromASTWithResolver(adapter, &interpTypeResolver{interp: interp})
	if err != nil {
		return &StringVal{V: fmt.Sprintf("error: %s", err)}
	}
	if schema.OutputSchema == nil {
		return &NilVal{}
	}
	data, err := json.Marshal(schema.OutputSchema)
	if err != nil {
		return &StringVal{V: fmt.Sprintf("error: %s", err)}
	}
	return &StringVal{V: string(data)}
}

// evalToolInputSchema returns just the inputSchema JSON without the
// surrounding tool envelope that `.schema()` includes.
func (interp *Interpreter) evalToolInputSchema(name string) Value {
	decl, ok := interp.toolDecls[name]
	if !ok {
		return &NilVal{}
	}
	adapter := &toolDeclAdapter{decl: decl, interp: interp}
	schema, err := tool.FromASTWithResolver(adapter, &interpTypeResolver{interp: interp})
	if err != nil {
		return &StringVal{V: fmt.Sprintf("error: %s", err)}
	}
	data, err := json.Marshal(schema.InputSchema)
	if err != nil {
		return &StringVal{V: fmt.Sprintf("error: %s", err)}
	}
	return &StringVal{V: string(data)}
}

func (interp *Interpreter) evalToolSchema(name string) Value {
	decl, ok := interp.toolDecls[name]
	if !ok {
		return &NilVal{}
	}

	adapter := &toolDeclAdapter{decl: decl, interp: interp}
	schema, err := tool.FromASTWithResolver(adapter, &interpTypeResolver{interp: interp})
	if err != nil {
		return &StringVal{V: fmt.Sprintf("error: %s", err)}
	}

	data, err := schema.ToMCPJSON()
	if err != nil {
		return &StringVal{V: fmt.Sprintf("error: %s", err)}
	}

	return &StringVal{V: string(data)}
}

type toolDeclAdapter struct {
	decl   *parser.ToolDecl
	interp *Interpreter
}

func (a *toolDeclAdapter) ToolName() string        { return a.decl.Name }
func (a *toolDeclAdapter) ToolDescription() string { return a.decl.Description }
func (a *toolDeclAdapter) ToolOutputType() string  { return a.decl.OutputType }

func (a *toolDeclAdapter) ToolInputs() []tool.FieldLike {
	fields := make([]tool.FieldLike, len(a.decl.Inputs))
	for i, f := range a.decl.Inputs {
		fields[i] = &fieldAdapter{field: f}
	}
	return fields
}

func (a *toolDeclAdapter) ToolOutputs() []tool.FieldLike {
	fields := make([]tool.FieldLike, len(a.decl.Outputs))
	for i, f := range a.decl.Outputs {
		fields[i] = &fieldAdapter{field: f}
	}
	return fields
}

type fieldAdapter struct {
	field parser.Field
}

func (f *fieldAdapter) FieldName() string     { return f.field.Name }
func (f *fieldAdapter) FieldTypeExpr() string { return f.field.TypeExpr }
func (f *fieldAdapter) FieldHasDefault() bool { return f.field.Default != nil }

func (f *fieldAdapter) FieldAnnotation() *tool.AnnotationLike {
	if f.field.Annotation != nil && f.field.Annotation.Name == "doc" {
		if len(f.field.Annotation.Args) > 0 {
			if sl, ok := f.field.Annotation.Args[0].(*parser.StringLiteral); ok {
				return &tool.AnnotationLike{Name: "doc", Value: sl.Value}
			}
		}
	}
	return nil
}

type interpTypeResolver struct {
	interp *Interpreter
}

func (r *interpTypeResolver) ResolveObject(name string) ([]tool.FieldLike, bool) {
	decl, ok := r.interp.objectDecls[name]
	if !ok {
		return nil, false
	}
	fields := make([]tool.FieldLike, len(decl.Fields))
	for i, f := range decl.Fields {
		fields[i] = &fieldAdapter{field: f}
	}
	return fields, true
}

func (r *interpTypeResolver) ResolveEnum(name string) (tool.EnumLike, bool) {
	decl, ok := r.interp.enumDecls[name]
	if !ok {
		return nil, false
	}
	return &enumDeclAdapter{decl: decl}, true
}

type enumDeclAdapter struct {
	decl *parser.EnumDecl
}

func (a *enumDeclAdapter) EnumName() string { return a.decl.Name }

func (a *enumDeclAdapter) EnumVariants() []tool.EnumVariantLike {
	out := make([]tool.EnumVariantLike, len(a.decl.Variants))
	for i, v := range a.decl.Variants {
		out[i] = &enumVariantAdapter{variant: v}
	}
	return out
}

type enumVariantAdapter struct {
	variant parser.EnumVariant
}

func (a *enumVariantAdapter) VariantName() string { return a.variant.Name }

func (a *enumVariantAdapter) VariantFields() []tool.FieldLike {
	out := make([]tool.FieldLike, len(a.variant.Fields))
	for i, f := range a.variant.Fields {
		out[i] = &fieldAdapter{field: f}
	}
	return out
}

// GetMCPDecls returns all MCP declarations collected during evaluation.
func (interp *Interpreter) GetMCPDecls() map[string]*parser.MCPDecl {
	return interp.mcpDecls
}

// GetToolDecls returns all tool declarations collected during evaluation.
func (interp *Interpreter) GetToolDecls() map[string]*parser.ToolDecl {
	return interp.toolDecls
}

// GetToolSchema generates a tool.ToolSchema from a named tool declaration.
func (interp *Interpreter) GetToolSchema(name string) *tool.ToolSchema {
	decl, ok := interp.toolDecls[name]
	if !ok {
		return nil
	}
	adapter := &toolDeclAdapter{decl: decl, interp: interp}
	schema, err := tool.FromASTWithResolver(adapter, &interpTypeResolver{interp: interp})
	if err != nil {
		return nil
	}
	return schema
}

// InvokeToolJSON invokes a tool by name with JSON arguments.
func (interp *Interpreter) InvokeToolJSON(name string, argsJSON json.RawMessage) (string, error) {
	decl, ok := interp.toolDecls[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	var argsMap map[string]any
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &argsMap); err != nil {
			return "", fmt.Errorf("invalid JSON arguments: %s", err)
		}
	}

	for _, input := range decl.Inputs {
		isOptional := strings.HasPrefix(input.TypeExpr, "Option[") || input.Default != nil
		if !isOptional {
			if _, exists := argsMap[input.Name]; !exists {
				return "", fmt.Errorf("missing required argument: %s", input.Name)
			}
		}
	}

	for _, input := range decl.Inputs {
		val, exists := argsMap[input.Name]
		if !exists {
			continue
		}
		if err := interp.validateArgType(val, input.TypeExpr); err != nil {
			return "", fmt.Errorf("type mismatch for '%s': %s", input.Name, err)
		}
	}

	var callArgs []parser.CallArg
	for _, input := range decl.Inputs {
		val, exists := argsMap[input.Name]
		if !exists {
			continue
		}
		expr := interp.jsonValueToAST(val, input.TypeExpr)
		if expr != nil {
			callArgs = append(callArgs, parser.CallArg{Name: input.Name, Value: expr})
		}
	}

	result := interp.evalToolRun(name, callArgs)
	return result.Inspect(), nil
}

func stripOption(typeExpr string) string {
	if strings.HasPrefix(typeExpr, "Option[") && strings.HasSuffix(typeExpr, "]") {
		return typeExpr[7 : len(typeExpr)-1]
	}
	return typeExpr
}

func listInner(typeExpr string) (string, bool) {
	if strings.HasPrefix(typeExpr, "[") && strings.HasSuffix(typeExpr, "]") {
		return typeExpr[1 : len(typeExpr)-1], true
	}
	return "", false
}

func (interp *Interpreter) validateArgType(val any, typeExpr string) error {
	if val == nil {
		return nil
	}
	typeExpr = stripOption(typeExpr)

	if inner, isList := listInner(typeExpr); isList {
		arr, ok := val.([]any)
		if !ok {
			return fmt.Errorf("expected list, got %T", val)
		}
		for i, elem := range arr {
			if err := interp.validateArgType(elem, inner); err != nil {
				return fmt.Errorf("element %d: %s", i, err)
			}
		}
		return nil
	}

	switch typeExpr {
	case "Int":
		if f, ok := val.(float64); ok {
			if f != math.Trunc(f) {
				return fmt.Errorf("expected Int, got Float")
			}
			return nil
		}
		return fmt.Errorf("expected Int, got %T", val)
	case "Float":
		if _, ok := val.(float64); ok {
			return nil
		}
		return fmt.Errorf("expected Float, got %T", val)
	case "String":
		if _, ok := val.(string); ok {
			return nil
		}
		return fmt.Errorf("expected String, got %T", val)
	case "Bool":
		if _, ok := val.(bool); ok {
			return nil
		}
		return fmt.Errorf("expected Bool, got %T", val)
	}

	if enumDecl, ok := interp.enumDecls[typeExpr]; ok {
		return interp.validateEnumArg(val, enumDecl)
	}

	if objDecl, ok := interp.objectDecls[typeExpr]; ok {
		return interp.validateObjectArg(val, objDecl)
	}

	return nil
}

func (interp *Interpreter) validateEnumArg(val any, decl *parser.EnumDecl) error {
	obj, ok := val.(map[string]any)
	if !ok {
		if s, isString := val.(string); isString {
			for _, v := range decl.Variants {
				if v.Name == s && len(v.Fields) == 0 {
					return nil
				}
			}
			return fmt.Errorf("unknown variant '%s' in enum '%s'", s, decl.Name)
		}
		return fmt.Errorf("expected object with 'kind' for enum '%s', got %T", decl.Name, val)
	}
	kindRaw, ok := obj[tool.DiscriminatorField]
	if !ok {
		return fmt.Errorf("missing 'kind' discriminator for enum '%s'", decl.Name)
	}
	kind, ok := kindRaw.(string)
	if !ok {
		return fmt.Errorf("'kind' must be a string for enum '%s', got %T", decl.Name, kindRaw)
	}
	for _, v := range decl.Variants {
		if v.Name != kind {
			continue
		}
		for _, f := range v.Fields {
			fv, has := obj[f.Name]
			if !has {
				if strings.HasPrefix(f.TypeExpr, "Option[") || f.Default != nil {
					continue
				}
				return fmt.Errorf("variant '%s.%s' missing field '%s'", decl.Name, kind, f.Name)
			}
			if err := interp.validateArgType(fv, f.TypeExpr); err != nil {
				return fmt.Errorf("variant '%s.%s' field '%s': %s", decl.Name, kind, f.Name, err)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown variant '%s' in enum '%s'", kind, decl.Name)
}

func (interp *Interpreter) validateObjectArg(val any, decl *parser.ObjectDecl) error {
	obj, ok := val.(map[string]any)
	if !ok {
		return fmt.Errorf("expected object for type '%s', got %T", decl.Name, val)
	}
	for _, f := range decl.Fields {
		fv, has := obj[f.Name]
		if !has {
			if strings.HasPrefix(f.TypeExpr, "Option[") || f.Default != nil {
				continue
			}
			return fmt.Errorf("type '%s' missing field '%s'", decl.Name, f.Name)
		}
		if err := interp.validateArgType(fv, f.TypeExpr); err != nil {
			return fmt.Errorf("field '%s': %s", f.Name, err)
		}
	}
	return nil
}

// jsonValueToAST lowers a JSON-decoded value into a parser.Expression.
// typeExpr guides reconstruction: typed objects/enums get re-tagged with
// their declared shape; untyped values become anonymous literals.
func (interp *Interpreter) jsonValueToAST(val any, typeExpr string) parser.Expression {
	typeExpr = stripOption(typeExpr)

	if val == nil {
		return &parser.NilLiteral{}
	}

	if inner, isList := listInner(typeExpr); isList {
		arr, ok := val.([]any)
		if !ok {
			return nil
		}
		elems := make([]parser.Expression, 0, len(arr))
		for _, e := range arr {
			if expr := interp.jsonValueToAST(e, inner); expr != nil {
				elems = append(elems, expr)
			}
		}
		return &parser.ListLiteral{Elements: elems}
	}

	if typeExpr != "" {
		if enumDecl, ok := interp.enumDecls[typeExpr]; ok {
			return interp.jsonValueToEnumAST(val, enumDecl)
		}
		if objDecl, ok := interp.objectDecls[typeExpr]; ok {
			return interp.jsonValueToObjectAST(val, objDecl)
		}
	}

	switch v := val.(type) {
	case float64:
		if v == math.Trunc(v) {
			return &parser.IntegerLiteral{Value: int64(v)}
		}
		return &parser.FloatLiteral{Value: v}
	case string:
		return &parser.StringLiteral{Value: v}
	case bool:
		return &parser.BooleanLiteral{Value: v}
	case []any:
		elems := make([]parser.Expression, 0, len(v))
		for _, e := range v {
			if expr := interp.jsonValueToAST(e, ""); expr != nil {
				elems = append(elems, expr)
			}
		}
		return &parser.ListLiteral{Elements: elems}
	case map[string]any:
		fields := make([]parser.ObjectLiteralField, 0, len(v))
		for k, fv := range v {
			if expr := interp.jsonValueToAST(fv, ""); expr != nil {
				fields = append(fields, parser.ObjectLiteralField{Name: k, Value: expr})
			}
		}
		return &parser.ObjectLiteral{Fields: fields}
	}
	return nil
}

func (interp *Interpreter) jsonValueToEnumAST(val any, decl *parser.EnumDecl) parser.Expression {
	if s, isString := val.(string); isString {
		return &parser.FieldAccessExpression{
			Left:  &parser.Identifier{Value: decl.Name},
			Field: s,
		}
	}
	obj, ok := val.(map[string]any)
	if !ok {
		return nil
	}
	kindRaw, ok := obj[tool.DiscriminatorField]
	if !ok {
		return nil
	}
	kind, ok := kindRaw.(string)
	if !ok {
		return nil
	}
	var variant *parser.EnumVariant
	for i := range decl.Variants {
		if decl.Variants[i].Name == kind {
			variant = &decl.Variants[i]
			break
		}
	}
	if variant == nil {
		return nil
	}

	args := make([]parser.CallArg, 0, len(variant.Fields))
	for _, f := range variant.Fields {
		fv, has := obj[f.Name]
		if !has {
			continue
		}
		if expr := interp.jsonValueToAST(fv, f.TypeExpr); expr != nil {
			args = append(args, parser.CallArg{Name: f.Name, Value: expr})
		}
	}

	return &parser.CallExpression{
		Function: &parser.FieldAccessExpression{
			Left:  &parser.Identifier{Value: decl.Name},
			Field: kind,
		},
		Args: args,
	}
}

func (interp *Interpreter) jsonValueToObjectAST(val any, decl *parser.ObjectDecl) parser.Expression {
	obj, ok := val.(map[string]any)
	if !ok {
		return nil
	}
	fields := make([]parser.ObjectLiteralField, 0, len(decl.Fields))
	for _, f := range decl.Fields {
		fv, has := obj[f.Name]
		if !has {
			continue
		}
		if expr := interp.jsonValueToAST(fv, f.TypeExpr); expr != nil {
			fields = append(fields, parser.ObjectLiteralField{Name: f.Name, Value: expr})
		}
	}
	return &parser.ObjectLiteral{Name: decl.Name, Fields: fields}
}

// GetServiceDecls returns all service declarations collected during evaluation.
func (interp *Interpreter) GetServiceDecls() map[string]*parser.ServiceDecl {
	return interp.serviceDecls
}

// Env returns the interpreter's environment for function lookup.
func (interp *Interpreter) Env() *Environment {
	return interp.env
}

// CallFunctionWithValues invokes a FunctionVal in a fresh child interpreter
// for safe concurrent invocation (e.g. HTTP request handlers).
func (interp *Interpreter) CallFunctionWithValues(fn *FunctionVal, args map[string]Value) (Value, error) {
	child := &Interpreter{
		env:             NewEnclosedEnvironment(fn.Env),
		effectStack:     interp.effectStack,
		runtimeEffects:  interp.runtimeEffects,
		objectDecls:     interp.objectDecls,
		enumDecls:       interp.enumDecls,
		actorDecls:      interp.actorDecls,
		pipelineDecls:   interp.pipelineDecls,
		toolDecls:       interp.toolDecls,
		agentDecls:      interp.agentDecls,
		mcpDecls:        interp.mcpDecls,
		serviceDecls:    interp.serviceDecls,
		llmClient:       interp.llmClient,
		capabilityStack: interp.capabilityStack,
		fsSessionStack:  interp.fsSessionStack,
	}
	child.registerBuiltins()

	for _, param := range fn.Params {
		if val, ok := args[param.Name]; ok {
			child.env.Set(param.Name, val)
		}
	}

	result := child.evalBlock(fn.Body)
	if rs, ok := result.(*ReturnSignal); ok {
		return rs.Value, nil
	}
	return result, nil
}
