package parser

import "github.com/adriangitvitz/yoru/lexer"

// Node is the base interface for all AST nodes.
type Node interface {
	TokenLiteral() string
}

// Statement is a node that doesn't produce a value.
type Statement interface {
	Node
	statementNode()
}

// Expression is a node that produces a value.
type Expression interface {
	Node
	expressionNode()
}

// Program is the root node of every AST.
type Program struct {
	Statements []Statement
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	}
	return ""
}

// DelegateField represents a delegate declaration inside an object body.
// delegate base: User  — forwards field/method access to the delegated object.
type DelegateField struct {
	Name     string // field name ("base")
	TypeExpr string // delegated type ("User")
}

// ObjectDecl: object User { id: String, email: String }
type ObjectDecl struct {
	Token     lexer.Token // the OBJECT token
	Name      string
	Fields    []Field
	Delegates []DelegateField
}

func (o *ObjectDecl) statementNode()       {}
func (o *ObjectDecl) TokenLiteral() string { return o.Token.Literal }

// Field is a named, typed field in an object, tool input, etc.
type Field struct {
	Name       string
	TypeExpr   string
	Capability string // iso/trn/ref/val/box/tag or ""
	Default    Expression
	Annotation *Annotation
}

// Annotation: @doc("...") or @name(args)
type Annotation struct {
	Name string
	Args []Expression
}

// EnumDecl: enum Role { Admin, User, Guest }
type EnumDecl struct {
	Token    lexer.Token // the ENUM token
	Name     string
	Variants []EnumVariant
}

func (e *EnumDecl) statementNode()       {}
func (e *EnumDecl) TokenLiteral() string { return e.Token.Literal }

// EnumVariant: Admin or Ok(value: T)
type EnumVariant struct {
	Name   string
	Fields []Field
}

// FnDecl: fn name(params) -> ReturnType effect [E] { body }
type FnDecl struct {
	Token      lexer.Token // the FN token
	Name       string
	Params     []Param
	ReturnType string
	Effects    []string
	Body       *BlockExpression
}

func (f *FnDecl) statementNode()       {}
func (f *FnDecl) TokenLiteral() string { return f.Token.Literal }

// Param is a function parameter.
type Param struct {
	Name       string
	TypeExpr   string
	Capability string
	Default    Expression
}

// ActorDecl: actor Counter { state count: Int = 0; receive Msg { ... } }
type ActorDecl struct {
	Token    lexer.Token // the ACTOR token
	Name     string
	States   []StateField
	Receives []ReceiveBlock
}

func (a *ActorDecl) statementNode()       {}
func (a *ActorDecl) TokenLiteral() string { return a.Token.Literal }

// StateField: state count: Int = 0
type StateField struct {
	Name     string
	TypeExpr string
	Default  Expression
}

// ReceiveBlock: receive Increment { self.count += 1 }
type ReceiveBlock struct {
	MessageType string
	Params      []Param
	ReturnType  string
	Body        *BlockExpression
}

// PipelineDecl: pipeline Double { source: ... |> transform: ... |> sink: ... }
type PipelineDecl struct {
	Token  lexer.Token // the PIPELINE token
	Name   string
	Stages []PipelineStage
}

func (p *PipelineDecl) statementNode()       {}
func (p *PipelineDecl) TokenLiteral() string { return p.Token.Literal }

// PipelineStage: source: expr, transform: expr, sink: expr, etc.
type PipelineStage struct {
	Kind string // "source", "transform", "sink", "partition", "on_error", "back_pressure", "checkpoint"
	Expr Expression
}

// ToolDecl: tool SearchOrders { description: "..." capability: .read_only input { ... } output: T ... }
type ToolDecl struct {
	Token       lexer.Token // the TOOL token
	Name        string
	Description string
	Capability  string // optional: "read_only", "write", etc.
	Inputs      []Field
	// Output: either OutputType is set (output: Type) or Outputs is populated
	// (output { fields }). Structured form drives runtime validation + MCP outputSchema.
	OutputType string
	Outputs    []Field
	Effects    []string
	RunFn      *FnDecl
}

func (t *ToolDecl) statementNode()       {}
func (t *ToolDecl) TokenLiteral() string { return t.Token.Literal }

