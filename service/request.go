package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strconv"

	"github.com/adriangitvitz/yoru/interpreter"
)

// requestContextKeyT keys the per-request bag middleware writes and Yoru
// handlers read via `req.context.get(...)`.
type requestContextKeyT struct{}

var requestContextKey = requestContextKeyT{}

// SetRequestContext returns a copy of r with key/val attached to its bag.
// GoRequestToYoru materializes the bag into the Yoru `req.context` MapVal.
func SetRequestContext(r *http.Request, key string, val interpreter.Value) *http.Request {
	bag, _ := r.Context().Value(requestContextKey).(map[string]interpreter.Value)
	if bag == nil {
		bag = make(map[string]interpreter.Value)
	}
	// Copy-on-write: chained middleware must not see each other's mutations via aliasing.
	newBag := make(map[string]interpreter.Value, len(bag)+1)
	maps.Copy(newBag, bag)
	newBag[key] = val
	ctx := context.WithValue(r.Context(), requestContextKey, newBag)
	return r.WithContext(ctx)
}

// GoRequestToYoru converts a Go HTTP request to a Yoru ObjectVal.
func GoRequestToYoru(r *http.Request) *interpreter.ObjectVal {
	headerFields := make(map[string]interpreter.Value)
	for k, v := range r.Header {
		if len(v) > 0 {
			headerFields[k] = &interpreter.StringVal{V: v[0]}
		}
	}

	queryFields := make(map[string]interpreter.Value)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			queryFields[k] = &interpreter.StringVal{V: v[0]}
		}
	}

	// Surface per-request bag stamped by middleware as a Yoru MapVal.
	ctxEntries := make(map[string]interpreter.Value)
	var ctxOrder []string
	if bag, ok := r.Context().Value(requestContextKey).(map[string]interpreter.Value); ok {
		for k, v := range bag {
			ctxEntries[k] = v
			ctxOrder = append(ctxOrder, k)
		}
	}

	return &interpreter.ObjectVal{
		TypeName: "Request",
		Fields: map[string]interpreter.Value{
			"method":  &interpreter.StringVal{V: r.Method},
			"path":    &interpreter.StringVal{V: r.URL.Path},
			"headers": &interpreter.ObjectVal{TypeName: "Headers", Fields: headerFields},
			"query":   &interpreter.ObjectVal{TypeName: "Query", Fields: queryFields},
			"context": &interpreter.MapVal{Entries: ctxEntries, Order: ctxOrder},
		},
	}
}

// YoruResponseToGo writes a Yoru Value as an HTTP response.
func YoruResponseToGo(val interpreter.Value, w http.ResponseWriter) {
	resp, ok := val.(*interpreter.ResponseVal)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		data := valueToJSON(val)
		jsonBytes, _ := json.Marshal(data)
		_, _ = w.Write(jsonBytes)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.Status)
	if _, isNil := resp.Body.(*interpreter.NilVal); !isNil {
		data := valueToJSON(resp.Body)
		jsonBytes, _ := json.Marshal(data)
		_, _ = w.Write(jsonBytes)
	}
}

// ParseBody reads and parses a JSON request body into a Yoru Value.
func ParseBody(r *http.Request) (interpreter.Value, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	defer func() { _ = r.Body.Close() }()

	if len(body) == 0 {
		return &interpreter.NilVal{}, nil
	}

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}

	return jsonToValue(raw), nil
}

// jsonToValue converts a JSON-decoded value to a Yoru Value.
func jsonToValue(raw any) interpreter.Value {
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
			fields[k] = jsonToValue(val)
		}
		return &interpreter.ObjectVal{TypeName: "Object", Fields: fields}
	case []any:
		elems := make([]interpreter.Value, len(v))
		for i, val := range v {
			elems[i] = jsonToValue(val)
		}
		return &interpreter.ListVal{Elements: elems}
	}
	return &interpreter.NilVal{}
}

// valueToJSON converts a Yoru Value to a JSON-serializable Go value.
func valueToJSON(v interpreter.Value) any {
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
			result[i] = valueToJSON(elem)
		}
		return result
	case *interpreter.ObjectVal:
		result := make(map[string]any)
		for k, field := range val.Fields {
			result[k] = valueToJSON(field)
		}
		return result
	case *interpreter.MapVal:
		result := make(map[string]any, len(val.Entries))
		for k, entry := range val.Entries {
			result[k] = valueToJSON(entry)
		}
		return result
	case *interpreter.ResponseVal:
		return map[string]any{
			"status": val.Status,
			"body":   valueToJSON(val.Body),
		}
	}
	return v.Inspect()
}

// FormatValueAsString returns a value formatted for display (used in responses).
func FormatValueAsString(v interpreter.Value) string {
	switch val := v.(type) {
	case *interpreter.StringVal:
		return val.V
	case *interpreter.IntVal:
		return strconv.FormatInt(val.V, 10)
	default:
		return v.Inspect()
	}
}
