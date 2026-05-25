package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
)

// Middleware is a function that wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares in order (first middleware is outermost).
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for _, middleware := range slices.Backward(middlewares) {
		handler = middleware(handler)
	}
	return handler
}

// LoggerMiddleware logs method, path, status, and duration for each request.
func LoggerMiddleware(logFn func(method, path string, status int, duration time.Duration)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(rw, r)
			logFn(r.Method, r.URL.Path, rw.status, time.Since(start))
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// CORSConfig configures CORS behavior.
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
}

// CORSMiddleware handles Cross-Origin Resource Sharing.
func CORSMiddleware(config CORSConfig) Middleware {
	if len(config.AllowedMethods) == 0 {
		config.AllowedMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}
	if len(config.AllowedHeaders) == 0 {
		config.AllowedHeaders = []string{"Content-Type", "Authorization"}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := false
			for _, o := range config.AllowedOrigins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}

			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			if len(config.AllowedOrigins) == 1 && config.AllowedOrigins[0] == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))

			if r.Method == "OPTIONS" {
				w.WriteHeader(200)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RecoverMiddleware catches panics and returns 500.
func RecoverMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(500)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"error": fmt.Sprintf("internal server error: %v", err),
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDMiddleware adds an X-Request-ID header if not already present.
func RequestIDMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				buf := make([]byte, 16)
				rand.Read(buf)
				id = hex.EncodeToString(buf)
			}
			w.Header().Set("X-Request-ID", id)
			r = r.WithContext(context.WithValue(r.Context(), requestIDKey, id))
			next.ServeHTTP(w, r)
		})
	}
}

type contextKey string

const requestIDKey contextKey = "request_id"

// AuthMiddleware calls a Yoru jwt_verify on each request; on success the
// resulting user claims are stored in the request context under userKey.
func AuthMiddleware(verifyFn *interpreter.FunctionVal, secret string, interp *interpreter.Interpreter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r)
			if token != "" {
				result, err := interp.CallFunctionWithValues(verifyFn, map[string]interpreter.Value{
					"token":  &interpreter.StringVal{V: token},
					"secret": &interpreter.StringVal{V: secret},
				})
				if err == nil {
					if opt, ok := result.(*interpreter.EnumVal); ok && opt.Variant == "Some" {
						r = r.WithContext(context.WithValue(r.Context(), userKey, opt.Fields["value"]))
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

const userKey contextKey = "user"

// AuthRequiredMiddleware rejects requests without authenticated user.
func AuthRequiredMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(userKey) == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(401)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}
