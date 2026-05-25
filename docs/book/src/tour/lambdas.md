# Lambdas and closures

Lambdas are anonymous functions. They close over variables from their
enclosing scope.

## Full form

```yoru
let add = fn(a: Int, b: Int) -> Int { a + b }
println(add(2, 3))      // 5
```

## Shorthand

When the body is a single expression, use `=>`:

```yoru
let inc = fn(x: Int) => x + 1
let double = fn(x: Int) => x * 2
```

The two forms are equivalent. Use the shorthand for one-liners passed
inline to a higher-order function.

## Higher-order usage

```yoru
let nums = [1, 2, 3, 4, 5]

let squares = map(nums, fn(x: Int) -> Int { x * x })
let evens   = filter(nums, fn(x: Int) => x % 2 == 0)
let total   = reduce(nums, 0, fn(acc: Int, x: Int) => acc + x)
```

## Capturing the environment

Lambdas capture surrounding bindings by reference. This is how the
`with_capability` builtin works:

```yoru
let mrn = "M-1"
let read = fn() => ReadPatient.run(mrn: mrn)
let record = with_capability("phi_read", read)
```

`mrn` is in scope inside the lambda even after `with_capability` invokes
it from a different stack frame.

## Closures and mutation

A closure can read mutable bindings from its parent scope, but it cannot
reassign them. If you need shared mutable state across closures, use an
actor (see [Actors](../concurrency/actors.md)). The actor model is
Yoru's official answer to "what about `let mut counter`?"
