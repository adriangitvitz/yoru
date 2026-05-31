# Keyword reference

## Reserved keywords

These produce dedicated token types in the lexer. They cannot be used as
identifiers.

### Structural

`object`, `blueprint`, `actor`, `agent`, `tool`, `mcp`, `service`,
`pipeline`, `protocol`, `impl`, `effect`, `handle`, `flow`.

### Functions and bindings

`fn`, `let`, `mut`, `type`, `enum`, `union`.

### Concurrency

`spawn`, `receive`, `send`, `emit`, `yield`.

### Control flow

`match`, `if`, `else`, `for`, `in`, `while`, `do`, `return`, `break`,
`continue`.

### Modules

`import`, `export`, `use`, `where`, `with`.

### Literals

`true`, `false`, `nil`.

### Self / super

`self`, `super`.

### Reference capabilities

`iso`, `trn`, `ref`, `val`, `box`, `tag`.

### Pipeline

`stream`, `partition`, `merge`, `window`, `sink`, `source`, `transform`.

### Composition

`delegate`.

### Reserved for future use

`async`, `await`.

## Contextual identifiers

These are **not** keywords. The lexer produces `IDENT`. The parser
assigns them meaning only in specific positions, which leaves you free
to use them as variable names elsewhere.

| Identifier  | Special position |
|-------------|------------------|
| `state`     | Inside an `actor` body, declares a field. |
| `using`     | Inside a `handle(...) { ... }` block.     |
| `on_error`  | Inside a pipeline fault policy.           |
| `checkpoint` | Inside a pipeline checkpoint policy.     |
| `strategy`  | Field name inside a (future) `Supervisor.new(...)` call. The exposed supervision API today is the `supervise_agents(...)` builtin - there is no `Supervisor.new` constructor yet. |
| `description`, `input`, `output`, `model`, `system`, `tools`, `config`, `capability` | Inside tool/agent/mcp declarations. |
| `transport`, `auth`, `prefix`, `middleware` | Inside service/mcp declarations. |

> `partition` is **not** contextual - it is a reserved keyword (token
> `PARTITION` in `lexer/token.go`). It can only appear in the syntactic
> positions the parser recognises for it (currently the pipeline
> stage), but it can never be used as an identifier.
