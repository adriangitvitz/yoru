package stdlib

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
)

// HTTPProvider implements HTTP.get / HTTP.post. Returns a Response object
// on success, Result.Err on transport failures so `??` and pattern matching
// can recover.
type HTTPProvider struct {
	Client    *http.Client
	UserAgent string // sent as User-Agent header on every request
}

// DefaultUserAgent is sent when UserAgent is empty. Wikimedia and others
// reject Go's default "Go-http-client/1.1".
const DefaultUserAgent = "yoru/0.1 (+https://github.com/adriangitvitz/yoru)"

// NewHTTPProvider builds a provider with a 30s default timeout. Callers
// can supply their own *http.Client for proxies/transports.
func NewHTTPProvider() *HTTPProvider {
	return &HTTPProvider{
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *HTTPProvider) EffectName() string { return "HTTP" }

func (p *HTTPProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"get": &interpreter.BuiltinVal{
			Name: "HTTP.get",
			Fn: func(args []interpreter.Value) (interpreter.Value, error) {
				if len(args) < 1 {
					return httpErr("http_bad_args", "HTTP.get(url, headers?) requires a URL string"), nil
				}
				urlStr, ok := args[0].(*interpreter.StringVal)
				if !ok {
					return httpErr("http_bad_args", "HTTP.get URL must be a String"), nil
				}
				headers, hdrErr := parseHeadersArg(args, 1)
				if hdrErr != nil {
					return hdrErr, nil
				}
				return p.doRequest("GET", urlStr.V, "", headers), nil
			},
		},
		"post": &interpreter.BuiltinVal{
			Name: "HTTP.post",
			Fn: func(args []interpreter.Value) (interpreter.Value, error) {
				if len(args) < 1 {
					return httpErr("http_bad_args", "HTTP.post(url, body?, headers?) requires at least a URL"), nil
				}
				urlStr, ok := args[0].(*interpreter.StringVal)
				if !ok {
					return httpErr("http_bad_args", "HTTP.post URL must be a String"), nil
				}
				body := ""
				if len(args) > 1 {
					if _, isNil := args[1].(*interpreter.NilVal); !isNil {
						b, ok := args[1].(*interpreter.StringVal)
						if !ok {
							return httpErr("http_bad_args", "HTTP.post body must be a String or nil"), nil
						}
						body = b.V
					}
				}
				headers, hdrErr := parseHeadersArg(args, 2)
				if hdrErr != nil {
					return hdrErr, nil
				}
				return p.doRequest("POST", urlStr.V, body, headers), nil
			},
		},
	}
}

// parseHeadersArg pulls an optional headers Map at args[idx]. Returns
// (nil, nil) when not provided; (nil, errValue) when present but malformed.
func parseHeadersArg(args []interpreter.Value, idx int) (map[string]string, interpreter.Value) {
	if len(args) <= idx {
		return nil, nil
	}
	if _, isNil := args[idx].(*interpreter.NilVal); isNil {
		return nil, nil
	}
	m, ok := args[idx].(*interpreter.MapVal)
	if !ok {
		return nil, httpErr("http_bad_args", "headers must be a Map[String, String] or nil")
	}
	out := make(map[string]string, len(m.Entries))
	for k, v := range m.Entries {
		s, ok := v.(*interpreter.StringVal)
		if !ok {
			return nil, httpErr("http_bad_args", "every header value must be a String")
		}
		out[k] = s.V
	}
	return out, nil
}

// doRequest performs the HTTP call. extraHeaders is merged after the
// default User-Agent so a caller User-Agent overrides via map iteration.
func (p *HTTPProvider) doRequest(method, url, body string, extraHeaders map[string]string) interpreter.Value {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return httpErr("http_request_failed", fmt.Sprintf("build request: %s", err))
	}

	// Some APIs (Wikimedia, GitHub, etc.) require a non-default User-Agent.
	ua := p.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	req.Header.Set("User-Agent", ua)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return httpErr("http_request_failed", err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpErr("http_request_failed", fmt.Sprintf("read body: %s", err))
	}

	return buildResponse(resp.StatusCode, resp.Header, string(respBody))
}

// buildResponse returns the Response object Yoru code sees.
func buildResponse(status int, header http.Header, body string) interpreter.Value {
	headersMap := &interpreter.MapVal{
		Entries: make(map[string]interpreter.Value),
		Order:   nil,
	}
	for k, vs := range header {
		// Multi-value headers joined with comma (matches http.Header.Get).
		joined := strings.Join(vs, ", ")
		headersMap.Entries[k] = &interpreter.StringVal{V: joined}
		headersMap.Order = append(headersMap.Order, k)
	}

	return &interpreter.ObjectVal{
		TypeName: "Response",
		Fields: map[string]interpreter.Value{
			"status":  &interpreter.IntVal{V: int64(status)},
			"headers": headersMap,
			"body":    &interpreter.StringVal{V: body},
		},
	}
}

// httpErr constructs a Result.Err with a structured kind/message.
func httpErr(kind, message string) interpreter.Value {
	errObj := &interpreter.ObjectVal{
		TypeName: "Error",
		Fields: map[string]interpreter.Value{
			"kind":    &interpreter.StringVal{V: kind},
			"message": &interpreter.StringVal{V: message},
		},
	}
	return &interpreter.EnumVal{
		TypeName: "Result",
		Variant:  "Err",
		Fields: map[string]interpreter.Value{
			"error": errObj,
		},
	}
}

