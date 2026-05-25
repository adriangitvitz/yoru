package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/adriangitvitz/yoru/parser"
)

// ServiceConfig holds everything needed to build a service server.
type ServiceConfig struct {
	Decl   *parser.ServiceDecl
	Interp *interpreter.Interpreter
}

// Server is an HTTP server built from a Yoru service declaration.
type Server struct {
	router  *Router
	config  ServiceConfig
	openAPI []byte
}

// NewServer creates a Server from config, wiring routes to handler functions.
func NewServer(config ServiceConfig) (*Server, error) {
	router := NewRouter()

	prefix := config.Decl.Prefix
	for _, route := range config.Decl.Routes {
		handlerName := route.Handler

		if route.InlineHandler != nil {
			handlerName = route.InlineHandler.Name
			fnVal := &interpreter.FunctionVal{
				Name:   handlerName,
				Params: route.InlineHandler.Params,
				Body:   route.InlineHandler.Body,
				Env:    config.Interp.Env(),
			}
			config.Interp.Env().Set(handlerName, fnVal)
		}

		if _, ok := config.Interp.Env().Get(handlerName); !ok {
			return nil, fmt.Errorf("service '%s': handler '%s' not found", config.Decl.Name, handlerName)
		}
		pattern := prefix + route.Pattern
		if err := router.Register(route.Method, pattern, handlerName); err != nil {
			return nil, err
		}
	}

	spec := GenerateOpenAPI(config.Decl)
	openAPIJSON, _ := spec.ToJSON()

	return &Server{
		router:  router,
		config:  config,
		openAPI: openAPIJSON,
	}, nil
}

// resolveMiddlewares maps service middleware refs to Middleware functions,
// using interp to evaluate any args in parameterised refs (e.g. CORS.allow_origin("*")).
func resolveMiddlewares(refs []parser.MiddlewareRef, interp *interpreter.Interpreter) []Middleware {
	var mws []Middleware
	for _, ref := range refs {
		mw := buildMiddleware(ref, interp)
		if mw != nil {
			mws = append(mws, mw)
		}
	}
	return mws
}

// buildMiddleware dispatches a MiddlewareRef to its factory; unknown
// (Name, Method) combos return nil and are silently skipped.
func buildMiddleware(ref parser.MiddlewareRef, interp *interpreter.Interpreter) Middleware {
	// User factories override built-ins.
	customMu.RLock()
	factory, ok := customMiddlewares[ref.Name]
	customMu.RUnlock()
	if ok {
		return factory(ref, interp)
	}

	switch ref.Name {
	case "Logger":
		return LoggerMiddleware(func(method, path string, status int, duration time.Duration) {})
	case "CORS":
		return buildCORSMiddleware(ref, interp)
	case "Recover":
		return RecoverMiddleware()
	case "RequestID":
		return RequestIDMiddleware()
	}
	return nil
}

// MiddlewareFactory builds a Middleware from a MiddlewareRef; interp lets it
// evaluate ref args (e.g. JWT.verify("secret")).
type MiddlewareFactory func(ref parser.MiddlewareRef, interp *interpreter.Interpreter) Middleware

var (
	customMu          sync.RWMutex
	customMiddlewares = map[string]MiddlewareFactory{}
)

// RegisterMiddleware installs a custom factory under name; re-registering an
// existing name (including built-ins) overrides it for the process lifetime.
func RegisterMiddleware(name string, factory MiddlewareFactory) {
	customMu.Lock()
	defer customMu.Unlock()
	customMiddlewares[name] = factory
}

// UnregisterMiddleware removes a registered factory; used by tests.
func UnregisterMiddleware(name string) {
	customMu.Lock()
	defer customMu.Unlock()
	delete(customMiddlewares, name)
}

// buildCORSMiddleware handles bare `CORS` plus `CORS.allow_origin(s)(...)` forms.
func buildCORSMiddleware(ref parser.MiddlewareRef, interp *interpreter.Interpreter) Middleware {
	cfg := CORSConfig{}
	switch ref.Method {
	case "":
		// Bare `CORS` defaults to Access-Control-Allow-Origin: *.
		cfg.AllowedOrigins = []string{"*"}
	case "allow_origin":
		if origin, ok := middlewareStringArg(ref, 0, interp); ok {
			cfg.AllowedOrigins = []string{origin}
		}
	case "allow_origins":
		if list, ok := middlewareListArg(ref, 0, interp); ok {
			for _, v := range list {
				if s, isStr := v.(*interpreter.StringVal); isStr {
					cfg.AllowedOrigins = append(cfg.AllowedOrigins, s.V)
				}
			}
		}
	}
	return CORSMiddleware(cfg)
}

// middlewareStringArg evaluates ref.Args[idx] and returns its string value.
func middlewareStringArg(ref parser.MiddlewareRef, idx int, interp *interpreter.Interpreter) (string, bool) {
	if idx >= len(ref.Args) || interp == nil {
		return "", false
	}
	v := interp.EvalExpressionPublic(ref.Args[idx])
	if s, ok := v.(*interpreter.StringVal); ok {
		return s.V, true
	}
	return "", false
}

// middlewareListArg evaluates ref.Args[idx] and returns its list elements.
func middlewareListArg(ref parser.MiddlewareRef, idx int, interp *interpreter.Interpreter) ([]interpreter.Value, bool) {
	if idx >= len(ref.Args) || interp == nil {
		return nil, false
	}
	v := interp.EvalExpressionPublic(ref.Args[idx])
	if list, ok := v.(*interpreter.ListVal); ok {
		return list.Elements, true
	}
	return nil, false
}

// Handler returns an http.Handler for this server.
func (s *Server) Handler() http.Handler {
	handler := s.coreHandler()
	if len(s.config.Decl.Middlewares) > 0 {
		mws := resolveMiddlewares(s.config.Decl.Middlewares, s.config.Interp)
		handler = Chain(handler, mws...)
	}
	return handler
}

func (s *Server) coreHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/openapi.json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write(s.openAPI)
			return
		}

		match := s.router.Match(r.Method, r.URL.Path)
		if match == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			data, _ := json.Marshal(map[string]string{"error": "not found"})
			_, _ = w.Write(data)
			return
		}

		fnVal, ok := s.config.Interp.Env().Get(match.Route.Handler)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			data, _ := json.Marshal(map[string]string{"error": "handler not found"})
			_, _ = w.Write(data)
			return
		}

		fn, ok := fnVal.(*interpreter.FunctionVal)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			data, _ := json.Marshal(map[string]string{"error": "handler is not a function"})
			_, _ = w.Write(data)
			return
		}

		args := make(map[string]interpreter.Value)

		reqObj := GoRequestToYoru(r)

		for k, v := range match.Params {
			args[k] = &interpreter.StringVal{V: v}
		}

		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			body, err := ParseBody(r)
			if err == nil && body != nil {
				args["body"] = body
			}
		}

		args["req"] = reqObj

		result, err := s.config.Interp.CallFunctionWithValues(fn, args)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			data, _ := json.Marshal(map[string]string{"error": err.Error()})
			_, _ = w.Write(data)
			return
		}

		YoruResponseToGo(result, w)
	})
}

// ListenAndServe starts the HTTP server on host:port. Empty host binds all
// interfaces; a non-empty host (e.g. "127.0.0.1") restricts the bind.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.config.Decl.Host, s.config.Decl.Port)
	return http.ListenAndServe(addr, s.Handler())
}
