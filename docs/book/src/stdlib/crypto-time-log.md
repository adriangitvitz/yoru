# Crypto, Time, Log

## `Crypto`

All Crypto operations work on **`String`s** (not `Bytes`) and return
hex- or base64-encoded `String`s. The actual entries are:

| Function | Signature | Returns |
|----------|-----------|---------|
| `Crypto.sha256(s)`              | `String -> String` | Lowercase hex digest |
| `Crypto.hmac_sha256(key, msg)`  | `(String, String) -> String` | Lowercase hex HMAC |
| `Crypto.random_hex(n)`          | `Int -> String` | `n` random bytes, hex-encoded (output length `2n`) |
| `Crypto.base64url_encode(s)`    | `String -> String` | URL-safe base64, no padding |
| `Crypto.base64url_decode(s)`    | `String -> String` | Decoded string |
| `Crypto.constant_time_eq(a, b)` | `(String, String) -> Bool` | Timing-safe comparison |

```yoru
let token = Crypto.random_hex(16)               // 32-char hex string
let mac   = Crypto.hmac_sha256(secret, payload) // lowercase hex
let same  = Crypto.constant_time_eq(mac, expected)
```

### What is **not** in the Crypto namespace today

| Documented elsewhere | Reality |
|----------------------|---------|
| `Crypto.random(n) -> Bytes` | No such function. Use `Crypto.random_hex(n)` and decode if you need raw bytes. |
| `Crypto.base64_encode/decode` (standard base64) | Only the **URL-safe** variant is implemented (`base64url_*`). |
| `Crypto.hex_encode/decode` | No such functions. Hex is produced by `sha256` / `hmac_sha256` / `random_hex` directly. For ad-hoc hex, use `Bytes.from_hex(s)` and `b.to_hex()` on the `Bytes` value. |

## `Time`

| Function | Purpose |
|----------|---------|
| `Time.now_unix()`        | Seconds since epoch (`Int`).                 |
| `Time.now_ms()`          | Milliseconds since epoch (`Int`).            |
| `Time.now_iso()`         | RFC 3339 timestamp as `String`.              |
| `Time.sleep(ms)`         | Suspend the current fibre for `ms`.          |
| `Time.add(ts, dur)`      | Add a duration to a timestamp.               |
| `Time.format(ts, layout)` | Format a timestamp.                         |
| `Time.parse(s, layout)`  | Parse a timestamp.                           |

For deterministic time in tests, install a custom `Time` provider — see
[Built-in effects](../effects/built-in-effects.md).

## `Log`

| Function | Purpose |
|----------|---------|
| `Log.debug(msg)`                  | Debug line. Hidden unless the provider's `LogLevel` is set to `LogDebug`. |
| `Log.info(msg)`                   | Info line. |
| `Log.warn(msg)`                   | Warning line. |
| `Log.error(msg)`                  | Error line. |
| `Log.info_fields(msg, obj)`       | Info line plus the string fields of `obj`. |
| `Log.error_fields(msg, obj)`      | Error line plus the string fields of `obj`. |
| `Log.with(obj)`                   | Attach persistent context fields (strings only) carried by every subsequent log line. |

```yoru
Log.with(LogCtx { request_id: req.id })
Log.info_fields("user_created", UserCtx { id: user.id, email: user.email })
```

The `obj` argument must be an object value. Both `TypeName { ... }` and
bare `{ ... }` literals work. Only string-valued fields are emitted —
non-string fields are silently dropped.

### Output format

Lines are **always JSON** — one object per line on stderr by default,
with `level`, `msg`, `ts`, and any context/one-shot fields merged in.
There is no `LOG_FORMAT` toggle.

### Level

The level threshold is set on the provider via `LogLevel` (`LogDebug`,
`LogInfo`, `LogWarn`, `LogError`), defaulting to `LogInfo` when the
provider is installed by `stdlib.InstallAll`. There is no `LOG_DEBUG`
environment variable; raise the level in Go by setting the field on
the provider before installation.
