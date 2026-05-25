package typechecker

import (
	"fmt"
	"maps"
	"strings"

	"github.com/adriangitvitz/yoru/lexer"
	"github.com/adriangitvitz/yoru/parser"
)

// CheckResult holds the outcome of type checking.
type CheckResult struct {
	Errors []string
}

// CheckSource parses and type-checks a Yoru source string.
func CheckSource(src string) *CheckResult {
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		result := &CheckResult{}
		for _, e := range p.Errors() {
			result.Errors = append(result.Errors, "parse error: "+e)
		}
		return result
	}
	c := NewChecker()
	return c.Check(prog)
}

// Binding represents a variable in scope.
type Binding struct {
	Type       Type
	Capability string // iso/trn/ref/val/box/tag or ""
	Mutable    bool
	Consumed   bool
}

// Scope is a lexical scope with parent chain.
type Scope struct {
	parent   *Scope
	bindings map[string]*Binding
}

func newScope(parent *Scope) *Scope {
	return &Scope{parent: parent, bindings: make(map[string]*Binding)}
}

func (s *Scope) define(name string, b *Binding) {
	s.bindings[name] = b
}

func (s *Scope) lookup(name string) (*Binding, bool) {
	if b, ok := s.bindings[name]; ok {
		return b, true
	}
	if s.parent != nil {
		return s.parent.lookup(name)
	}
	return nil, false
}

func (s *Scope) localLookup(name string) (*Binding, bool) {
	b, ok := s.bindings[name]
	return b, ok
}

// Checker walks the AST and enforces type, capability, and effect rules.
type Checker struct {
	globalTypes  map[string]Type
	globalFns    map[string]*FnType
	globalActors map[string]*ActorType
	scope        *Scope
	errors       []string

	// Function context
	currentFnName    string
	currentFnReturn  Type
	currentFnEffects []string
	usedEffects      map[string]bool
	handledEffects   map[string]bool
	insideActor      bool
	currentActorName string
	knownFnEffects   map[string][]string // fn name -> declared effects
	bodyHasReturn    bool

	// Per-Checker to avoid races across concurrent type checks.
	knownEffects map[string]bool
}

// defaultKnownEffects returns a fresh map of built-in effect namespaces.
func defaultKnownEffects() map[string]bool {
	return map[string]bool{
		"HTTP": true, "DB": true, "IO": true, "LLM": true, "Log": true,
		"Agent": true, "Stream": true, "Spawn": true, "Metric": true, "Clock": true,
		"Crypto": true, "Time": true, "JSON": true, "Redis": true, "Rabbit": true, "SQS": true, "Kafka": true,
		"Subprocess": true,
	}
}

// Builtin type names.
var builtinTypes = map[string]Type{
	"Int":    TypeInt,
	"Float":  TypeFloat,
	"String": TypeString,
	"Bool":   TypeBool,
	"Nil":    TypeNil,
	"Void":   TypeVoid,
	"Bytes":  &BuiltinType{Name: "Bytes"},
}

// NewChecker returns a Checker with its own knownEffects map.
func NewChecker() *Checker {
	return &Checker{
		globalTypes:    make(map[string]Type),
		globalFns:      make(map[string]*FnType),
		globalActors:   make(map[string]*ActorType),
		scope:          newScope(nil),
		knownFnEffects: make(map[string][]string),
		usedEffects:    make(map[string]bool),
		handledEffects: make(map[string]bool),
		knownEffects:   defaultKnownEffects(),
	}
}

func (c *Checker) addError(msg string) {
	c.errors = append(c.errors, msg)
}

func (c *Checker) pushScope() {
	c.scope = newScope(c.scope)
}

func (c *Checker) popScope() {
	c.scope = c.scope.parent
}

