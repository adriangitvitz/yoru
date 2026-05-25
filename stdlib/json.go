package stdlib

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/adriangitvitz/yoru/interpreter"
)

// JSONProvider implements the JSON effect via encoding/json. Interp
// (optional) enables JSON.decode(json, "TypeName") to validate against
// a Yoru `object` declaration.
type JSONProvider struct {
	Interp *interpreter.Interpreter
}

func (p *JSONProvider) EffectName() string { return "JSON" }

func (p *JSONProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"encode": &interpreter.BuiltinVal{Name: "JSON.encode", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("JSON.encode() takes 1 argument")
			}
			data := valueToGo(args[0])
			b, err := json.Marshal(data)
			if err != nil {
				return &interpreter.StringVal{V: ""}, nil
			}
			return &interpreter.StringVal{V: string(b)}, nil
		}},
		"decode": &interpreter.BuiltinVal{Name: "JSON.decode", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("JSON.decode() takes 1 or 2 arguments, got %d", len(args))
			}
			s, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("JSON.decode() argument must be String")
			}
			var raw any
			if err := json.Unmarshal([]byte(s.V), &raw); err != nil {
				return &interpreter.NilVal{}, nil
			}
			decoded := goToValue(raw)

			// 1-arg: generic ObjectVal, no validation.
			if len(args) == 1 {
				return decoded, nil
			}

			// 2-arg: validate against the named type, re-tag, Result.Err on failure.
			typeNameVal, ok := args[1].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("JSON.decode() second argument must be a type-name String")
			}
			obj, isObj := decoded.(*interpreter.ObjectVal)
			if !isObj {
				return interpreter.MakeErrResult("json_decode_failed",
					"JSON.decode("+typeNameVal.V+") expected a JSON object, got "+decoded.Type()), nil
			}
			if p.Interp == nil {
				// No interp to look up the type → best-effort: tag and skip
				// validation (for non-CLI embeddings).
				obj.TypeName = typeNameVal.V
				return obj, nil
			}
			fields, found := p.Interp.GetObjectFields(typeNameVal.V)
			if !found {
				return interpreter.MakeErrResult("json_decode_failed",
					"JSON.decode: unknown type '"+typeNameVal.V+"'"), nil
			}
			for _, f := range fields {
				if _, present := obj.Fields[f.Name]; !present {
					return interpreter.MakeErrResult("json_decode_failed",
						"JSON.decode("+typeNameVal.V+"): missing required field '"+f.Name+"'"), nil
				}
			}
			obj.TypeName = typeNameVal.V
			return obj, nil
		}},
		"pretty": &interpreter.BuiltinVal{Name: "JSON.pretty", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("JSON.pretty() takes 1 argument")
			}
			data := valueToGo(args[0])
			b, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				return &interpreter.StringVal{V: ""}, nil
			}
			return &interpreter.StringVal{V: string(b)}, nil
		}},
		"get": &interpreter.BuiltinVal{Name: "JSON.get", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("JSON.get() takes 2 arguments")
			}
			obj, ok := args[0].(*interpreter.ObjectVal)
			if !ok {
				return &interpreter.NilVal{}, nil
			}
			key, ok := args[1].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("JSON.get() key must be String")
			}
			if val, exists := obj.Fields[key.V]; exists {
				return val, nil
			}
			return &interpreter.NilVal{}, nil
		}},
		"merge": &interpreter.BuiltinVal{Name: "JSON.merge", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("JSON.merge() takes 2 arguments")
			}
			a, aOk := args[0].(*interpreter.ObjectVal)
			b, bOk := args[1].(*interpreter.ObjectVal)
			if !aOk || !bOk {
				return nil, fmt.Errorf("JSON.merge() arguments must be Objects")
			}
			fields := make(map[string]interpreter.Value)
			maps.Copy(fields, a.Fields)
			maps.Copy(fields, b.Fields)
			return &interpreter.ObjectVal{TypeName: "Object", Fields: fields}, nil
		}},
	}
}

// valueToGo converts a Yoru Value to a Go value for JSON marshaling.
func valueToGo(v interpreter.Value) any {
	switch val := v.(type) {
	case *interpreter.IntVal:
		return val.V
	case *interpreter.FloatVal:
		return val.V
	case *interpreter.StringVal:
		return val.V
	case *interpreter.BoolVal:
		return val.V
	case *interpreter.NilVal:
		return nil
	case *interpreter.ListVal:
		result := make([]any, len(val.Elements))
		for i, elem := range val.Elements {
			result[i] = valueToGo(elem)
		}
		return result
	case *interpreter.ObjectVal:
		result := make(map[string]any)
		for k, field := range val.Fields {
			result[k] = valueToGo(field)
		}
		return result
	}
	return v.Inspect()
}

// goToValue converts a JSON-decoded Go value to a Yoru Value.
func goToValue(raw any) interpreter.Value {
	switch v := raw.(type) {
	case float64:
		if v == float64(int64(v)) {
			return &interpreter.IntVal{V: int64(v)}
		}
		return &interpreter.FloatVal{V: v}
	case string:
		return &interpreter.StringVal{V: v}
	case bool:
		return &interpreter.BoolVal{V: v}
	case nil:
		return &interpreter.NilVal{}
	case map[string]any:
		fields := make(map[string]interpreter.Value)
		for k, val := range v {
			fields[k] = goToValue(val)
		}
		return &interpreter.ObjectVal{TypeName: "Object", Fields: fields}
	case []any:
		elems := make([]interpreter.Value, len(v))
		for i, val := range v {
			elems[i] = goToValue(val)
		}
		return &interpreter.ListVal{Elements: elems}
	}
	return &interpreter.NilVal{}
}
