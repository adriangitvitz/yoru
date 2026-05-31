# Objects

Objects are named records. They hold state. They do not have inheritance.

## Declaring

```yoru
object User {
  id: String,
  email: String,
  active: Bool,
}
```

## Constructing

Use the type name followed by `{ field: value, ... }`:

```yoru
let u = User { id: "u1", email: "a@example.com", active: true }
println(u.email)        // a@example.com
```

Field access uses dot notation.

Bare `{ key: value, ... }` literals also parse - they produce a generic
`Object` value (the same shape `JSON.decode` returns). Use a typed
object when you want field-level checking; use a bare literal for an
ad-hoc record:

```yoru
let cfg = { host: "localhost", port: 8080 }
println(cfg.host)       // localhost
```

## Updating

Objects are immutable. To "update" a field, build a new value:

```yoru
let archived = User {
  id: u.id,
  email: u.email,
  active: false,
}
```

There is no spread syntax for field copy at the moment, so you list every
field explicitly. This is verbose on purpose - it makes refactors loud.

## Methods via `impl`

Two impl shapes work:

```yoru
// Plain methods - attach behaviour without a protocol.
impl User {
  fn display(self) -> String {
    self.email + (if self.active { " (active)" } else { " (archived)" })
  }
}

println(u.display())
```

```yoru
// Protocol implementation - required when callers depend on the
// behaviour-by-shape rather than the concrete type.
protocol Display { fn show(self) -> String }

impl Display for User {
  fn show(self) -> String { u.display() }
}
```

The first parameter is **bare `self`** (no capability annotation in the
source today). Methods are called with dot syntax.

## Delegation (composition over inheritance)

You can embed one object inside another and automatically expose its
protocols.

```yoru
object AdminUser {
  delegate base: User,
  permissions: [String],
}

let a = AdminUser {
  base: User { id: "u2", email: "root@example.com", active: true },
  permissions: ["read", "write"],
}
```

The `delegate` field makes the outer object behave as if it implemented
every protocol that `User` implements. If `User` has a `Display` impl,
`AdminUser` gets that impl for free.

## When to use an object vs. an enum

- **Object** when the shape is fixed and every instance has the same set
  of fields. (Users, requests, responses, config.)
- **Enum** when an instance is *one of* several variants with different
  fields. (Results, events, parser AST.) See
  [Enums and pattern matching](./enums-and-matching.md).