func (c *Checker) resolveType(typeExpr string) Type {
	if typeExpr == "" {
		return TypeVoid
	}
	if t, ok := builtinTypes[typeExpr]; ok {
		return t
	}
	if t, ok := c.globalTypes[typeExpr]; ok {
		return t
	}
	// List shorthand: [T]
	if strings.HasPrefix(typeExpr, "[") && strings.HasSuffix(typeExpr, "]") {
		inner := typeExpr[1 : len(typeExpr)-1]
		elem := c.resolveType(inner)
		if elem != nil {
			return &ListType{Element: elem}
		}
	}
	// Generic: Name[T, U]
	if idx := strings.Index(typeExpr, "["); idx > 0 {
		return &BuiltinType{Name: typeExpr}
	}
	return nil
}

// typesMatch returns true if two types are compatible.
// UnknownType matches anything.
func typesMatch(a, b Type) bool {
	if a == nil || b == nil {
		return true
	}
	if _, ok := a.(*UnknownType); ok {
		return true
	}
	if _, ok := b.(*UnknownType); ok {
		return true
	}
	return a.TypeName() == b.TypeName()
}

// Check performs two-pass type checking on a program.
func (c *Checker) Check(prog *parser.Program) *CheckResult {
	c.collectDeclarations(prog)
	c.checkBodies(prog)
	return &CheckResult{Errors: c.errors}
}

// Pass 1: gather every top-level decl before any body is checked so that
// forward references between functions, actors, and objects type-check.
func (c *Checker) collectDeclarations(prog *parser.Program) {
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.ObjectDecl:
			c.collectObjectDecl(s)
		case *parser.EnumDecl:
			c.collectEnumDecl(s)
		case *parser.FnDecl:
			c.collectFnDecl(s)
		case *parser.ActorDecl:
			c.collectActorDecl(s)
		case *parser.AgentDecl:
			// Register as a minimal ActorType so `spawn AgentName()` checks.
			c.globalActors[s.Name] = &ActorType{Name: s.Name}
		case *parser.EffectDecl:
			c.knownEffects[s.Name] = true
		case *parser.ExportStatement:
			switch inner := s.Inner.(type) {
			case *parser.ObjectDecl:
				c.collectObjectDecl(inner)
			case *parser.EnumDecl:
				c.collectEnumDecl(inner)
			case *parser.FnDecl:
				c.collectFnDecl(inner)
			case *parser.ActorDecl:
				c.collectActorDecl(inner)
			}
		case *parser.ImportStatement:
			// no-op: imports resolved by interpreter
		}
	}
}

func (c *Checker) collectObjectDecl(decl *parser.ObjectDecl) {
	if _, exists := c.globalTypes[decl.Name]; exists {
		c.addError(fmt.Sprintf("type '%s' already declared", decl.Name))
		return
	}
	obj := &ObjectType{Name: decl.Name, Fields: make(map[string]Type)}
	c.globalTypes[decl.Name] = obj
}

func (c *Checker) collectEnumDecl(decl *parser.EnumDecl) {
	if _, exists := c.globalTypes[decl.Name]; exists {
		c.addError(fmt.Sprintf("type '%s' already declared", decl.Name))
		return
	}
	c.globalTypes[decl.Name] = &EnumType{Name: decl.Name, Variants: make(map[string][]Type)}
}

func (c *Checker) collectFnDecl(decl *parser.FnDecl) {
	ft := &FnType{
		Name:       decl.Name,
		ReturnType: c.resolveType(decl.ReturnType),
		Effects:    decl.Effects,
	}
	for _, p := range decl.Params {
		ft.Params = append(ft.Params, c.resolveType(p.TypeExpr))
	}
	c.globalFns[decl.Name] = ft
	c.knownFnEffects[decl.Name] = decl.Effects
}