// AgentDecl: agent MyAgent { model: "..." system: "..." tools: [T1, T2] config { ... } }
// Outputs (optional): replies must parse as JSON matching the schema; the
// runtime validates and re-tags as <Name>.Output. RetryInvalidOutput=0 falls
// back to AgentConfig default.
type AgentDecl struct {
	Token              lexer.Token // the AGENT token
	Name               string
	Model              string
	System             string
	Tools              []string // tool name references
	MaxTurns           int
	BudgetTokens       int
	Temperature        float64
	Outputs            []Field
	RetryInvalidOutput int
}

func (a *AgentDecl) statementNode()       {}
func (a *AgentDecl) TokenLiteral() string { return a.Token.Literal }

// MCPDecl: mcp MathServer { name: "math-server" version: "1.0.0" tools: [Add] resources: [...] auth: .api_key transport: .stdio }
type MCPDecl struct {
	Token      lexer.Token   // the MCP token
	Name       string        // declaration name: "MathServer"
	ServerName string        // string: "math-server"
	Version    string        // string: "1.0.0"
	Tools      []string      // tool references: ["Add", "Multiply"]
	Resources  []MCPResource // resource declarations
	Auth       string        // auth mode: "api_key", "jwt", ""
	AuthArgs   []CallArg     // optional named/positional args for parameterised auth
	// (e.g. `.jwt(secret: "...")` → AuthArgs = [{Name:"secret", Value:StringLiteral}])
	Transport string // "stdio" (from .stdio)
}

// MCPResource represents a resource declaration in an MCP server.
type MCPResource struct {
	Name    string // resource name
	URI     string // resource URI
	Content string // static content (for inline resources)
}

func (m *MCPDecl) statementNode()       {}
func (m *MCPDecl) TokenLiteral() string { return m.Token.Literal }

// ServiceDecl: service OrderAPI { prefix: "/v1" middleware: [Logger, CORS] GET "/orders" -> list_orders; transport: .http(port: 8080) }
type ServiceDecl struct {
	Token       lexer.Token // the SERVICE token
	Name        string
	Prefix      string          // optional route prefix (e.g. "/v1/users")
	Middlewares []MiddlewareRef // see MiddlewareRef
	Routes      []ServiceRoute
	Port        int    // default 8080
	Host        string // optional bind address; defaults to all interfaces ("")
}

// LeadingDotExpression is `.name` or `.name(args...)` in contextual slots
// where the enum type is inferred from position (pipeline `on_error`,
// `back_pressure`, mcp `auth:`, etc.). Materialized as a tagged ObjectVal
// { tag, args, named } for policy consumers to introspect.
type LeadingDotExpression struct {
	Token lexer.Token // the DOT token
	Name  string
	Args  []CallArg
}

func (l *LeadingDotExpression) expressionNode()      {}
func (l *LeadingDotExpression) TokenLiteral() string { return l.Token.Literal }

// MiddlewareRef is one entry in a service's `middleware: [...]` slot.
// Forms: Logger | CORS.allow_origin("*") | RateLimit.rps(100). The server's
// resolveMiddlewares dispatches on (Name, Method) and evaluates Args at
// server-construction time.
type MiddlewareRef struct {
	Name   string
	Method string
	Args   []Expression
}

func (s *ServiceDecl) statementNode()       {}
func (s *ServiceDecl) TokenLiteral() string { return s.Token.Literal }

// ServiceRoute: GET "/orders/:id" -> get_order  OR  GET "/hello" -> fn(req) { ... }
type ServiceRoute struct {
	Method        string  // GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS
	Pattern       string  // "/orders/:id"
	Handler       string  // function name reference (empty if InlineHandler is set)
	InlineHandler *FnDecl // inline handler function (nil if Handler is set)
}

// ProtocolDecl: protocol Printable { fn print(self) -> String }
type ProtocolDecl struct {
	Token   lexer.Token
	Name    string
	Methods []ProtocolMethod
}

func (p *ProtocolDecl) statementNode()       {}
func (p *ProtocolDecl) TokenLiteral() string { return p.Token.Literal }

// ProtocolMethod: fn name(params) -> ReturnType
type ProtocolMethod struct {
	Name       string
	Params     []Param
	ReturnType string
	Effects    []string
}

// ImplDecl: impl Protocol for Type { ... }
type ImplDecl struct {
	Token    lexer.Token
	Protocol string
	Target   string
	Methods  []*FnDecl
}

