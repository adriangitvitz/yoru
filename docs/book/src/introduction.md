# Introduction

**Yoru** (夜, "night") is a statically typed programming language for backend
services, ETL pipelines, and LLM agent orchestration. It treats tools,
agents, MCP servers, supervised actors, and HTTP services as first-class
language constructs rather than libraries.

This book is a hands-on tour for someone who has never written a line of
Yoru. It assumes you can read code in any C-family language (Go, Rust, Swift,
TypeScript, Python) — no prior experience with effect systems, actor models,
or capabilities is required.

## What you will learn

By the end of this book you will be able to:

- Install Yoru and run a `.yr` file.
- Read and write the core syntax: bindings, functions, objects, enums,
  pattern matching, errors with `Result`.
- Understand the **effect system** and write functions whose I/O is tracked
  in the type signature.
- Spawn **actors** and supervise crashing children.
- Build **pipelines** that fan out across worker goroutines.
- Declare **tools**, wire them into an **agent**, and expose them through an
  **MCP server** that Claude Desktop can call.
- Stand up an HTTP **service** with middleware, JWT auth, and rate
  limiting — all in a single `.yr` file.

## How this book is organized

The book moves from concrete (installing the binary, writing your first
file) toward conceptual (the effect system, supervised concurrency,
capability scoping). Skim the first three chapters, then jump to whichever
later chapter answers the problem in front of you. Most chapters are under
five minutes of reading; each ends with a runnable example.

## A note on phase

Yoru is in **Phase 0** (`yoru version` prints `yoru 0.1.0 (Phase 0)`).
The tree-walking interpreter is fast enough for production agents, MCP
servers, and small services, but the bytecode VM and LLVM backend are
not built yet. Everything in this book runs against `yoru run`. The
language surface is stable: nothing in this book is expected to be
removed.

## Where to go next

- [Install Yoru](./getting-started/installation.md) — five minutes.
- [Hello, Yoru](./getting-started/hello-yoru.md) — your first program.
- The full design rationale lives in `docs/SPECS.md`. The compact reference
  is the source of practical usage; `docs/USAGE.md` is a pointer back
  here from anyone who used the pre-book reference.
