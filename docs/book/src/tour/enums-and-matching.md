# Enums and pattern matching

Enums (also called sum types or tagged unions) describe a value that is
*one of* several named variants. They are how Yoru models choice.

## Declaring

```yoru
enum Status {
  Active(score: Int)
  Pending
  Inactive(reason: String)
}
```

Each variant may have zero or more named fields.

## Constructing

```yoru
let s1 = Status.Active(score: 95)
let s2 = Status.Pending
let s3 = Status.Inactive(reason: "expired")
```

## Pattern matching

`match` is exhaustive: the compiler errors if you forget a variant.

```yoru
fn describe(s: Status) -> String {
  match s {
    Active(score)         => "active with " + to_string(score)
    Pending               => "pending"
    Inactive(reason)      => "inactive: " + reason
  }
}
```

Arms can have **guards**:

```yoru
fn label(n: Int) -> String {
  match n {
    x if x < 0   => "negative"
    0            => "zero"
    x if x < 10  => "small"
    _            => "large"
  }
}
```

`_` is the wildcard. A bare lowercase identifier binds the whole subject
to that name, which is what makes the `r => r.field` idiom work.

## Matching values, not just enums

`match` works on any value:

```yoru
match HTTP.get(url) {
  Err(e) => println("network error: " + to_string(e))
  r      => println("status: " + to_string(r.status))
}
```

The pattern `r =>` binds the whole `Ok` payload to `r`, and field access
just works.

## Supported pattern forms

| Form | Example |
|------|---------|
| Literal | `0`, `"x"`, `true` |
| Wildcard | `_` |
| Binding | `x` (any lowercase identifier; binds the whole subject) |
| Variant constructor | `Active(score)`, `Inactive(reason)` - fields bound in declaration order |
| Variant with no fields | `Pending` |
| Object destructure (shorthand) | `Point { x, y }` - binds `x` and `y` to the object's field values |
| Object destructure (literal match) | `Point { x: 0, y: 0 }` - matches only when both fields equal the literal |
| Object destructure (rebind) | `Point { x: a, y: b }` - binds the field's value to a different name |

## Object destructure in practice

```yoru
object Point { x: Int, y: Int }

fn label(p: Point) -> String {
  match p {
    Point { x: 0, y: 0 } => "origin"
    Point { x: 0, y }    => "on y-axis at " + to_string(y)
    Point { x, y: 0 }    => "on x-axis at " + to_string(x)
    Point { x, y }       => "(" + to_string(x) + ", " + to_string(y) + ")"
  }
}
```

If the subject is not of the named type, the arm doesn't match - so
destructure patterns also serve as type guards inside a `match` with
mixed inputs.
