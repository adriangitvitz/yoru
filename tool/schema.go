package tool

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolSchema is the Anthropic-compatible JSON Schema for a tool.
type ToolSchema struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	InputSchema  InputSchema   `json:"input_schema"`
	OutputSchema *OutputSchema `json:"output_schema,omitempty"`
}

type InputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required"`
}

type OutputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required"`
}

type PropertySchema struct {
	Type        string                    `json:"type,omitempty"`
	Description string                    `json:"description,omitempty"`
	Items       *PropertySchema           `json:"items,omitempty"`
	Properties  map[string]PropertySchema `json:"properties,omitempty"`
	Required    []string                  `json:"required,omitempty"`
	AnyOf       []PropertySchema          `json:"anyOf,omitempty"`
	Enum        []string                  `json:"enum,omitempty"`
	Const       string                    `json:"const,omitempty"`
}

func (ts *ToolSchema) ToJSON() ([]byte, error) {
	return json.Marshal(ts)
}

// ToMCPJSON serializes a ToolSchema to JSON in the MCP wire shape (camelCase keys).
func (ts *ToolSchema) ToMCPJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name         string        `json:"name"`
		Description  string        `json:"description"`
		InputSchema  InputSchema   `json:"inputSchema"`
		OutputSchema *OutputSchema `json:"outputSchema,omitempty"`
	}{
		Name:         ts.Name,
		Description:  ts.Description,
		InputSchema:  ts.InputSchema,
		OutputSchema: ts.OutputSchema,
	})
}

const DiscriminatorField = "kind"

type TypeResolver interface {
	ResolveObject(name string) ([]FieldLike, bool)
	ResolveEnum(name string) (EnumLike, bool)
}

type EnumLike interface {
	EnumName() string
	EnumVariants() []EnumVariantLike
}

type EnumVariantLike interface {
	VariantName() string
	VariantFields() []FieldLike
}

func YoruTypeToJSONSchema(yoruType string) PropertySchema {
	return YoruTypeToJSONSchemaWith(yoruType, nil)
}

func YoruTypeToJSONSchemaWith(yoruType string, resolver TypeResolver) PropertySchema {
	if strings.HasPrefix(yoruType, "Option[") && strings.HasSuffix(yoruType, "]") {
		return YoruTypeToJSONSchemaWith(yoruType[7:len(yoruType)-1], resolver)
	}

	if strings.HasPrefix(yoruType, "[") && strings.HasSuffix(yoruType, "]") {
		inner := YoruTypeToJSONSchemaWith(yoruType[1:len(yoruType)-1], resolver)
		return PropertySchema{Type: "array", Items: &inner}
	}

	switch yoruType {
	case "Int":
		return PropertySchema{Type: "integer"}
	case "Float":
		return PropertySchema{Type: "number"}
	case "String":
		return PropertySchema{Type: "string"}
	case "Bool":
		return PropertySchema{Type: "boolean"}
	}

	if resolver != nil {
		if fields, ok := resolver.ResolveObject(yoruType); ok {
			return objectSchema(fields, resolver)
		}
		if enumDecl, ok := resolver.ResolveEnum(yoruType); ok {
			return enumSchema(enumDecl, resolver)
		}
	}

	return PropertySchema{Type: "string"}
}

func objectSchema(fields []FieldLike, resolver TypeResolver) PropertySchema {
	props := make(map[string]PropertySchema, len(fields))
	var required []string
	for _, f := range fields {
		ps := YoruTypeToJSONSchemaWith(f.FieldTypeExpr(), resolver)
		if ann := f.FieldAnnotation(); ann != nil && ann.Name == "doc" {
			ps.Description = ann.Value
		}
		props[f.FieldName()] = ps
		if !fieldIsOptional(f) {
			required = append(required, f.FieldName())
		}
	}
	return PropertySchema{Type: "object", Properties: props, Required: required}
}

func enumSchema(decl EnumLike, resolver TypeResolver) PropertySchema {
	variants := decl.EnumVariants()

	allUnit := true
	for _, v := range variants {
		if len(v.VariantFields()) > 0 {
			allUnit = false
			break
		}
	}

	if allUnit {
		names := make([]string, len(variants))
		for i, v := range variants {
			names[i] = v.VariantName()
		}
		return PropertySchema{Type: "string", Enum: names}
	}

	branches := make([]PropertySchema, len(variants))
	for i, v := range variants {
		props := map[string]PropertySchema{
			DiscriminatorField: {Const: v.VariantName()},
		}
		required := []string{DiscriminatorField}
		for _, f := range v.VariantFields() {
			ps := YoruTypeToJSONSchemaWith(f.FieldTypeExpr(), resolver)
			if ann := f.FieldAnnotation(); ann != nil && ann.Name == "doc" {
				ps.Description = ann.Value
			}
			props[f.FieldName()] = ps
			if !fieldIsOptional(f) {
				required = append(required, f.FieldName())
			}
		}
		branches[i] = PropertySchema{Type: "object", Properties: props, Required: required}
	}
	return PropertySchema{AnyOf: branches}
}

func fieldIsOptional(f FieldLike) bool {
	return strings.HasPrefix(f.FieldTypeExpr(), "Option[") || f.FieldHasDefault()
}

type ToolDeclLike interface {
	ToolName() string
	ToolDescription() string
	ToolInputs() []FieldLike
	ToolOutputType() string
	ToolOutputs() []FieldLike
}

type FieldLike interface {
	FieldName() string
	FieldTypeExpr() string
	FieldHasDefault() bool
	FieldAnnotation() *AnnotationLike
}

type AnnotationLike struct {
	Name  string
	Value string
}

func FromAST(decl ToolDeclLike) (*ToolSchema, error) {
	return FromASTWithResolver(decl, nil)
}

func FromASTWithResolver(decl ToolDeclLike, resolver TypeResolver) (*ToolSchema, error) {
	if decl.ToolDescription() == "" {
		return nil, fmt.Errorf("tool '%s' missing description", decl.ToolName())
	}

	props := make(map[string]PropertySchema)
	var required []string

	for _, input := range decl.ToolInputs() {
		ps := YoruTypeToJSONSchemaWith(input.FieldTypeExpr(), resolver)
		if ann := input.FieldAnnotation(); ann != nil && ann.Name == "doc" {
			ps.Description = ann.Value
		}
		props[input.FieldName()] = ps
		if !fieldIsOptional(input) {
			required = append(required, input.FieldName())
		}
	}

	ts := &ToolSchema{
		Name:        decl.ToolName(),
		Description: decl.ToolDescription(),
		InputSchema: InputSchema{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
	}
	if outputs := decl.ToolOutputs(); len(outputs) > 0 {
		oprops := make(map[string]PropertySchema)
		var orequired []string
		for _, out := range outputs {
			ps := YoruTypeToJSONSchemaWith(out.FieldTypeExpr(), resolver)
			if ann := out.FieldAnnotation(); ann != nil && ann.Name == "doc" {
				ps.Description = ann.Value
			}
			oprops[out.FieldName()] = ps
			if !strings.HasPrefix(out.FieldTypeExpr(), "Option[") {
				orequired = append(orequired, out.FieldName())
			}
		}
		ts.OutputSchema = &OutputSchema{
			Type:       "object",
			Properties: oprops,
			Required:   orequired,
		}
	}
	return ts, nil
}