func (c *Checker) collectActorDecl(decl *parser.ActorDecl) {
	if _, exists := c.globalTypes[decl.Name]; exists {
		c.addError(fmt.Sprintf("type '%s' already declared", decl.Name))
		return
	}
	actor := &ActorType{
		Name:     decl.Name,
		States:   make(map[string]Type),
		Receives: make(map[string]*ReceiveType),
	}
	for _, sf := range decl.States {
		actor.States[sf.Name] = c.resolveType(sf.TypeExpr)
	}
	for _, rb := range decl.Receives {
		rt := &ReceiveType{
			Params:     make(map[string]Type),
			ReturnType: c.resolveType(rb.ReturnType),
		}
		for _, p := range rb.Params {
			rt.Params[p.Name] = c.resolveType(p.TypeExpr)
		}
		actor.Receives[rb.MessageType] = rt
	}
	c.globalTypes[decl.Name] = actor
	c.globalActors[decl.Name] = actor
}

// Pass 2 walks bodies; globalTypes/globalActors are already populated by Pass 1.
func (c *Checker) checkBodies(prog *parser.Program) {
	// Pre-register top-level lets/muts as TypeUnknown so hoisted function
	// bodies can resolve them — runtime is dynamic enough to compensate.
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			c.scope.define(s.Name, &Binding{Type: TypeUnknown, Capability: "val"})
		case *parser.MutStatement:
			c.scope.define(s.Name, &Binding{Type: TypeUnknown, Capability: "ref"})
		}
	}

	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.ObjectDecl:
			c.checkObjectFields(s)
		case *parser.EnumDecl:
			c.checkEnumVariants(s)
		case *parser.FnDecl:
			c.checkFnDecl(s)
		case *parser.ActorDecl:
			c.checkActorDecl(s)
		case *parser.ExportStatement:
			switch inner := s.Inner.(type) {
			case *parser.ObjectDecl:
				c.checkObjectFields(inner)
			case *parser.EnumDecl:
				c.checkEnumVariants(inner)
			case *parser.FnDecl:
				c.checkFnDecl(inner)
			case *parser.ActorDecl:
				c.checkActorDecl(inner)
			}
		case *parser.ImportStatement:
			// no-op: imports resolved by interpreter
		}
	}
}

func (c *Checker) checkObjectFields(decl *parser.ObjectDecl) {
	for _, f := range decl.Fields {
		if c.resolveType(f.TypeExpr) == nil {
			c.addError(fmt.Sprintf("unknown type '%s'", f.TypeExpr))
		}
	}
}

func (c *Checker) checkEnumVariants(decl *parser.EnumDecl) {
	for _, v := range decl.Variants {
		for _, f := range v.Fields {
			if c.resolveType(f.TypeExpr) == nil {
				c.addError(fmt.Sprintf("unknown type '%s'", f.TypeExpr))
			}
		}
	}
}

func (c *Checker) checkFnDecl(decl *parser.FnDecl) {
	c.pushScope()
	defer c.popScope()

	prevFnName := c.currentFnName
	prevFnReturn := c.currentFnReturn
	prevFnEffects := c.currentFnEffects
	prevUsed := c.usedEffects
	prevHandled := c.handledEffects
	prevReturn := c.bodyHasReturn

	c.currentFnName = decl.Name
	c.currentFnReturn = c.resolveType(decl.ReturnType)
	c.currentFnEffects = decl.Effects
	c.usedEffects = make(map[string]bool)
	c.handledEffects = make(map[string]bool)
	c.bodyHasReturn = false

	for _, p := range decl.Params {
		if p.Name == "self" {
			continue
		}
		c.scope.define(p.Name, &Binding{
			Type:       c.resolveType(p.TypeExpr),
			Capability: capOrDefault(p.Capability),
		})
	}

	bodyType := c.checkBlock(decl.Body)

	if decl.ReturnType != "" {
		if bodyType == nil || bodyType == TypeVoid {
			c.addError(fmt.Sprintf("must return %s but body may not return a value", decl.ReturnType))
		} else if !typesMatch(c.currentFnReturn, bodyType) {
			c.addError(fmt.Sprintf("return type mismatch: expected %s, got %s",
				c.currentFnReturn.TypeName(), bodyType.TypeName()))
		}
	}

	c.checkEffects(decl.Name, decl.Effects)

	c.currentFnName = prevFnName
	c.currentFnReturn = prevFnReturn
	c.currentFnEffects = prevFnEffects
	c.usedEffects = prevUsed
	c.handledEffects = prevHandled
	c.bodyHasReturn = prevReturn
}

