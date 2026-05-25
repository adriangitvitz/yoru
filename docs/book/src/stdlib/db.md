# DB

The `DB` effect provides query, execute, and transaction operations.
The default runtime ships an in-memory driver — fine for prototyping
and tests. For production, plug in a Postgres / MySQL / SQLite provider
in Go before launching the interpreter.

## Operations

| Function | Purpose |
|----------|---------|
| `DB.query(sql, args)`     | Execute a `SELECT`. Returns a list of rows. |
| `DB.query_one(sql, args)` | Execute a `SELECT`, return the first row or `Err`. |
| `DB.exec(sql, args)`      | Execute an `INSERT` / `UPDATE` / `DELETE`. Returns affected row count. |
| `DB.transaction(fn)`      | Run `fn` inside a transaction. `fn` may be a Yoru closure or a Go builtin. |

## Examples

```yoru
let orders = DB.query("SELECT * FROM orders WHERE email = $1 LIMIT $2",
                      [email, to_string(limit)])

let one = DB.query_one("SELECT * FROM users WHERE id = $1", [id])

let n = DB.exec("UPDATE users SET active = false WHERE id = $1", [id])
```

## `DB.transaction(fn)` — transactional closures

`DB.transaction` accepts a **Yoru closure** and runs it inside a fresh
transaction. The return shape is always a `Result`:

- Closure returns a non-`Result` value → wrapped as `Result.Ok(v)` and
  the transaction commits.
- Closure returns `Result.Ok(v)` → passed through, transaction commits.
- Closure returns `Result.Err(...)` → the same `Err` propagates back and
  the transaction rolls back.
- Closure panics (Go-side runtime fault) → transaction rolls back and
  the caller sees `Result.Err{kind: "db_transaction_panic"}`.

```yoru
fn create_order(req: Request, body: CreateOrder) -> Response effect [DB, Crypto] {
  let result = DB.transaction(fn() => {
    let id = Crypto.random_hex(8)
    DB.exec("INSERT INTO orders (id, item) VALUES ($1, $2)", [id, body.item])
    DB.exec("INSERT INTO audit (action, ref) VALUES ($1, $2)", ["create_order", id])
    Result.Ok(id)
  })
  match result {
    Ok(id) => created({ id: id })
    Err(_) => internal_error("could not create order")
  }
}
```

Closures capture surrounding lexical scope, so `id`, `body`, and any
helper functions in the request handler are visible inside the
transaction body.

### Closures vs. plain BEGIN/COMMIT

`DB.transaction(fn() => ...)` is the canonical way to be atomic. The
old `DB.exec("BEGIN", []) ... DB.exec("COMMIT", [])` workaround still
works for drivers that support raw BEGIN/COMMIT, but you lose
rollback-on-error and rollback-on-panic for free with the closure form.

## Custom providers

```go
interp := interpreter.NewInterpreter()
interp.InstallProvider(stdlib.NewDBProvider(myDriver).WithInterp(interp))
stdlib.InstallAll(interp, os.Stderr)
interp.EvalSourceInto(source)
```

`WithInterp(interp)` is required for the Yoru-closure form of
`DB.transaction` to work — without it the provider rejects
`*FunctionVal` with a clear error message. `stdlib.InstallAll` wires
it automatically for the default in-memory driver.

The contract is in `stdlib/db.go`. A driver implements `Query`,
`QueryOne`, `Exec`, `Begin`. The default `NewMemoryDriver()` is what
`stdlib.InstallAll` wires up when no driver is supplied. Build your
own for any driver Go's `database/sql` supports.
