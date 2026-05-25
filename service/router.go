package service

import (
	"fmt"
	"strings"
)

// PathSegment represents one segment of a URL pattern.
type PathSegment struct {
	Value   string // literal value or param name (without ':')
	IsParam bool
}

// Route is a registered route with parsed pattern segments.
type Route struct {
	Method   string
	Pattern  string
	Handler  string
	Segments []PathSegment
}

// Match is the result of a successful route match.
type Match struct {
	Route  *Route
	Params map[string]string
}

// DuplicateRouteError is returned when the same method+pattern is registered twice.
type DuplicateRouteError struct {
	Method  string
	Pattern string
}

func (e *DuplicateRouteError) Error() string {
	return fmt.Sprintf("duplicate route: %s %s", e.Method, e.Pattern)
}

// Router matches HTTP requests to registered routes.
type Router struct {
	routes []*Route
}

// NewRouter creates an empty router.
func NewRouter() *Router {
	return &Router{}
}

// Register adds a route to the router.
func (r *Router) Register(method, pattern, handler string) error {
	for _, existing := range r.routes {
		if existing.Method == method && existing.Pattern == normalizePath(pattern) {
			return &DuplicateRouteError{Method: method, Pattern: pattern}
		}
	}

	segments := parsePattern(normalizePath(pattern))
	r.routes = append(r.routes, &Route{
		Method:   method,
		Pattern:  normalizePath(pattern),
		Handler:  handler,
		Segments: segments,
	})
	return nil
}

// Match finds a route matching the given method and path.
func (r *Router) Match(method, path string) *Match {
	path = normalizePath(path)
	pathParts := splitPath(path)

	for _, route := range r.routes {
		if route.Method != method {
			continue
		}
		if len(route.Segments) != len(pathParts) {
			continue
		}

		params := make(map[string]string)
		matched := true
		for i, seg := range route.Segments {
			if seg.IsParam {
				params[seg.Value] = pathParts[i]
			} else if seg.Value != pathParts[i] {
				matched = false
				break
			}
		}

		if matched {
			return &Match{Route: route, Params: params}
		}
	}

	return nil
}

// parsePattern splits a pattern like "/orders/:id" into segments.
func parsePattern(pattern string) []PathSegment {
	parts := splitPath(pattern)
	segments := make([]PathSegment, len(parts))
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			segments[i] = PathSegment{Value: part[1:], IsParam: true}
		} else {
			segments[i] = PathSegment{Value: part, IsParam: false}
		}
	}
	return segments
}

// splitPath splits a path into non-empty segments.
func splitPath(path string) []string {
	var parts []string
	for p := range strings.SplitSeq(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// normalizePath removes trailing slashes (preserving root "/").
func normalizePath(path string) string {
	if path == "/" || path == "" {
		return "/"
	}
	return strings.TrimRight(path, "/")
}