func (c *Checker) checkActorDecl(decl *parser.ActorDecl) {
	prevInsideActor := c.insideActor
	prevActorName := c.currentActorName
	c.insideActor = true
	c.currentActorName = decl.Name
	defer func() {
		c.insideActor = prevInsideActor
		c.currentActorName = prevActorName
	}()

	actor := c.globalActors[decl.Name]

	for _, sf := range decl.States {
		if sf.Default != nil {
			defaultType := c.inferExprType(sf.Default)
			stateType := actor.States[sf.Name]
			if !typesMatch(stateType, defaultType) {
				c.addError(fmt.Sprintf("type mismatch in state '%s': expected %s, got %s",
					sf.Name, stateType.TypeName(), defaultType.TypeName()))
			}
		}
	}

	for _, rb := range decl.Receives {
		c.checkReceiveBlock(&rb, actor)
	}
}

func (c *Checker) checkReceiveBlock(rb *parser.ReceiveBlock, actor *ActorType) {
	c.pushScope()
	defer c.popScope()

	// Bind self as ref to current actor
	c.scope.define("self", &Binding{
		Type:       actor,
		Capability: "ref",
	})

	for _, p := range rb.Params {
		c.scope.define(p.Name, &Binding{
			Type:       c.resolveType(p.TypeExpr),
			Capability: capOrDefault(p.Capability),
		})
	}

	bodyType := c.checkBlock(rb.Body)

	if rb.ReturnType != "" {
		declaredReturn := c.resolveType(rb.ReturnType)
		if bodyType != nil && declaredReturn != nil && !typesMatch(declaredReturn, bodyType) {
			c.addError(fmt.Sprintf("receive '%s' return type mismatch: expected %s, got %s",
				rb.MessageType, declaredReturn.TypeName(), bodyType.TypeName()))
		}
	}
}

// checkBlock processes a block expression and returns the type of its final expression.
func (c *Checker) checkBlock(block *parser.BlockExpression) Type {
	if block == nil {
		return TypeVoid
	}
	var lastType Type
	for _, stmt := range block.Statements {
		lastType = c.checkStatement(stmt)
	}
	return lastType
}

func (c *Checker) checkStatement(stmt parser.Statement) Type {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		return c.checkLetStatement(s)
	case *parser.MutStatement:
		return c.checkMutStatement(s)
	case *parser.ReturnStatement:
		return c.checkReturnStatement(s)
	case *parser.ExpressionStatement:
		return c.inferExprType(s.Expression)
	case *parser.ImportStatement:
		return TypeVoid
	case *parser.LetDestructureStatement:
		// Infer value but bind destructured names as Unknown.
		c.inferExprType(s.Value)
		for _, name := range s.Pattern.Names {
			c.scope.define(name, &Binding{Type: TypeUnknown, Capability: "ref"})
		}
		if s.Pattern.RestName != "" {
			c.scope.define(s.Pattern.RestName, &Binding{Type: TypeUnknown, Capability: "ref"})
		}
		return TypeVoid
	case *parser.ExportStatement:
		return c.checkStatement(s.Inner)
	}
	return TypeVoid
}

