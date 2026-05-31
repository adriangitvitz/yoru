# Hello, Yoru

Create a file called `hello.yr`:

```yoru
fn main() {
  println("hello, yoru")
}
```

Run it:

```sh
yoru run hello.yr
```

You should see:

```
hello, yoru
```

## What just happened?

`yoru run` does four things, in order:

1. **Lex** the source into tokens.
2. **Parse** the tokens into an AST.
3. **Type-check** the AST, including effect inference and capability
   checking.
4. **Evaluate** the program. If the file defines `fn main()`, it is called
   automatically after any top-level statements run.

If any of the first three steps fails, you get an error and nothing runs.
This is the value proposition: a misspelled effect, a missing tool
capability, or a type mismatch is caught before the program touches your
database.

## A program with a little more in it

```yoru
fn greet(name: String) -> String {
  "hello, " + name
}

fn main() {
  let names = ["yoru", "claude", "world"]
  for n in names {
    println(greet(n))
  }
}
```

Run it the same way. Notice that:

- Functions are declared with `fn`.
- Bindings are immutable by default (`let`); use `mut` for variables you
  intend to reassign.
- The last expression of a block is the value of that block, so `greet`
  needs no `return`.
- Lists use square brackets; iteration uses `for ... in ...`.

That's enough to read the rest of the book. The next chapter walks the full
syntax tour. Before that, take five minutes to learn the [CLI](./cli.md) -
you'll use `yoru check` and `yoru fmt` constantly.
