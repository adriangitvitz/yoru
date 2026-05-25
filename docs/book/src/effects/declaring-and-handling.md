# Declaring and handling effects

## Declaring on a function

Add `effect [...]` after the return type. The list may be empty (which
documents that the function is pure with respect to tracked effects):

```yoru
fn pure_double(x: Int) -> Int effect [] { x * 2 }

fn read_user(id: String) -> User effect [DB] {
  DB.find(User, id)
}

fn forward(id: String) -> String effect [DB, HTTP] {
  let u = read_user(id)
  let r = HTTP.get(u.callback_url)
  r.body
}
```

The compiler computes the effect set of every call site and either:

- **succeeds** if the caller's declared effects cover the callee's, or
- **errors** with the missing effect names.

## Declaring a custom effect

```yoru
effect Metrics {
  count(name: String, n: Int) -> Void
  observe(name: String, v: Float) -> Void
}
```

Business code uses it without knowing where metrics actually go:

```yoru
fn handle_order(o: Order) -> Void effect [Metrics, DB] {
  Metrics.count("orders.received", 1)
  DB.insert("orders", o)
}
```

## Handlers

A `handle` block intercepts an effect and runs the body with the provided
implementation in scope:

```yoru
let result = handle(Metrics) {
  using: PrometheusSink.new(endpoint: "http://prom:9090")
} in {
  handle_order(my_order)
}
```

In tests, swap the handler for an in-memory recorder:

```yoru
let collected = handle(Metrics) {
  using: TestSink.new()
} in {
  handle_order(my_order)
}
assert(collected.count("orders.received") == 1)
```

The body runs with the alternative implementation. Outside the `handle`
block, the original (or no) implementation applies.

## Effect propagation in practice

The most common case is **not** writing custom effects — it is wiring up
the built-in ones. See [Built-in effects](./built-in-effects.md) for the
full catalogue (`HTTP`, `DB`, `LLM`, `Log`, `JSON`, `Crypto`, `Time`, and
the optional message-bus effects).

## Capability scoping

Tools declare a capability and require callers to grant it:

```yoru
let r = with_capability("phi_read", fn() => ReadPatient.run(mrn: "M-1"))
```

This is conceptually the same shape as effect handling — a region of code
runs with a named capability on the stack — but it goes through the
`with_capability` builtin rather than the `handle` keyword because the
runtime needs to enforce it dynamically (a tool may be invoked by an LLM
at runtime, not by a function call in source). See
[Capability scoping](../agents/capability-scoping.md).
