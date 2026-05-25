# REST handlers

A `service` declaration produces an HTTP server. Routes map verbs and
paths to handler functions defined elsewhere in the same file.

## Minimal example

```yoru
object Order { id: String, item: String }

fn list_orders(req: Request) -> Response {
  ok([Order { id: "1", item: "Widget" }])
}

fn get_order(req: Request, id: String) -> Response {
  ok(Order { id: id, item: "Widget" })
}

service OrderAPI {
  GET  "/orders"     -> list_orders
  GET  "/orders/:id" -> get_order
  transport: .http(port: 8080)
}
```

Run with `yoru run` — the service auto-starts on port 8080.

## Path parameters

`:id` in the route path is passed as an extra positional argument to the
handler:

```yoru
GET "/users/:user_id/orders/:order_id" -> get_specific_order
```

```yoru
fn get_specific_order(req: Request, user_id: String, order_id: String) -> Response { ... }
```

## Request body

Handlers that accept a body declare it as the second parameter. The
runtime decodes JSON request bodies into a Yoru value and binds it to
the `body` argument:

```yoru
object CreateOrder { item: String }

fn create_order(req: Request, body: Object) -> Response effect [Crypto] {
  let item = JSON.get(body, "item")
  created(Order { id: Crypto.random_hex(8), item: to_string(item) })
}

service OrderAPI {
  POST "/orders" -> create_order
  transport: .http(port: 8080)
}
```

`body` arrives as a dynamic `ObjectVal`. Use `JSON.get(body, "field")`
to pull fields, then construct your typed object explicitly — there is
no automatic decoding into a named `object` type today (see
[HTTP and JSON](../stdlib/http-and-json.md)).

## Response helpers

| Helper | HTTP status | Use for |
|--------|-------------|---------|
| `ok(body)`              | 200 | Success with a payload |
| `created(body)`         | 201 | Resource created       |
| `no_content()`          | 204 | Success with no body   |
| `bad_request(msg)`      | 400 | Client validation error |
| `unauthorized(msg)`     | 401 | Missing or invalid auth |
| `forbidden(msg)`        | 403 | Authenticated but not allowed |
| `not_found(msg)`        | 404 | No such resource       |
| `internal_error(msg)`   | 500 | Unexpected server fault |

Each helper takes care of `Content-Type: application/json` for you.

The `body` can be a named object, list, primitive, a map built with
`Map.of(...)`, or a bare `{ key: value }` literal — the helper handles
each shape and emits JSON appropriately.

## Reading values stamped by middleware

Middleware can derive values from the request (auth claims, user ID,
trace ID) and stamp them onto `req.context` for handlers to read:

```yoru
fn me(req: Request) -> Response {
  let user_id = req.context.get("user_id") ?? "anonymous"
  ok(WhoAmI { user_id: user_id })
}
```

`req.context` is always a `Map` — empty when no middleware has written
to it. Missing keys return `nil`, so the `??` fallback handles both
"middleware didn't run" and "middleware ran but didn't stamp this key"
the same way. See [Middleware](./middleware.md#surfacing-values-to-handlers-via-reqcontext)
for the writer side.

## Effects in handlers

Handlers can declare effects in their signature just like any function:

```yoru
fn list_orders(req: Request) -> Response effect [DB, Log] {
  Log.info("listing orders")
  let rows = DB.query("SELECT id, item FROM orders LIMIT 100")
  ok(rows)
}
```

The compiler ensures every effect the handler uses is either declared
or handled.

## Transports

```yoru
transport: .http(port: 8080)
transport: .http(port: 8080, host: "127.0.0.1")  // bind to loopback only
transport: .http(host: "0.0.0.0", port: 9090)    // arg order doesn't matter
```

`port:` is required. `host:` is optional and defaults to the empty
string, which binds **all interfaces** (the historical behaviour).
Pass `"127.0.0.1"` to limit the listener to loopback, `"0.0.0.0"` to be
explicit about binding everything, or a specific interface IP to pin
the listener.

For HTTPS, terminate TLS at a reverse proxy in front of the service.
