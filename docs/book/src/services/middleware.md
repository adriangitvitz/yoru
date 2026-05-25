# Middleware

`middleware: [...]` on a service block wraps every route in the listed
middleware, in order. Names can be **bare** (`Logger`) or
**parameterised** (`CORS.allow_origin("https://app.example.com")`).

## Built-in middleware

| Form | Behaviour |
|------|-----------|
| `Logger` | Per-request access log to stderr (method, path, status, duration). |
| `CORS` | Wide-open CORS (`Access-Control-Allow-Origin: *`). |
| `CORS.allow_origin("https://x.example")` | Single allowed origin. |
| `CORS.allow_origins(["https://a.example", "https://b.example"])` | Allow-list. |
| `Recover` | Catches handler panics, returns 500. |
| `RequestID` | Adds an `X-Request-ID` header if not already present. |
| `JWT.verify(secret)` | HMAC-SHA256 bearer-token check; stamps claims on `req.context["jwt_claims"]`; 401 on failure. See [JWT auth and rate limits](./auth-and-rate-limits.md). |
| `RateLimit.rps(n)` | Global token bucket: `n` requests/second; 429 on overflow. |
| `RateLimit.per_ip(n)` | One token bucket per client IP. |

Example with a mix:

```yoru
service API {
  middleware: [
    RequestID,
    Logger,
    CORS.allow_origin("https://app.example.com"),
    Recover,
  ]
  GET "/health" -> health_check
  transport: .http(port: 8080)
}
```

Middleware runs in **declaration order on the way in**, reverse on the
way out. Put `Logger` early so it sees the final status code; put
`Recover` close to the handler so it catches panics from your code but
not from upstream middleware errors.

## Writing your own middleware

Middleware is registered from Go via `service.RegisterMiddleware`. The
factory receives the parsed `MiddlewareRef` (name + method + args) and
the interpreter handle, and returns a standard `func(http.Handler) http.Handler`.

```go
import (
  "net/http"
  "github.com/adriangitvitz/yoru/interpreter"
  "github.com/adriangitvitz/yoru/parser"
  "github.com/adriangitvitz/yoru/service"
)

func init() {
  service.RegisterMiddleware("TraceID", func(ref parser.MiddlewareRef, _ *interpreter.Interpreter) service.Middleware {
    return func(next http.Handler) http.Handler {
      return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        traceID := r.Header.Get("X-Trace-Id")
        if traceID == "" {
          traceID = generateID()
        }
        r = service.SetRequestContext(r, "trace_id", &interpreter.StringVal{V: traceID})
        w.Header().Set("X-Trace-Id", traceID)
        next.ServeHTTP(w, r)
      })
    }
  })
}
```

Once registered, the name is usable in any `service` declaration:

```yoru
service API {
  middleware: [TraceID]
  GET "/" -> handler
  transport: .http(port: 8080)
}
```

User registrations **override** built-ins with the same name — useful
for replacing the stock `Logger` with one that talks to your structured
log pipeline. To remove a registration (mostly for tests), call
`service.UnregisterMiddleware("Name")`.

## Surfacing values to handlers via `req.context`

Middleware that derives a value from the incoming request (auth claims,
user ID, trace ID, request start time) writes it onto the Go-side
request with `SetRequestContext(r, "key", value)`. Yoru handlers read
the same value via `req.context.get("key")`:

```yoru
fn me(req: Request) -> Response {
  let user = req.context.get("user_id") ?? "anonymous"
  ok(user)
}
```

`req.context` is a `Map<String, Value>`. It's always present (empty by
default); `get(...)` returns `nil` for missing keys, so the `??`
fallback pattern is idiomatic.

See [JWT auth and rate limits](./auth-and-rate-limits.md) for a worked
example where a JWT middleware verifies the bearer token and stamps the
claims onto `req.context`.
