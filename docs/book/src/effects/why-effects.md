# Why effects?

Most production bugs are not "the algorithm is wrong." They are "this code
talks to a thing it wasn't supposed to talk to": a test function that
hits a real database, a request handler that swallows a network call's
error, a billing job that secretly logs PII. Yoru's effect system makes
**every side-effecting operation visible in the type signature** and
gives you a single mechanism to handle it.

## The signature tells the story

```yoru
fn enrich(record: ref Record) -> Result[Row, Error] effect [DB, HTTP, LLM]
```

A reader knows, at a glance: this function reads/writes the database,
makes outbound HTTP calls, and asks an LLM. The compiler will refuse to
let you call it from a context that has not handled (or propagated) all
three effects.

If you remove the LLM call later, you remove `LLM` from the signature,
and every caller's required effect set shrinks accordingly. No
documentation drift.

## Why not exceptions?

Exceptions hide the control-flow graph. A function typed `Foo()` could
throw anything. You don't know without reading the entire transitive
call tree.

Effects are stricter - they force the cost into the signature. In
exchange, you get:

- **Testability.** Swap a real `HTTP` handler for a stub for free.
- **Determinism.** Provide a fixed `Clock` for time-dependent code.
- **Capability scoping.** A request handler that wraps the user agent
  inside `handle(DB) { using: read_only_pool } in ...` *cannot* write to
  the database. The type system enforces it.

## Why not async/await?

`async` is a special kind of effect (suspendable computation). Languages
that bolt async on as syntax (`async fn`, `await`) end up with the
**function colouring** problem - once a function is async, every caller
must be, too. Conversely, you cannot call an async function from a
synchronous one without an `await`.

Yoru handles suspension through the `Stream` and `IO` effects. You
never write `async` or `await`. A function that issues an HTTP call
looks like a normal function; the runtime handles fibre suspension under
the hood.

```yoru
fn fetch(url: String) -> String effect [HTTP] {
  let r = HTTP.get(url)    // non-blocking fibre suspend
  r.body
}

fn handle_request(req: Request) -> Response effect [HTTP, DB] {
  let body = fetch(req.url)        // no `await`
  DB.exec("INSERT INTO requests (body) VALUES ($1)", [body])
  ok(body)
}
```

The signature of `handle_request` accumulates both effects automatically.

## How much do I have to write?

Less than you'd think. Yoru **infers** effects through every call site,
so you only declare them when:

1. You are writing the public face of a function and want the doc value.
2. The compiler tells you a callee uses an effect the caller does not
   declare.

For internal helpers, you can often omit `effect [...]` entirely and let
inference do the work. For tools, agents, and services, declaring effects
is the contract - write them explicitly.

## Next

[Declaring and handling effects](./declaring-and-handling.md) walks
through the syntax for handlers.
