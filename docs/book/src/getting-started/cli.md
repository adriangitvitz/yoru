# The `yoru` CLI

Everything you do during development goes through one binary.

| Command | What it does |
|---------|--------------|
| `yoru run <file.yr>` | Lex, parse, type-check, evaluate. Auto-starts `service` declarations and auto-calls `fn main()`. |
| `yoru check <file.yr>` | Lex, parse, type-check only. No evaluation. Fast feedback during editing. |
| `yoru fmt <file.yr>` | Format a source file in place. |
| `yoru repl` | Interactive read-eval-print loop. Useful for poking at expressions. |
| `yoru build --target mcp --output <path> <file.yr>` | Produce a standalone MCP server binary speaking JSON-RPC 2.0 on stdio. |
| `yoru build --target http --output <path> <file.yr>` | Produce a standalone HTTP service binary. |
| `yoru version` | Print the version. |

> The build subcommand takes `--target <mcp|http>` and `--output <path>`.
> The short `-o` flag is **not** recognised — using it silently writes
> the binary to the default location (`./<basename>`) in the current
> working directory. Always use `--output`.

## Patterns you will repeat

### Tight inner loop while writing code

```sh
yoru check src/main.yr   # fails fast on type/effect errors
yoru run   src/main.yr   # runs it
```

### Format on save

If your editor doesn't already run `yoru fmt` on save, do it manually:

```sh
yoru fmt src/main.yr
```

### Build an MCP server to plug into Claude Desktop

```sh
yoru build --target mcp --output ./bin/research-mcp src/research_mcp.yr
```

Then point `claude_desktop_config.json` at the resulting binary:

```json
{
  "mcpServers": {
    "research": {
      "command": "/absolute/path/to/research-mcp"
    }
  }
}
```

See [MCP servers](../agents/mcp-servers.md) for the full walkthrough.

### Build a standalone HTTP service

```sh
yoru build --target http --output ./bin/api src/api.yr
./bin/api      # listens on the port declared in the service block
```

The build target reads a **single file**. Multi-file projects (where
handlers live in separate `.yr` files imported by the entry point) must
be flattened into one source before building, or run via `yoru run`
from the project root.

## Errors are part of the API

Every error message Yoru's compiler produces is covered by a test that
asserts its exact string. If you script around `yoru check` output, the
strings are stable across patch releases. See `lexer_test.go`,
`parser_test.go`, and `typechecker/effects_test.go` for the catalogue.