func (i *ImplDecl) statementNode()       {}
func (i *ImplDecl) TokenLiteral() string { return i.Token.Literal }

// EffectDecl: effect LLMCall { complete(prompt: String) -> String }
type EffectDecl struct {
	Token      lexer.Token
	Name       string
	Operations []EffectOperation
}

func (e *EffectDecl) statementNode()       {}
func (e *EffectDecl) TokenLiteral() string { return e.Token.Literal }

// EffectOperation represents an operation in a custom effect declaration.
type EffectOperation struct {
	Name       string
	Params     []Param
	ReturnType string
}

// ImportStatement: import "./path" | import "./path" as Name | import { a, b } from "./path"
type ImportStatement struct {
	Token lexer.Token // the IMPORT token
	Path  string      // "./path" string literal
	Alias string      // "as Name" alias (empty = none)
	Names []string    // selective { a, b } import (nil = import all)
}

func (i *ImportStatement) statementNode()       {}
func (i *ImportStatement) TokenLiteral() string { return i.Token.Literal }

// TypeAliasDecl: type Name = ExistingType
type TypeAliasDecl struct {
	Token    lexer.Token
	Name     string
	TypeExpr string
}

func (t *TypeAliasDecl) statementNode()       {}
func (t *TypeAliasDecl) TokenLiteral() string { return t.Token.Literal }

// LetStatement: let x: Type = expr
type LetStatement struct {
	Token      lexer.Token // LET token
	Name       string
	TypeExpr   string
	Capability string
	Value      Expression
}

func (l *LetStatement) statementNode()       {}
func (l *LetStatement) TokenLiteral() string { return l.Token.Literal }

// MutStatement: mut x: Type = expr
type MutStatement struct {
	Token      lexer.Token // MUT token
	Name       string
	TypeExpr   string
	Capability string
	Value      Expression
}

func (m *MutStatement) statementNode()       {}
func (m *MutStatement) TokenLiteral() string { return m.Token.Literal }

// ReturnStatement: return expr
type ReturnStatement struct {
	Token lexer.Token
	Value Expression
}

func (r *ReturnStatement) statementNode()       {}
func (r *ReturnStatement) TokenLiteral() string { return r.Token.Literal }

// ExpressionStatement wraps an expression used as a statement.
type ExpressionStatement struct {
	Token      lexer.Token
	Expression Expression
}

func (e *ExpressionStatement) statementNode()       {}
func (e *ExpressionStatement) TokenLiteral() string { return e.Token.Literal }

// BreakStatement: `break` — exits the innermost enclosing loop.
type BreakStatement struct {
	Token lexer.Token
}

func (b *BreakStatement) statementNode()       {}
func (b *BreakStatement) TokenLiteral() string { return b.Token.Literal }

// ContinueStatement: `continue` — skips to the next iteration of the
// innermost enclosing loop.
type ContinueStatement struct {
	Token lexer.Token
}

func (c *ContinueStatement) statementNode()       {}
func (c *ContinueStatement) TokenLiteral() string { return c.Token.Literal }

// Identifier: foo, x, counter
type Identifier struct {
	Token lexer.Token
	Value string
}

func (i *Identifier) expressionNode()      {}
func (i *Identifier) TokenLiteral() string { return i.Token.Literal }

// IntegerLiteral: 42
type IntegerLiteral struct {
	Token lexer.Token
	Value int64
}

func (il *IntegerLiteral) expressionNode()      {}
func (il *IntegerLiteral) TokenLiteral() string { return il.Token.Literal }

// FloatLiteral: 3.14
type FloatLiteral struct {
	Token lexer.Token
	Value float64
}

func (fl *FloatLiteral) expressionNode()      {}
func (fl *FloatLiteral) TokenLiteral() string { return fl.Token.Literal }

// StringLiteral: "hello"
type StringLiteral struct {
	Token lexer.Token
	Value string
}

func (sl *StringLiteral) expressionNode()      {}
func (sl *StringLiteral) TokenLiteral() string { return sl.Token.Literal }

// BooleanLiteral: true / false
type BooleanLiteral struct {
	Token lexer.Token
	Value bool
}

func (bl *BooleanLiteral) expressionNode()      {}
func (bl *BooleanLiteral) TokenLiteral() string { return bl.Token.Literal }

