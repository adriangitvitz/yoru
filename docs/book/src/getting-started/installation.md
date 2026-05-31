# Installation

Yoru is distributed as a single Go binary. You build it from source.

## Requirements

- **Go 1.22 or newer.** Check with `go version`.
- A POSIX-ish shell. macOS, Linux, and WSL all work.

## Build from source

```sh
git clone https://github.com/adriangitvitz/yoru
cd yoru
go build ./cmd/yoru
```

This produces `./yoru` in the repository root. Move it onto your `PATH`:

```sh
mv ./yoru /usr/local/bin/    # or ~/bin, ~/.local/bin, etc.
```

Verify the install:

```sh
yoru version
```

You should see something like `yoru 0.1.0 (Phase 0)`.

## Optional: environment variables

These are only needed if you want to run agents against a real LLM. Yoru
runs perfectly well without them - agents just return
`Result.Err{kind: "llm_not_configured"}` until you set one.

| Variable             | When to set it                                       |
|----------------------|------------------------------------------------------|
| `OPENROUTER_API_KEY` | Recommended. Multi-provider gateway. Takes priority. |
| `ANTHROPIC_API_KEY`  | Use Anthropic directly. Only consulted if the above is unset. |

Optional message-bus providers are auto-installed when you set the
corresponding env var; see [Optional providers](../stdlib/optional-providers.md).

## Next

Once `yoru version` works, head to [Hello, Yoru](./hello-yoru.md).
