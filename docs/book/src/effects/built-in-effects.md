# Built-in effects

These effect names are reserved and recognised by the type-checker
(`typechecker/checker.go`'s `defaultKnownEffects`). Provider
installation is separate - some are wired up automatically by
`stdlib.InstallAll`, others are allow-listed as effect labels you may
declare but have no runtime provider yet.

| Effect       | Provider installed by `stdlib.InstallAll`? | Operations / how surfaced                                                                 |
|--------------|---------------------------------------------|--------------------------------------------------------------------------------------------|
| `IO`         | No (built-in toplevel functions)            | `println`, `print`, `env`, `assert`. Annotate as `[IO]` when documenting side-effecting fns. |
| `Log`        | Yes                                          | `Log.debug`, `Log.info`, `Log.warn`, `Log.error`, `Log.info_fields`, `Log.error_fields`, `Log.with` |
| `JSON`       | Yes                                          | `JSON.encode`, `JSON.decode`, `JSON.get`, `JSON.pretty`, `JSON.merge`                       |
| `HTTP`       | Yes                                          | `HTTP.get`, `HTTP.post`                                                                     |
| `DB`         | Yes (in-memory driver by default)            | `DB.query`, `DB.query_one`, `DB.exec`, `DB.transaction`                                     |
| `Crypto`     | Yes                                          | `Crypto.sha256`, `Crypto.hmac_sha256`, `Crypto.random_hex`, `Crypto.base64url_encode/decode`, `Crypto.constant_time_eq` |
| `Time`       | Yes                                          | `Time.now_unix`, `Time.now_ms`, `Time.now_iso`, `Time.sleep`, `Time.add`, `Time.format`, `Time.parse` |
| `Subprocess` | Yes                                          | Subprocess invocation; see `stdlib/subprocess.go`.                                          |
| `LLM`        | No (surfaced via `agent` declarations)       | An `agent`'s `.chat(...)` triggers the LLM call. No direct `LLM.complete(...)` builtin exists. |
| `Agent`, `Spawn`, `Stream`, `Metric`, `Clock` | No                          | Allow-listed effect labels. Use them to annotate functions even though no provider ships today. |

Opt-in providers - auto-installed when their env var is present:

| Effect   | Env var          | Operations                                                                                                |
|----------|------------------|-----------------------------------------------------------------------------------------------------------|
| `Redis`  | `REDIS_URL`      | `lpush`, `rpush`, `lpop`, `brpop`, `llen`, `get`, `set`, `del`, `publish`, `xadd`, `xread_next`, `ping`   |
| `Rabbit` | `RABBITMQ_URL`   | `publish`, `consume` (returns `Delivery{body, ack_id}`), `ack`, `queue_size`, `ping`                      |
| `SQS`    | `AWS_REGION` + creds (`SQS_ENDPOINT_URL` optional) | `create_queue`, `send_message`, `receive_message`, `delete_message`, `queue_size`, `ping` |
| `Kafka`  | `KAFKA_BROKERS`  | `write_message`, `read_message`, `commit`, `create_topic`, `ping`                                         |

The optional bus providers all return `Delivery{body, ack_id}` on read,
so manual ACK gives you at-least-once delivery without leaking the
underlying client semantics into application code.

## Declaring vs. handling

You **declare** an effect on a function by adding it to the function's
`effect [...]` list. The compiler refuses to compile a call into a
context that does not cover all declared effects.

You **handle** an effect by either:

1. Calling the function from inside a `handle(EffectName) { ... } in
   ...` block (substitutes a provider scoped to the body), or
2. Propagating the effect up to a context that has a provider (for the
   stdlib-installed effects, that is the top-level `yoru run`).

## Custom providers from Go

If you need a custom backing implementation (a mock for tests, a
proprietary provider in production), construct the interpreter
manually:

```go
interp := interpreter.NewInterpreter()
interp.InstallProvider(&MyCustomProvider{})
stdlib.InstallAll(interp, os.Stderr)
interp.EvalSourceInto(source)
```

Providers are scoped per-interpreter - concurrent HTTP request handlers
each get isolated state.