// NilLiteral: nil
type NilLiteral struct {
	Token lexer.Token
}

func (nl *NilLiteral) expressionNode()      {}
func (nl *NilLiteral) TokenLiteral() string { return nl.Token.Literal }

// SelfExpression: self
type SelfExpression struct {
	Token lexer.Token
}

func (se *SelfExpression) expressionNode()      {}
func (se *SelfExpression) TokenLiteral() string { return se.Token.Literal }

// PrefixExpression: !x, -x
type PrefixExpression struct {
	Token    lexer.Token
	Operator string
	Right    Expression
}

func (pe *PrefixExpression) expressionNode()      {}
func (pe *PrefixExpression) TokenLiteral() string { return pe.Token.Literal }

// InfixExpression: a + b, a == b, a <- b
type InfixExpression struct {
	Token    lexer.Token
	Left     Expression
	Operator string
	Right    Expression
}

func (ie *InfixExpression) expressionNode()      {}
func (ie *InfixExpression) TokenLiteral() string { return ie.Token.Literal }

// PostfixExpression: expr?
type PostfixExpression struct {
	Token    lexer.Token
	Left     Expression
	Operator string
}

func (pe *PostfixExpression) expressionNode()      {}
func (pe *PostfixExpression) TokenLiteral() string { return pe.Token.Literal }

// CallExpression: foo(a, b) or foo(name: value)
type CallExpression struct {
	Token    lexer.Token // the LPAREN token
	Function Expression
	Args     []CallArg
}

func (ce *CallExpression) expressionNode()      {}
func (ce *CallExpression) TokenLiteral() string { return ce.Token.Literal }

// CallArg supports both positional and named arguments.
type CallArg struct {
	Name  string // "" for positional args
	Value Expression
}

// FieldAccessExpression: obj.field
type FieldAccessExpression struct {
	Token lexer.Token // the DOT token
	Left  Expression
	Field string
}

func (fa *FieldAccessExpression) expressionNode()      {}
func (fa *FieldAccessExpression) TokenLiteral() string { return fa.Token.Literal }

// IndexExpression: arr[0]
type IndexExpression struct {
	Token lexer.Token // the LBRACKET token
	Left  Expression
	Index Expression
}

func (ie *IndexExpression) expressionNode()      {}
func (ie *IndexExpression) TokenLiteral() string { return ie.Token.Literal }

// ListLiteral: [1, 2, 3]
type ListLiteral struct {
	Token    lexer.Token // the LBRACKET token
	Elements []Expression
}

func (ll *ListLiteral) expressionNode()      {}
func (ll *ListLiteral) TokenLiteral() string { return ll.Token.Literal }

// BlockExpression: { stmt1; stmt2; expr }
type BlockExpression struct {
	Token      lexer.Token // the LBRACE token
	Statements []Statement
}

func (be *BlockExpression) expressionNode()      {}
func (be *BlockExpression) TokenLiteral() string { return be.Token.Literal }

// IfExpression: if cond { ... } else { ... }
type IfExpression struct {
	Token       lexer.Token
	Condition   Expression
	Consequence *BlockExpression
	Alternative *BlockExpression
}

func (ie *IfExpression) expressionNode()      {}
func (ie *IfExpression) TokenLiteral() string { return ie.Token.Literal }

// MatchExpression: match expr { pattern => body, ... }
type MatchExpression struct {
	Token   lexer.Token
	Subject Expression
	Arms    []MatchArm
}

func (me *MatchExpression) expressionNode()      {}
func (me *MatchExpression) TokenLiteral() string { return me.Token.Literal }

// MatchArm: pattern [if guard] => expression
type MatchArm struct {
	Pattern MatchPattern
	Guard   Expression // optional; non-nil when the arm has `if cond`
	Body    Expression
}

// MatchPattern: identifier, literal, destructure, object_destructure, or wildcard.
type MatchPattern struct {
	// Kind: "identifier" (Role.Admin), "destructure" (Ok(user)),
	//       "object_destructure" (Point { x, y } or Point { x: 0 }),
	//       "literal" (0, "hello"), "wildcard" (_)
	Kind string
	// Value: the matched value string (e.g. "Role.Admin", "_", "0", "Point")
	Value string
	// Bindings: positional destructured names (e.g. ["user"] for Ok(user))
	Bindings []string
	// ObjectFields: named field patterns for object_destructure
	ObjectFields []ObjectFieldPattern
}

