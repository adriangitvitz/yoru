# Primitive types

The Yoru primitives match what you'd expect from a typical statically
typed language.

| Type | Examples | Notes |
|------|----------|-------|
| `Int` | `0`, `42`, `-7` | 64-bit signed |
| `Float` | `3.14`, `-0.5`, `1.0e6` | 64-bit IEEE-754 |
| `Bool` | `true`, `false` | |
| `String` | `"hello"`, `"line\nbreak"` | UTF-8; `+` concatenates |
| `Bytes` | `Bytes.from("abc")`, `Bytes.from_hex("deadbeef")` | Raw byte buffer |
| `nil` | `nil` | The unit-style absence value |

## String escapes

`\n`, `\t`, `\r`, `\\`, `\"`, and `\0` are the recognised escapes.

```yoru
println("first\nsecond\ttabbed")
```

## Numeric conversion

```yoru
let n: Int = int("42")          // String -> Int (panics on bad input)
let f: Float = float("3.14")    // String -> Float
let s: String = to_string(123)  // any value -> String
```

`to_string` works on every Yoru value, including objects, lists, and
`Result`. It is your friend when wiring `println` for debugging.

`to_string` of a whole-valued `Float` (e.g. `5.0`) prints as `5` — the
trailing `.0` is dropped. Pass through `float(...)` and format yourself
if you need the fractional part preserved.

## Math builtins

`abs`, `min`, `max`, `pow`, `sqrt`, `floor`, `ceil`, `round`.

```yoru
let h = sqrt(pow(3.0, 2.0) + pow(4.0, 2.0))
println(to_string(h))    // 5
```

## Bytes

Bytes ops are **methods on the byte value**, not namespaced functions.

```yoru
let b = Bytes.from("hello")
println(to_string(b.len()))   // 5
println(b.to_hex())           // 68656c6c6f
println(b.to_string())        // hello
let chunk = b.slice(1, 4)     // Bytes (not String); use .to_string()
println(chunk.to_string())    // ell
```

`Bytes.from(s)`, `Bytes.from_hex(s)`, and `Bytes.new(n)` are the
**constructors** in the `Bytes` namespace. Everything else hangs off
the value: `b.len()`, `b.to_string()`, `b.to_hex()`, `b.slice(lo, hi)`.
