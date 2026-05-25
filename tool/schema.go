package tool

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolSchema is the Anthropic-compatible JSON Schema for a tool. OutputSchema
// is informational on Anthropic but load-bearing on MCP 2024-11-05.
// InputSchema and OutputSchema are distinct types so future fields on one
// can't leak into the other.
type ToolSchema struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	InputSchema  InputSchema   `json:"input_schema"`
	OutputSchema *OutputSchema `json:"output_schema,omitempty"`
}

// InputSchema describes the tool's input parameters.
type InputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required"`
}

// OutputSchema describes the tool's structured return shape.
type OutputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required"`
}

// PropertySchema describes a single input property.
type PropertySchema struct {
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Items       *PropertySchema `json:"items,omitempty"`
}

// ToJSON serializes to the Anthropic API shape (`input_schema`, snake_case).
func (ts *ToolSchema) ToJSON() ([]byte, error) {
	return json.Marshal(ts)
}

// ToMCPJSON serializes to the MCP wire shape (`inputSchema`/`outputSchema`,
// camelCase). Used by MCP servers and by Yoru's `Tool.schema()`.
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

// YoruTypeToJSONSchema converts a Yoru type expression to a JSON Schema
// PropertySchema. Option[T] unwraps to T; optionality lives in Required.
func YoruTypeToJSONSchema(yoruType string) PropertySchema {
	if strings.HasPrefix(yoruType, "Option[") && strings.HasSuffix(yoruType, "]") {
		inner := yoruType[7 : len(yoruType)-1]
		ps := YoruTypeToJSONSchema(inner)
		return ps
	}

	if strings.HasPrefix(yoruType, "[") && strings.HasSuffix(yoruType, "]") {
		inner := yoruType[1 : len(yoruType)-1]
		itemSchema := YoruTypeToJSONSchema(inner)
		return PropertySchema{Type: "array", Items: &itemSchema}
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
	default:
		return PropertySchema{Type: "string"}
	}
}

// ToolDeclLike abstracts over parser.ToolDecl so tool/ doesn't import parser.
// ToolOutputs() returns fields from the `output { ... }` block form (empty
// otherwise); when non-empty, FromAST emits an OutputSchema.
type ToolDeclLike interface {
	ToolName() string
	ToolDescription() string
	ToolInputs() []FieldLike
	ToolOutputType() string
	ToolOutputs() []FieldLike
}

// FieldLike abstracts over parser.Field.
type FieldLike interface {
	FieldName() string
	FieldTypeExpr() string
	FieldHasDefault() bool
	FieldAnnotation() *AnnotationLike
}

// AnnotationLike holds annotation data.
type AnnotationLike struct {
	Name  string
	Value string
}

// FromAST builds a ToolSchema from a ToolDeclLike.
func FromAST(decl ToolDeclLike) (*ToolSchema, error) {
	if decl.ToolDescription() == "" {
		return nil, fmt.Errorf("tool '%s' missing description", decl.ToolName())
	}

	props := make(map[string]PropertySchema)
	var required []string

	for _, input := range decl.ToolInputs() {
		typeExpr := input.FieldTypeExpr()
		ps := YoruTypeToJSONSchema(typeExpr)

		if ann := input.FieldAnnotation(); ann != nil && ann.Name == "doc" {
			ps.Description = ann.Value
		}

		props[input.FieldName()] = ps

		isOptional := strings.HasPrefix(typeExpr, "Option[") || input.FieldHasDefault()
		if !isOptional {
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
			typeExpr := out.FieldTypeExpr()
			ps := YoruTypeToJSONSchema(typeExpr)
			if ann := out.FieldAnnotation(); ann != nil && ann.Name == "doc" {
				ps.Description = ann.Value
			}
			oprops[out.FieldName()] = ps
			// Output fields are required by default; Option[T] is nullable.
			if !strings.HasPrefix(typeExpr, "Option[") {
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

