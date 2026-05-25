package service

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/adriangitvitz/yoru/parser"
)

// OpenAPISpec is a minimal OpenAPI 3.0 specification.
type OpenAPISpec struct {
	OpenAPI string                        `json:"openapi"`
	Info    OpenAPIInfo                    `json:"info"`
	Paths   map[string]map[string]*Operation `json:"paths"`
}

// OpenAPIInfo describes the API.
type OpenAPIInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

// Operation describes a single API endpoint.
type Operation struct {
	Summary    string       `json:"summary"`
	Parameters []Parameter  `json:"parameters,omitempty"`
	Responses  map[string]ResponseSpec `json:"responses"`
	RequestBody *RequestBody `json:"requestBody,omitempty"`
}

// Parameter describes a path/query parameter.
type Parameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
	Schema   Schema `json:"schema"`
}

// Schema is a minimal JSON Schema.
type Schema struct {
	Type string `json:"type"`
}

// ResponseSpec describes a response.
type ResponseSpec struct {
	Description string `json:"description"`
}

// RequestBody describes a request body.
type RequestBody struct {
	Required bool                       `json:"required"`
	Content  map[string]MediaTypeObject `json:"content"`
}

// MediaTypeObject describes a media type.
type MediaTypeObject struct {
	Schema Schema `json:"schema"`
}

var paramRegex = regexp.MustCompile(`:(\w+)`)

// GenerateOpenAPI builds an OpenAPI 3.0 spec from a ServiceDecl.
func GenerateOpenAPI(decl *parser.ServiceDecl) *OpenAPISpec {
	spec := &OpenAPISpec{
		OpenAPI: "3.0.0",
		Info: OpenAPIInfo{
			Title:   decl.Name,
			Version: "1.0.0",
		},
		Paths: make(map[string]map[string]*Operation),
	}

	for _, route := range decl.Routes {
		openAPIPath := convertPath(route.Pattern)
		method := strings.ToLower(route.Method)

		if spec.Paths[openAPIPath] == nil {
			spec.Paths[openAPIPath] = make(map[string]*Operation)
		}

		op := &Operation{
			Summary: route.Handler,
			Responses: map[string]ResponseSpec{
				"200": {Description: "OK"},
			},
		}

		params := paramRegex.FindAllStringSubmatch(route.Pattern, -1)
		for _, match := range params {
			op.Parameters = append(op.Parameters, Parameter{
				Name:     match[1],
				In:       "path",
				Required: true,
				Schema:   Schema{Type: "string"},
			})
		}

		if method == "post" || method == "put" || method == "patch" {
			op.RequestBody = &RequestBody{
				Required: true,
				Content: map[string]MediaTypeObject{
					"application/json": {
						Schema: Schema{Type: "object"},
					},
				},
			}
		}

		spec.Paths[openAPIPath][method] = op
	}

	return spec
}

// convertPath converts :param to {param} for OpenAPI paths.
func convertPath(pattern string) string {
	return paramRegex.ReplaceAllString(pattern, "{$1}")
}

// ToJSON serializes the spec to pretty-printed JSON.
func (s *OpenAPISpec) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}
