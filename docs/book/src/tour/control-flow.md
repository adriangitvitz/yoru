# Control flow

## `if` / `else`

`if` is an expression. The branches must have compatible types.

```yoru
let label = if score >= 70 { "pass" } else { "fail" }
```

### `else if` chains

```yoru
if x < 0 {
  println("negative")
} else if x == 0 {
  println("zero")
} else {
  println("positive")
}
```

For multi-way decisions, prefer [`match`](./enums-and-matching.md) with
guards - it is exhaustive and the indentation stays flat.

## `for`

`for x in iterable` walks the iterable and binds each element to `x`:

```yoru
for n in [1, 2, 3] {
  println(to_string(n))
}

for i in range(5)    { println(to_string(i)) }   // 0..4
for i in range(2, 6) { println(to_string(i)) }   // 2..5
```

`continue` skips to the next iteration. `break` exits the loop.

```yoru
for n in range(10) {
  if n == 3 { continue }
  if n == 7 { break }
  println(to_string(n))
}
```

## `while`

```yoru
mut i = 0
while i < 5 {
  println(to_string(i))
  i += 1
}
```

Same `break` and `continue` semantics as `for`.

## `match`

See [Enums and pattern matching](./enums-and-matching.md). `match` is the
exhaustive, type-checked structural form. Prefer it over `if`/`else`
chains whenever you are switching on a tagged value - and especially in
place of the missing `else if`.

## Early `return`

`return` exits the enclosing `fn`. There is no labelled break.

```yoru
fn first_negative(xs: [Int]) -> Int {
  for x in xs {
    if x < 0 { return x }
  }
  return 0
}
```
