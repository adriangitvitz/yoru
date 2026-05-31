# Protocols and `impl`

A **protocol** is a behavioural contract - like a Go interface or a Rust
trait - but it can also declare which effects its methods are allowed to
use. Every method definition for an object goes through a protocol;
there are no free-standing `impl Type { ... }` blocks.

## Declaring a protocol

```yoru
protocol Serializable {
  fn serialize(self) -> Bytes effect [IO]
  fn deserialize(data: Bytes) -> Result[Self, String]
}
```

Notes:

- `self` is **bare** - no capability annotation (no `self: ref`).
- `Self` refers to the implementing type.
- Methods may declare effects with `effect [...]`.

## Implementing a protocol

The syntax is `impl ProtocolName for TypeName { ... }`:

```yoru
object User {
  id: String,
  email: String,
}

impl Serializable for User {
  fn serialize(self) -> Bytes effect [IO] {
    Bytes.from(JSON.encode(self))
  }
  fn deserialize(data: Bytes) -> Result[User, String] {
    JSON.decode(data.to_string())
  }
}
```

After this `impl`, any `User` value carries the `Serializable` methods:

```yoru
let u = User { id: "u1", email: "a@example.com" }
let bytes = u.serialize()
```

## Plain `impl Type { ... }` is also valid

If you only need to attach methods to a single type (no polymorphism),
skip the protocol and use a plain impl block. Methods land on the type
directly:

```yoru
impl User {
  fn display(self) -> String { self.id + " <" + self.email + ">" }
}
```

Use a protocol when you need callers to depend on the *shape* across
multiple types. Use a plain impl for type-local helpers.

## When to reach for a protocol

- You have **two or more** object types that need to share a method
  signature.
- You want callers to depend on the **shape**, not the concrete type.
- You want effect annotations to be part of the contract.

If only one type ever implements a behaviour and you don't need
polymorphism, a regular top-level function is cleaner than introducing
a protocol just to define a method.