func (c *Checker) checkLetStatement(stmt *parser.LetStatement) Type {
	if _, exists := c.scope.localLookup(stmt.Name); exists {
		c.addError(fmt.Sprintf("'%s' already declared in this scope", stmt.Name))
		return TypeVoid
	}

	// Check iso aliasing: if value is an identifier referencing an iso binding
	c.checkLetValueIso(stmt.Value)

	valueType := c.inferExprType(stmt.Value)
	cap := capOrDefault(stmt.Capability)

	if stmt.TypeExpr != "" {
		declaredType := c.resolveType(stmt.TypeExpr)
		if !typesMatch(declaredType, valueType) {
			c.addError(fmt.Sprintf("type mismatch: expected %s, got %s",
				declaredType.TypeName(), valueType.TypeName()))
		}
		c.scope.define(stmt.Name, &Binding{
			Type:       declaredType,
			Capability: cap,
		})
	} else {
		c.scope.define(stmt.Name, &Binding{
			Type:       valueType,
			Capability: cap,
		})
	}
	return TypeVoid
}

// checkLetValueIso checks if a let statement's value tries to alias an iso variable.
func (c *Checker) checkLetValueIso(expr parser.Expression) {
	ident, ok := expr.(*parser.Identifier)
	if !ok {
		return
	}
	b, found := c.scope.lookup(ident.Value)
	if !found {
		return
	}
	if b.Capability == "iso" {
		if b.Consumed {
			c.addError(fmt.Sprintf("iso variable '%s' was already consumed", ident.Value))
		} else {
			c.addError(fmt.Sprintf("iso variable '%s' cannot be aliased", ident.Value))
		}
	}
}

func (c *Checker) checkMutStatement(stmt *parser.MutStatement) Type {
	if _, exists := c.scope.localLookup(stmt.Name); exists {
		c.addError(fmt.Sprintf("'%s' already declared in this scope", stmt.Name))
		return TypeVoid
	}

	valueType := c.inferExprType(stmt.Value)
	cap := capOrDefault(stmt.Capability)

	if stmt.TypeExpr != "" {
		declaredType := c.resolveType(stmt.TypeExpr)
		if !typesMatch(declaredType, valueType) {
			c.addError(fmt.Sprintf("type mismatch: expected %s, got %s",
				declaredType.TypeName(), valueType.TypeName()))
		}
		c.scope.define(stmt.Name, &Binding{
			Type:       declaredType,
			Capability: cap,
			Mutable:    true,
		})
	} else {
		c.scope.define(stmt.Name, &Binding{
			Type:       valueType,
			Capability: cap,
			Mutable:    true,
		})
	}
	return TypeVoid
}

func (c *Checker) checkReturnStatement(stmt *parser.ReturnStatement) Type {
	c.bodyHasReturn = true
	if stmt.Value != nil {
		return c.inferExprType(stmt.Value)
	}
	return TypeVoid
}

func (c *Checker) inferExprType(expr parser.Expression) Type {
	if expr == nil {
		return TypeVoid
	}
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return TypeInt
	case *parser.FloatLiteral:
		return TypeFloat
	case *parser.StringLiteral:
		return TypeString
	case *parser.BooleanLiteral:
		return TypeBool
	case *parser.NilLiteral:
		return TypeNil
	case *parser.Identifier:
		return c.inferIdentifier(e)
	case *parser.SelfExpression:
		if b, ok := c.scope.lookup("self"); ok {
			return b.Type
		}
		return TypeVoid
	case *parser.PrefixExpression:
		return c.inferExprType(e.Right)
	case *parser.InfixExpression:
		return c.inferInfix(e)
	case *parser.PostfixExpression:
		return c.inferExprType(e.Left)
	case *parser.CallExpression:
		return c.inferCall(e)
	case *parser.FieldAccessExpression:
		return c.inferFieldAccess(e)
	case *parser.AssignmentExpression:
		return c.checkAssignment(e)
	case *parser.SpawnExpression:
		return c.checkSpawn(e)
	case *parser.HandleExpression:
		return c.checkHandle(e)
	case *parser.IfExpression:
		return c.checkIf(e)
	case *parser.MatchExpression:
		return c.checkMatch(e)
	case *parser.BlockExpression:
		c.pushScope()
		t := c.checkBlock(e)
		c.popScope()
		return t
	case *parser.ObjectLiteral:
		return c.checkObjectLiteral(e)
	case *parser.LambdaExpression:
		return c.checkLambda(e)
	case *parser.ListLiteral:
		if len(e.Elements) == 0 {
			return &UnknownType{} // empty list matches any list type
		}
		var elemType Type
		for _, el := range e.Elements {
			elemType = c.inferExprType(el)
		}
		return &ListType{Element: elemType}
	case *parser.IndexExpression:
		c.inferExprType(e.Left)
		c.inferExprType(e.Index)
		return TypeUnknown
	case *parser.ForInExpression:
		c.inferExprType(e.Iterable)
		c.pushScope()
		if e.Variable != "_" {
			c.scope.define(e.Variable, &Binding{Type: TypeUnknown, Capability: "ref", Mutable: true})
		}
		c.checkBlock(e.Body)
		c.popScope()
		return TypeVoid
	case *parser.WhileExpression:
		c.inferExprType(e.Condition)
		c.pushScope()
		c.checkBlock(e.Body)
		c.popScope()
		return TypeVoid
	case *parser.SuperExpression:
		return TypeUnknown
	case *parser.SpreadExpression:
		return c.inferExprType(e.Value)
	}
	return TypeVoid
}

