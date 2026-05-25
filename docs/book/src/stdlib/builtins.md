# Builtins reference

Functions always in scope — no import required.

## I/O

| Function | Purpose |
|----------|---------|
| `println(v)`  | Print `v` (any type) plus a newline.            |
| `print(v)`    | Print `v` without a newline.                    |
| `env(name)`   | Read environment variable `name`. Returns `""` if unset. |

## Conversion

| Function | Purpose |
|----------|---------|
| `to_string(v)` | Convert any value to a printable `String`. Whole-valued floats drop `.0`. |
| `int(s)`       | Parse `String` → `Int`.                         |
| `float(s)`     | Parse `String` → `Float`.                       |
| `type_of(v)`   | Return the type name of `v` as a `String`.      |

## Math

`abs`, `min`, `max`, `pow`, `sqrt`, `floor`, `ceil`, `round`.

```yoru
let h = sqrt(pow(3.0, 2.0) + pow(4.0, 2.0))   // 5
```

## Lists

| Function | Purpose |
|----------|---------|
| `len(xs)`             | Length of a list, string, or bytes. |
| `append(xs, v)`       | Returns a new list with `v` appended. |
| `slice(xs, lo, hi)`   | Sublist from `lo` (inclusive) to `hi` (exclusive). |
| `map(xs, f)`          | Apply `f` to each element.          |
| `filter(xs, pred)`    | Keep elements where `pred(x)` is true. |
| `reduce(xs, init, f)` | Fold left.                          |
| `sort(xs)`            | Return a sorted copy.               |
| `reverse(xs)`         | Return a reversed copy.             |
| `flatten(xss)`        | Concatenate a list of lists.        |
| `zip(a, b)`           | Pair elements from two lists.       |
| `range(n)`            | `[0, 1, ..., n-1]`. |
| `range(lo, hi)`       | `[lo, lo+1, ..., hi-1]`. Empty if `hi <= lo`. |
| `contains(xs, v)`     | Whether `xs` contains `v`.          |
| `find(xs, pred)`      | First element matching `pred`.      |
| `index_of(xs, v)`     | Index of `v` or `-1`.               |
| `join(xs, sep)`       | Join a list of strings.             |
| `repeat(v, n)`        | List of `v` repeated `n` times.     |

## Strings

`uppercase`, `lowercase`, `trim`, `split`, `replace`, `starts_with`,
`ends_with`, `contains`, `index_of`, `char_at`.

## Maps

Two **namespace** constructors:

| Function | Purpose |
|----------|---------|
| `Map.new()`             | Empty map. |
| `Map.of(k1, v1, k2, v2, ...)` | Build a map from alternating key/value arguments. Keys are `String`. |
| `Map.of(obj)`           | Build a map from a (bare or typed) object literal's fields. |

Everything else is a **method on the map value**. `set` and `delete`
return a new map (the original is unchanged):

- `m.get(k)`, `m.has(k)`
- `m.set(k, v)` → new map with `k`/`v` (overwrite or insert)
- `m.delete(k)` → new map without `k`
- `m.keys()`, `m.values()`, `m.entries()`, `m.size()`

There is also a top-level helper `contains_key(m, k)` that mirrors
`m.has(k)`.

## Bytes

Three constructors in the `Bytes` namespace:

| Function | Purpose |
|----------|---------|
| `Bytes.from(s)`     | `String -> Bytes`. |
| `Bytes.from_hex(s)` | Parse hex string to bytes. |
| `Bytes.new(n)`      | `n` zero bytes. |

Everything else is a **method on the byte value**: `b.len()`,
`b.slice(lo, hi)` (returns `Bytes`), `b.to_string()`, `b.to_hex()`.

## Collector (pipeline sink)

`Collector.collect()` — produces a list from all items reaching the sink.

## Capability and supervision

`with_capability(name, fn() => ...)` — grant `name` for the duration of
the lambda.

`Supervisor.new(names, strategy, max_restarts, window_seconds)` —
general-purpose supervision over actors and agents. Returns a
`SupervisorVal` with `.start()`, `.stop()`, `.children()`,
`.add_child(name)` methods. See
[Supervision](../concurrency/supervision.md).

`supervise_agents(names, strategy, max_restarts, window_seconds)` —
agent-only convenience: builds, starts, and returns the name→ref map
in one call.

## Tool values

A bare tool name (`MyTool`) resolves to a `Tool` value. Method dispatch:

| Method | Returns |
|--------|---------|
| `t.run(args)` | The tool's return value (validated if `output { ... }`) |
| `t.name()` | `String` |
| `t.description()` | `String` |
| `t.input_schema()` | Input JSON Schema as `String` |
| `t.output_schema()` | Output JSON Schema as `String`, or `nil` if no `output { ... }` block |
| `t.schema()` | Full MCP-shape tool schema as `String` |

See [Tools](../agents/tools.md) for the declaration form and patterns.

## HTTP response helpers

`ok(body)`, `created(body)`, `no_content()`, `bad_request(msg)`,
`unauthorized(msg)`, `forbidden(msg)`, `not_found(msg)`,
`internal_error(msg)`.

The `body` can be a named object, list, primitive, a map built with
`Map.of(...)`, **or a bare `{ key: value }` literal** — bare literals
produce a generic `Object` value.

## Misc

`assert(cond)` — abort if `cond` is false. Use only at startup or in
tests; production code should branch and return `Result.Err` instead.

`hash(v)` — deterministic 64-bit hash of any value.
