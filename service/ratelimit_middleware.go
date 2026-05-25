package service

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/adriangitvitz/yoru/parser"
)

// tokenBucket is a single-flow rate-limit bucket. Capacity equals rps so a
// fresh client gets one second of burst. Refill is continuous, computed on
// each Allow call — no background goroutine, no per-bucket timer.
type tokenBucket struct {
	mu       sync.Mutex
	capacity float64
	tokens   float64
	rps      float64
	last     time.Time
}

func newBucket(rps int) *tokenBucket {
	r := float64(rps)
	return &tokenBucket{
		capacity: r,
		tokens:   r,
		rps:      r,
		last:     time.Now(),
	}
}

// Allow consumes one token if available. Returns true when the request
// should proceed, false when the bucket is empty.
func (b *tokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = minF(b.capacity, b.tokens+elapsed*b.rps)
	b.last = now
	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// RateLimitMiddleware enforces a process-wide token bucket, returning 429
// when empty.
func RateLimitMiddleware(rps int) Middleware {
	if rps <= 0 {
		// No-op when rate is missing or zero (documented behavior).
		return func(next http.Handler) http.Handler { return next }
	}
	bucket := newBucket(rps)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !bucket.Allow() {
				writeTooManyRequests(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitPerIPMiddleware enforces one bucket per client IP (X-Forwarded-For
// first, then r.RemoteAddr). The bucket map grows unbounded — fine for short-
// lived services, needs pruning for long-lived ones with high IP turnover.
func RateLimitPerIPMiddleware(rps int) Middleware {
	if rps <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	var mu sync.Mutex
	buckets := make(map[string]*tokenBucket)

	getBucket := func(ip string) *tokenBucket {
		mu.Lock()
		defer mu.Unlock()
		b, ok := buckets[ip]
		if !ok {
			b = newBucket(rps)
			buckets[ip] = b
		}
		return b
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !getBucket(ip).Allow() {
				writeTooManyRequests(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP returns the originating IP, honoring X-Forwarded-For (leftmost,
// per RFC 7239) and falling back to r.RemoteAddr with the port stripped.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if before, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(before)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeTooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
}

func init() {
	RegisterMiddleware("RateLimit", func(ref parser.MiddlewareRef, interp *interpreter.Interpreter) Middleware {
		var rps int
		if len(ref.Args) > 0 && interp != nil {
			v := interp.EvalExpressionPublic(ref.Args[0])
			if iv, ok := v.(*interpreter.IntVal); ok {
				rps = int(iv.V)
			}
		}
		switch ref.Method {
		case "rps":
			return RateLimitMiddleware(rps)
		case "per_ip":
			return RateLimitPerIPMiddleware(rps)
		}
		// Bare or unknown method → no-op: unconfigured limit must not block.
		return RateLimitMiddleware(0)
	})
}
