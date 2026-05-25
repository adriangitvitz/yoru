# Operator reference

## Arithmetic

| Operator | Meaning |
|----------|---------|
| `+`  | Addition. Also `String` concatenation. |
| `-`  | Subtraction. Unary minus.              |
| `*`  | Multiplication.                        |
| `/`  | Division.                              |
| `%`  | Modulo.                                |

## Comparison

`==`, `!=`, `<`, `>`, `<=`, `>=`.

## Logical

| Operator | Meaning |
|----------|---------|
| `&&` | And (short-circuits). |
| `||` | Or (short-circuits).  |
| `!`  | Not.                  |

## Assignment

| Operator | Meaning |
|----------|---------|
| `=`   | Assign (only inside `mut` bindings or `actor` `receive` bodies). |
| `+=`  | Add and assign.                                                  |
| `-=`  | Subtract and assign.                                             |

## Yoru-specific

| Operator | Meaning |
|----------|---------|
| `->`  | Function return type, route handler arrow.                                |
| `=>`  | Match-arm body. Single-expression lambda body.                            |
| `\|>` | Pipe-forward in a `pipeline` stage. Composes left → right.                |
| `<-`  | Send a message to an actor (fire-and-forget).                              |
| `?`   | Postfix. Unwrap `Ok` or early-return `Err`.                                |
| `??`  | Infix. Fallback when the left is `Err`, `None`, or `nil`.                  |
| `...` | Spread/rest in destructuring and argument lists.                           |
| `@`   | Annotation prefix (`@doc("...")` on tool input fields).                    |

## Literals

| Form | Meaning |
|------|---------|
| `60s`, `500ms`, `5m`, `1.5h` | Duration literals. Desugar to `Int` milliseconds at parse time. Units: `ns`, `us`, `ms`, `s`, `m`, `h`. |
| `.tag(args...)` | Leading-dot enum shorthand (used in pipeline `on_error:`, `back_pressure:`, `mcp` `auth:` slots). Evaluates to a `Policy` ObjectVal with `tag`, `args`, `named` fields. |

## Delimiters

`(`, `)`, `{`, `}`, `[`, `]`, `,`, `:`, `;`, `.`.

## Comments

```yoru
// Line comment
/* Block comment */
```