func (c *Checker) inferIdentifier(ident *parser.Identifier) Type {
	// Known effect namespace (HTTP, DB, etc.)
	if c.knownEffects[ident.Value] {
		return &EffectNamespaceType{Name: ident.Value}
	}
	b, ok := c.scope.lookup(ident.Value)
	if !ok {
		// Uppercase unknowns are treated as external type/constructor refs.
		if len(ident.Value) > 0 && ident.Value[0] >= 'A' && ident.Value[0] <= 'Z' {
			return TypeUnknown
		}
		c.addError(fmt.Sprintf("undefined variable '%s'", ident.Value))
		return TypeVoid
	}
	return b.Type
}

func (c *Checker) inferInfix(expr *parser.InfixExpression) Type {
	// Send expression: target <- message
	if expr.Operator == "<-" {
		c.checkSendExpression(expr)
		return TypeVoid
	}

	leftType := c.inferExprType(expr.Left)
	rightType := c.inferExprType(expr.Right)

	switch expr.Operator {
	case "+", "-", "*", "/", "%":
		if leftType != nil && leftType != TypeVoid {
			return leftType
		}
		return rightType
	case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
		return TypeBool
	}
	return leftType
}

func (c *Checker) inferCall(expr *parser.CallExpression) Type {
	for _, arg := range expr.Args {
		c.inferExprType(arg.Value)
	}

	switch fn := expr.Function.(type) {
	case *parser.Identifier:
		return c.inferFnCall(fn.Value)
	case *parser.FieldAccessExpression:
		return c.inferMethodCall(fn)
	}

	return TypeUnknown
}

func (c *Checker) inferFnCall(fnName string) Type {
	if ft, ok := c.globalFns[fnName]; ok {
		// Propagate callee effects
		for _, eff := range ft.Effects {
			c.recordEffect(eff)
		}
		if ft.ReturnType != nil {
			return ft.ReturnType
		}
		return TypeVoid
	}
	// Could be a type constructor or external function — return unknown
	return TypeUnknown
}

func (c *Checker) inferMethodCall(fa *parser.FieldAccessExpression) Type {
	leftType := c.inferExprType(fa.Left)
	// Effect namespace method call (HTTP.get, DB.query, etc.)
	if ns, ok := leftType.(*EffectNamespaceType); ok {
		c.recordEffect(ns.Name)
		return TypeUnknown
	}
	return TypeUnknown
}

