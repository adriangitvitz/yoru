# Bindings and functions

## `let` and `mut`

Yoru has two binding forms.

```yoru
let x = 10            // immutable
let name = "ada"

mut counter = 0       // mutable
counter = counter + 1
counter += 5          // shorthand
```

`let` is the default; reach for `mut` only when you genuinely need to
reassign. Pattern matching, `for ... in`, and `match` are the idiomatic
ways to thread state through a program without `mut`.

## Functions

```yoru
fn add(a: Int, b: Int) -> Int {
  a + b
}
```

- Parameters require an explicit type.
- The return type goes after `->`.
- The **last expression** of a block is its value. No `return` needed for
  the simple case.

`return` exists for early exit:

```yoru
fn first_positive(xs: [Int]) -> Int {
  for x in xs {
    if x > 0 { return x }
  }
  return 0
}
```

## Effects in the signature

Any function that performs I/O, talks to a database, calls an LLM, etc.,
declares the effect:

```yoru
fn fetch_user(id: String) -> String effect [HTTP] {
  let r = HTTP.get("/users/" + id)
  match r {
    Err(e) => "error: " + to_string(e)
    resp => resp.body
  }
}
```

The compiler infers and checks effects through every call site. If
`fetch_user` calls a function that also uses `[DB]`, the type checker
will require you to either declare `[HTTP, DB]` or handle the `DB` effect
somewhere along the chain. See [Why effects?](../effects/why-effects.md).

## `fn main`

If your file declares `fn main()`, `yoru run` will call it after evaluating
all top-level statements. You can put helpers, type declarations, and
constants at the top level, and use `main` for the entry point logic.

```yoru
let GREETING = "hello"

fn shout(s: String) -> String {
  uppercase(s) + "!"
}

fn main() {
  println(shout(GREETING))
}
```