// ObjectFieldPattern is one field inside an object_destructure pattern.
// `{ x }` -> Subpattern nil (shorthand bind); `{ x: 0 }` or `{ x: bound }`
// -> Subpattern set to the nested literal/identifier pattern.
type ObjectFieldPattern struct {
	Field      string
	Subpattern *MatchPattern // nil = shorthand: bind the field to a variable named after the field
}

// LambdaExpression: fn(params) -> ReturnType { body }
type LambdaExpression struct {
	Token      lexer.Token // the FN token
	Params     []Param
	ReturnType string
	Body       *BlockExpression
}

func (le *LambdaExpression) expressionNode()      {}
func (le *LambdaExpression) TokenLiteral() string { return le.Token.Literal }

// SpawnExpression: spawn Counter()
type SpawnExpression struct {
	Token lexer.Token // the SPAWN token
	Call  Expression
}

func (se *SpawnExpression) expressionNode()      {}
func (se *SpawnExpression) TokenLiteral() string { return se.Token.Literal }

// HandleExpression: handle(Effect) { using: handler } in { body }
type HandleExpression struct {
	Token      lexer.Token // the HANDLE token
	EffectName string
	Handler    Expression
	Body       *BlockExpression
}

func (he *HandleExpression) expressionNode()      {}
func (he *HandleExpression) TokenLiteral() string { return he.Token.Literal }

// AssignmentExpression: x = expr, x += expr, x -= expr
type AssignmentExpression struct {
	Token    lexer.Token
	Left     Expression
	Operator string
	Value    Expression
}

func (ae *AssignmentExpression) expressionNode()      {}
func (ae *AssignmentExpression) TokenLiteral() string { return ae.Token.Literal }

// ObjectLiteral: User { id: "1", email: "a@b.com" }
type ObjectLiteral struct {
	Token  lexer.Token
	Name   string
	Fields []ObjectLiteralField
}

func (ol *ObjectLiteral) expressionNode()      {}
func (ol *ObjectLiteral) TokenLiteral() string { return ol.Token.Literal }

// ObjectLiteralField: name: value
type ObjectLiteralField struct {
	Name  string
	Value Expression
}

// ForInExpression: for x in [1, 2, 3] { ... }
type ForInExpression struct {
	Token    lexer.Token // the FOR token
	Variable string      // loop variable name ("x", "_")
	Iterable Expression  // the collection expression
	Body     *BlockExpression
}

func (f *ForInExpression) expressionNode()      {}
func (f *ForInExpression) TokenLiteral() string { return f.Token.Literal }

// WhileExpression: while condition { body }
type WhileExpression struct {
	Token     lexer.Token // the WHILE token
	Condition Expression
	Body      *BlockExpression
}

func (w *WhileExpression) expressionNode()      {}
func (w *WhileExpression) TokenLiteral() string { return w.Token.Literal }

// SpreadExpression: ...expr — used in list literals to splice a list
type SpreadExpression struct {
	Token lexer.Token // the SPREAD token
	Value Expression
}

func (se *SpreadExpression) expressionNode()      {}
func (se *SpreadExpression) TokenLiteral() string { return se.Token.Literal }

// SuperExpression: super — references the delegate (parent) object
type SuperExpression struct {
	Token lexer.Token
}

func (se *SuperExpression) expressionNode()      {}
func (se *SuperExpression) TokenLiteral() string { return se.Token.Literal }

// LetDestructureStatement: let [a, b, ...rest] = expr
type LetDestructureStatement struct {
	Token   lexer.Token // LET token
	Pattern DestructurePattern
	Value   Expression
}

func (ld *LetDestructureStatement) statementNode()       {}
func (ld *LetDestructureStatement) TokenLiteral() string { return ld.Token.Literal }

// DestructurePattern: [a, b, ...rest]
type DestructurePattern struct {
	Kind     string   // "list"
	Names    []string // positional names
	RestName string   // rest capture name ("" if no rest)
	RestIdx  int      // index where rest starts
}

// ExportStatement: export <inner statement>
type ExportStatement struct {
	Token lexer.Token // EXPORT token
	Inner Statement
}

func (es *ExportStatement) statementNode()       {}
func (es *ExportStatement) TokenLiteral() string { return es.Token.Literal }
