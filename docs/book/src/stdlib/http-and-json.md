# HTTP and JSON

## `HTTP`

```yoru
let r  = HTTP.get("https://example.com/users/1")
let r2 = HTTP.post("https://example.com/users",
                   JSON.encode(User { name: "Ada" }))
```

Both return a `Result`. On success, the value is:

```yoru
Response {
  status: Int,
  headers: Map<String, String>,
  body: String,
}
```

Both accept an **optional headers `Map`** as a final argument:

```yoru
let headers = Map.of("Authorization", "Bearer " + token,
                     "X-Trace-Id",     trace_id)
let r = HTTP.get(url, headers)
let r2 = HTTP.post(url, body, headers)
```

| Default | Value |
|---------|-------|
| Timeout       | 30 seconds |
| `User-Agent`  | `yoru/0.1` (`DefaultUserAgent` in `stdlib/http.go`) |

For custom timeouts or TLS config, construct an `HTTPProvider` in Go
before launching the interpreter (see `stdlib/http.go`).

### Common pattern

```yoru
fn fetch_status(url: String) -> Int effect [HTTP] {
  let r = HTTP.get(url)
  match r {
    Err(e) => { Log.warn("http " + e.kind); -1 }
    resp   => resp.status
  }
}
```

## `JSON`

| Function | Purpose |
|----------|---------|
| `JSON.encode(v)`       | Any Yoru value → JSON string.       |
| `JSON.decode(s)`       | JSON string → Yoru value (`ObjectVal`, `ListVal`, or primitive). |
| `JSON.get(v, key)`     | Read a field from a decoded JSON object. |
| `JSON.pretty(v)`       | Encode with indentation.            |
| `JSON.merge(a, b)`     | Shallow merge: keys from `b` override `a`. |

### Encoding requires a typed value

`JSON.encode` takes any Yoru value, but **object literals always need a
type name**:

```yoru
object User { id: Int, name: String }

let s = JSON.encode(User { id: 1, name: "Ada" })   // {"id":1,"name":"Ada"}
```

A bare `{ id: 1, name: "Ada" }` does **not** parse. When you need an
ad-hoc dictionary, build it with `Map.of(...)` and encode that:

```yoru
let s = JSON.encode(Map.of("id", 1, "name", "Ada"))
```

### Decoding

```yoru
let s    = "{\"id\":1,\"name\":\"Ada\"}"
let back = JSON.decode(s)              // generic ObjectVal (TypeName="Object")
let nm   = JSON.get(back, "name")      // "Ada"
let id   = int(to_string(JSON.get(back, "id")))
```

### Decoding with a type name

Two forms validate against a declared `object` type and re-tag the
resulting ObjectVal so downstream code sees the typed value:

```yoru
object User { id: Int, name: String }

// 1. Explicit second argument
let a = JSON.decode("{\"id\":1,\"name\":\"Ada\"}", "User")

// 2. Let-binding type annotation — equivalent to the above
let b: User = JSON.decode("{\"id\":1,\"name\":\"Ada\"}")
```

Both produce an `ObjectVal` with `TypeName="User"` and field access
that works the same way as `User { id: 1, name: "Ada" }`.

If any declared field is missing from the JSON, the call returns
`Result.Err{kind: "json_decode_failed", message: "...missing required
field 'X'"}`. Pair with `?`, `??`, or `match` at the call site:

```yoru
let u: User = JSON.decode(req.body)
match u {
  Err(e) => bad_request("invalid payload: " + e.message)
  user   => ok(user)
}
```

The annotation form is **passive** — if the RHS already produced a
typed ObjectVal (e.g. `let u: User = OtherType { ... }`), the
annotation does **not** clobber the existing tag. Use the explicit
two-arg form when you need the coercion to happen unconditionally.