func (c *Checker) inferFieldAccess(expr *parser.FieldAccessExpression) Type {
	leftType := c.inferExprType(expr.Left)
	if leftType == nil {
		return TypeVoid
	}

	// Tag access — cannot read actor state
	if tag, ok := leftType.(*TagType); ok {
		if actor, ok := c.globalActors[tag.ActorName]; ok {
			if _, hasState := actor.States[expr.Field]; hasState {
				c.addError(fmt.Sprintf("cannot access actor state from outside: '%s'", expr.Field))
				return TypeVoid
			}
		}
	}

	// Object field access
	if obj, ok := leftType.(*ObjectType); ok {
		if ft, exists := obj.Fields[expr.Field]; exists {
			return ft
		}
	}

	// Actor self field access (inside receive blocks)
	if actor, ok := leftType.(*ActorType); ok {
		if st, exists := actor.States[expr.Field]; exists {
			return st
		}
	}

	return TypeVoid
}

func (c *Checker) checkAssignment(expr *parser.AssignmentExpression) Type {
	c.inferExprType(expr.Value)

	switch target := expr.Left.(type) {
	case *parser.Identifier:
		b, ok := c.scope.lookup(target.Value)
		if !ok {
			c.addError(fmt.Sprintf("undefined variable '%s'", target.Value))
			return TypeVoid
		}
		if b.Capability == "val" {
			c.addError(fmt.Sprintf("cannot assign to val binding '%s'", target.Value))
			return TypeVoid
		}
	case *parser.FieldAccessExpression:
		c.inferExprType(target)
	}
	return TypeVoid
}

func (c *Checker) checkSpawn(expr *parser.SpawnExpression) Type {
	var actorName string
	switch call := expr.Call.(type) {
	case *parser.CallExpression:
		if ident, ok := call.Function.(*parser.Identifier); ok {
			actorName = ident.Value
		}
	case *parser.Identifier:
		actorName = call.Value
	}

	if actorName == "" {
		return TypeVoid
	}

	if _, ok := c.globalActors[actorName]; !ok {
		c.addError(fmt.Sprintf("undefined actor '%s'", actorName))
		return TypeVoid
	}

	return &TagType{ActorName: actorName}
}

func (c *Checker) checkHandle(expr *parser.HandleExpression) Type {
	if !c.knownEffects[expr.EffectName] {
		c.addError(fmt.Sprintf("unknown effect '%s'", expr.EffectName))
		return TypeVoid
	}

	// Evaluate handler expression (lenient — may reference external types)
	c.inferExprType(expr.Handler)

	// Add this effect to handled set
	prevHandled := c.handledEffects
	newHandled := make(map[string]bool)
	maps.Copy(newHandled, prevHandled)
	newHandled[expr.EffectName] = true
	c.handledEffects = newHandled

	c.pushScope()
	bodyType := c.checkBlock(expr.Body)
	c.popScope()

	c.handledEffects = prevHandled

	return bodyType
}

func (c *Checker) checkIf(expr *parser.IfExpression) Type {
	c.inferExprType(expr.Condition)

	c.pushScope()
	consType := c.checkBlock(expr.Consequence)
	c.popScope()

	if expr.Alternative != nil {
		c.pushScope()
		altType := c.checkBlock(expr.Alternative)
		c.popScope()
		_ = altType
	}

	return consType
}

func (c *Checker) checkMatch(expr *parser.MatchExpression) Type {
	c.inferExprType(expr.Subject)

	var resultType Type
	for _, arm := range expr.Arms {
		c.pushScope()
		// Destructure-pattern bindings: `Ok(value)` → value
		for _, name := range arm.Pattern.Bindings {
			c.scope.define(name, &Binding{Type: TypeUnknown, Capability: "ref"})
		}
		// Object-style destructure bindings: `Point { x, y }` binds x and y;
		// `Point { x: bound }` binds `bound`; `Point { x: 0 }` binds nothing.
		if arm.Pattern.Kind == "object_destructure" {
			for _, of := range arm.Pattern.ObjectFields {
				if of.Subpattern == nil {
					c.scope.define(of.Field, &Binding{Type: TypeUnknown, Capability: "ref"})
				} else if of.Subpattern.Kind == "identifier" && of.Subpattern.Value != "" {
					first := of.Subpattern.Value[0]
					if first >= 'a' && first <= 'z' && of.Subpattern.Value != "_" {
						c.scope.define(of.Subpattern.Value, &Binding{Type: TypeUnknown, Capability: "ref"})
					}
				}
			}
		}
		// Bare-lowercase identifier patterns bind the whole subject (mirrors
		// the interpreter); body sees it as TypeUnknown.
		if arm.Pattern.Kind == "identifier" && arm.Pattern.Value != "" {
			first := arm.Pattern.Value[0]
			if first >= 'a' && first <= 'z' && arm.Pattern.Value != "true" && arm.Pattern.Value != "false" {
				c.scope.define(arm.Pattern.Value, &Binding{Type: TypeUnknown, Capability: "ref"})
			}
		}
		// Guards run with the same scope; just infer to surface errors.
		if arm.Guard != nil {
			c.inferExprType(arm.Guard)
		}
		armType := c.inferExprType(arm.Body)
		c.popScope()
		if resultType == nil && armType != nil {
			resultType = armType
		}
	}
	return resultType
}

