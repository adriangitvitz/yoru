package typechecker

import "strings"

// Type is the interface all Yoru types implement.
type Type interface {
	TypeName() string
}

// BuiltinType represents primitive types: Int, Float, String, Bool, Nil.
type BuiltinType struct {
	Name string
}

func (b *BuiltinType) TypeName() string { return b.Name }

// ObjectType represents a declared object with named fields.
type ObjectType struct {
	Name   string
	Fields map[string]Type
}

func (o *ObjectType) TypeName() string { return o.Name }

// EnumType represents a declared enum with variants.
type EnumType struct {
	Name     string
	Variants map[string][]Type // variant name -> payload types
}

func (e *EnumType) TypeName() string { return e.Name }

// FnType represents a function signature.
type FnType struct {
	Name       string
	Params     []Type
	ReturnType Type
	Effects    []string
}

func (f *FnType) TypeName() string {
	if f.ReturnType != nil {
		return f.ReturnType.TypeName()
	}
	return "Void"
}

// ActorType represents a declared actor.
type ActorType struct {
	Name     string
	States   map[string]Type
	Receives map[string]*ReceiveType
}

func (a *ActorType) TypeName() string { return a.Name }

// ReceiveType represents a receive block's signature.
type ReceiveType struct {
	Params     map[string]Type
	ReturnType Type
}

// ListType represents [T].
type ListType struct {
	Element Type
}

func (l *ListType) TypeName() string { return "[" + l.Element.TypeName() + "]" }

// GenericType represents a parameterized type like Result[T, Error].
type GenericType struct {
	Name   string
	Params []Type
}

func (g *GenericType) TypeName() string {
	var result strings.Builder
	result.WriteString(g.Name + "[")
	for i, p := range g.Params {
		if i > 0 {
			result.WriteString(", ")
		}
		result.WriteString(p.TypeName())
	}
	return result.String() + "]"
}

// TagType represents the tag capability — identity only, no read/write access.
type TagType struct {
	ActorName string
}

func (t *TagType) TypeName() string { return "tag " + t.ActorName }

// EffectNamespaceType represents an effect namespace (HTTP, DB, etc.).
type EffectNamespaceType struct {
	Name string
}

func (e *EffectNamespaceType) TypeName() string { return e.Name }

// UnknownType represents a type that cannot be inferred at compile time.
// Matches any expected type — used for effect method returns and external calls.
type UnknownType struct{}

func (u *UnknownType) TypeName() string { return "<unknown>" }

// Builtin type singletons.
var (
	TypeInt     = &BuiltinType{Name: "Int"}
	TypeFloat   = &BuiltinType{Name: "Float"}
	TypeString  = &BuiltinType{Name: "String"}
	TypeBool    = &BuiltinType{Name: "Bool"}
	TypeNil     = &BuiltinType{Name: "Nil"}
	TypeVoid    = &BuiltinType{Name: "Void"}
	TypeUnknown = &UnknownType{}
)
