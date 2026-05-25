# JWT auth and rate limits

Both ship as built-in middleware: declare them in the service's
`middleware: [...]` slot and they wrap every route. They communicate
with handlers via `req.context` (see [Middleware](./middleware.md)).

## JWT

```yoru
service Api {
  middleware: [JWT.verify(env("JWT_SECRET"))]
  GET  "/me"     -> me
  POST "/orders" -> create_order
  transport: .http(port: 8080)
}
```

`JWT.verify(secret)`:

- Reads `Authorization: Bearer <token>` from the request.
- Verifies the HMAC-SHA256 signature against `secret`.
- Checks that `exp` (Unix seconds) is in the future.
- On success: stamps the decoded claims as a `Map` onto
  `req.context["jwt_claims"]` and lets the request through.
- On any failure (missing header, malformed token, bad signature,
  expired): short-circuits with `401 Unauthorized` and
  `WWW-Authenticate: Bearer realm="api"`.

The handler reads claims dynamically — every field carried in the
token is available by key:

```yoru
fn me(req: Request) -> Response {
  let claims = req.context.get("jwt_claims") ?? Map.new()
  ok({
    user_id: claims.get("sub")   ?? "anon",
    email:   claims.get("email") ?? "",
    role:    claims.get("role")  ?? "guest"
  })
}
```

Use the response if you need RBAC: `if claims.get("role") != "admin"
{ return forbidden("...") }`.

### Issuing tokens

For signing, use the bundled `service/auth.yr` helpers from your Yoru
handlers:

```yoru
fn login(req: Request, body: Object) -> Response effect [Crypto, JSON, Time, DB] {
  // ...verify password against DB...
  let token = jwt_make(user.id, user.email, user.role,
                       env("JWT_SECRET"), 3600)
  ok({ token: token })
}
```

`jwt_make(sub, email, role, secret, ttl_seconds)` produces a token in
the exact format `JWT.verify` accepts.

### Compatibility note

The token format is **internally compatible**: tokens minted via
`jwt_sign` / `jwt_make` verify cleanly via `JWT.verify`. The signature
is `base64url(hex(HMAC-SHA256(secret, signing_input)))` — the extra
hex layer is a Yoru-specific quirk. If you need strict RFC 7519
compatibility (e.g. to interoperate with another service), generate
the token outside Yoru and decode it inside a handler instead of
relying on the middleware.

## Rate limiting

```yoru
service Api {
  middleware: [RateLimit.per_ip(100)]   // 100 req/sec per client IP
  GET "/search" -> search
  transport: .http(port: 8080)
}
```

Two forms:

| Form | Behaviour |
|------|-----------|
| `RateLimit.rps(n)` | One global token bucket. `n` requests/second, capacity = `n`. |
| `RateLimit.per_ip(n)` | One bucket per client IP (X-Forwarded-For preferred, then `RemoteAddr`). |

Empty bucket → `429 Too Many Requests` with body `{"error":"rate_limited"}`.

Both refill continuously — no background timer, no per-bucket
goroutine. A fresh bucket starts full (capacity = `n` tokens), so a
client gets one full second of burst before throttling.

### Picking `rps` vs. `per_ip`

| Scenario | Reach for |
|----------|-----------|
| Cap process-wide load (e.g. expensive upstream call) | `rps` |
| Stop a single client from monopolising the service | `per_ip` |
| Both: process cap + per-tenant fairness | `[RateLimit.rps(1000), RateLimit.per_ip(100)]` |

You can stack them — middleware runs in declaration order, so the global
cap rejects first, then the per-IP cap.

### Per-key limiting (not yet built-in)

`RateLimit.per_key(n, key_fn)` from earlier doc drafts is not
implemented today. The token-bucket primitive is generic — if you need
per-tenant or per-token bucketing, register a custom middleware in Go
that derives the key however you like and uses the same
`tokenBucket` shape. See [Middleware](./middleware.md#writing-your-own-middleware).

## Stacking with capability scoping

JWT (or any context-stamping middleware) composes naturally with
`with_capability`. Pull the role from `req.context`, grant the matching
capability, hand off to a shared agent:

```yoru
fn admin_chat(req: Request, body: Object) -> Response {
  let claims = req.context.get("jwt_claims") ?? Map.new()
  let role   = to_string(claims.get("role") ?? "guest")

  if role != "admin" {
    return forbidden("admin only")
  }

  let reply = with_capability("admin", fn() => my_agent.chat(body.prompt))
  ok({ reply: reply })
}

service Api {
  middleware: [JWT.verify(env("JWT_SECRET"))]
  POST "/admin/chat" -> admin_chat
  transport: .http(port: 8080)
}
```

The middleware authenticates; the handler authorises; the capability
gates the privileged tool. Each layer does one thing.