func (c *Checker) checkObjectLiteral(expr *parser.ObjectLiteral) Type {
	for _, f := range expr.Fields {
		c.inferExprType(f.Value)
	}
	if expr.Name == "" {
		// Bare `{ k: v }` literal — typed as the generic "Object" runtime type.
		return &BuiltinType{Name: "Object"}
	}
	if t, ok := c.globalTypes[expr.Name]; ok {
		return t
	}
	return &BuiltinType{Name: expr.Name}
}

func (c *Checker) checkLambda(expr *parser.LambdaExpression) Type {
	c.pushScope()
	defer c.popScope()
	for _, p := range expr.Params {
		c.scope.define(p.Name, &Binding{
			Type:       c.resolveType(p.TypeExpr),
			Capability: capOrDefault(p.Capability),
		})
	}
	bodyType := c.checkBlock(expr.Body)
	if expr.ReturnType != "" {
		return c.resolveType(expr.ReturnType)
	}
	return bodyType
}

// checkSendExpression enforces capability rules on `actor <- msg`. iso
// values transfer ownership (and are marked consumed); ref values cannot
// cross actor boundaries; val and unresolved message-name idents are fine.
func (c *Checker) checkSendExpression(expr *parser.InfixExpression) {
	c.inferExprType(expr.Left)

	if ident, ok := expr.Right.(*parser.Identifier); ok {
		b, found := c.scope.lookup(ident.Value)
		if !found {
			// Unbound identifier is treated as a message-type name (Ping, Increment).
			return
		}
		if b.Capability == "iso" {
			if b.Consumed {
				c.addError(fmt.Sprintf("iso variable '%s' was already consumed", ident.Value))
				return
			}
			b.Consumed = true
			return
		}
		if b.Capability == "ref" {
			c.addError("cannot send ref capability across actor boundary")
			return
		}
		return
	}
	c.inferExprType(expr.Right)
}

func (c *Checker) recordEffect(effect string) {
	if c.handledEffects[effect] {
		return
	}
	c.usedEffects[effect] = true
}

// checkEffects enforces the two-way effect contract: used effects must be
// declared (stricter for main), declared effects must be used.
func (c *Checker) checkEffects(fnName string, declaredEffects []string) {
	declaredSet := make(map[string]bool)
	for _, e := range declaredEffects {
		declaredSet[e] = true
	}

	for effect := range c.usedEffects {
		if !declaredSet[effect] {
			if fnName == "main" {
				c.addError(fmt.Sprintf("unhandled effect '%s': main() must have no unhandled effects", effect))
			} else {
				c.addError(fmt.Sprintf("effect '%s' used but not declared", effect))
			}
		}
	}

	for _, effect := range declaredEffects {
		if !c.usedEffects[effect] {
			c.addError(fmt.Sprintf("effect '%s' declared but never used", effect))
		}
	}
}

func capOrDefault(cap string) string {
	if cap == "" {
		return "ref"
	}
	return cap
}
