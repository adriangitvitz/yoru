# Errors and `Result`

Yoru has no exceptions. Functions that can fail return `Result[T, E]`:

```yoru
enum Result[T, E] {
  Ok(value: T)
  Err(error: E)
}
```

The standard error shape used by the runtime is:

```yoru
object Error { kind: String, message: String }
```

Common `kind` values produced by the runtime:

| Source | Kind |
|--------|------|
| Integer divide-by-zero | `div_by_zero` |
| List / string / bytes index out of range | `index_out_of_bounds` |
| `actor.ask(...)` timed out | `ask_timeout` |
| Sending to a stopped actor (mailbox closed) | `actor_stopped` |
| `agent.chat(...)` timed out | `chat_timeout` |
| Agent reasoning loop failed | `agent_error` |
| LLM client not configured | `llm_not_configured` |
| HTTP request failed (timeout, bad URL, refused) | `http_request_failed` |
| `JSON.decode(body, "T")` missing field or unknown type | `json_decode_failed` |
| Tool called outside its capability scope | `capability_denied` |
| `DB.transaction` body panicked | `db_transaction_panic` |
| Supervisor input validation (bad strategy, unknown child name) | `supervisor_bad_args` |
| Supervisor failed to spawn a child | `supervisor_start_failed` |

## The `?` operator

`?` is **postfix**. On `Ok(v)` it unwraps to `v`. On `Err(e)` it returns
early from the enclosing function with that `Err`.

```yoru
fn safe_div(a: Int, b: Int) -> Int {
  let q = (a / b)?     // div_by_zero propagates out as Err
  q + 1
}
```

The `?` operator passes plain (non-`Result`) values through unchanged,
which is what makes inline arithmetic with `?` ergonomic.

## The `??` operator

`??` is **infix**. It supplies a fallback when the left side is `Err`,
`None`, or `nil`.

```yoru
let q = (10 / divisor) ?? 0
let m = Map.of("name", "Ada")
let name = m.get("missing") ?? "anonymous"
```

The fallback is evaluated lazily - if the left side is `Ok`/some, the
right side is not touched.

## Pattern matching for richer recovery

When you want to inspect the error before deciding, use `match`.

```yoru
match HTTP.get(url) {
  Err(e) if e.kind == "http_request_failed" => {
    Log.warn("retrying " + url)
    HTTP.get(url)
  }
  Err(e) => {
    Log.error("giving up: " + e.kind)
    internal_error("upstream failed")
  }
  r => r
}
```

## Don't catch what shouldn't be caught

If a function returns `Result`, it is saying *"this can fail in a way the
caller might recover from."* Internal invariants (impossible-by-construction
states) should remain ordinary code paths, not wrapped in `Result`. The
type checker is already enforcing them; do not water down the signal.
