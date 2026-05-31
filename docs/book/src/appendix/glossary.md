# Glossary

**Actor.** An object that runs on its own goroutine with private state
and a message inbox. The Isolated Turn Principle guarantees one message
is processed at a time. See [Actors](../concurrency/actors.md).

**Agent.** An actor backed by an LLM reasoning loop. You configure a
model, system prompt, and a set of tools; `.chat(...)` runs the loop.

**Ask.** Synchronous message send to an actor that waits for a reply.
Bounded by `AskTimeout` (5 s default). See `.ask(...)`.

**Back-pressure.** A flow-control mechanism that lets a slow downstream
stage signal upstream stages to slow down. Declared as part of the
pipeline (forward-looking in Phase 1).

**Capability (reference).** A type-level annotation (`iso`, `trn`,
`ref`, `val`, `box`, `tag`) that constrains how a reference can be
shared between actors. Prevents data races at compile time.

**Capability (runtime).** A named permission that must be in scope at
call time. Used by `tool capability: .name` and granted by
`with_capability("name", fn() => ...)`. See
[Capability scoping](../agents/capability-scoping.md).

**Effect.** A label on a function signature describing a category of
side effect the function may produce (`HTTP`, `DB`, `LLM`, `Log`, ...).
The compiler enforces that callers either declare or handle each effect.

**Effect handler.** A `handle(EffectName) { using: provider } in body`
block that intercepts an effect and routes it to a provider for the
duration of the body.

**Enum.** A sum type. A value is one of several named variants, each
with its own fields. Exhaustively matched with `match`.

**Fibre.** A lightweight, runtime-scheduled thread of execution. Yoru
maps actors and request handlers to fibres on top of OS threads.

**Impl block.** Either a plain method block (`impl T { ... }`) or a
protocol implementation (`impl T : Proto { ... }`).

**Isolated Turn Principle.** Each actor processes exactly one message
at a time, to completion. State mutation inside a `receive` body is
race-free by construction.

**MCP.** Model Context Protocol - a JSON-RPC 2.0 protocol for exposing
tools and resources to LLM clients. `yoru build --target mcp` produces
a binary that speaks it.

**Object.** A named record type. Holds fields. Has no inheritance.

**Pipeline.** A typed chain of source → transforms → sink, composed
with `|>`. May fan out with `partition: N`.

**Protocol.** A behavioural contract - a set of method signatures
implementing types must satisfy. Effect-aware.

**Result.** `enum Result[T, E] { Ok(value: T) Err(error: E) }`. The
standard way functions surface recoverable failure.

**Service.** A `service` declaration produces an HTTP server. Routes
map verbs and paths to handler functions.

**Supervisor.** A parent process that watches child actors and applies a
restart strategy when they crash. Two APIs:
`Supervisor.new(names, strategy, max_restarts, window_seconds)` - general
purpose, supports both actor and agent children, explicit start/stop -
and `supervise_agents(...)` - the agent-only convenience that
auto-starts. See [Supervision](../concurrency/supervision.md).

**Tool.** A typed, named unit of capability with auto-generated JSON
Schema. Callable by agents and exposed over MCP. Tools are
**first-class values** in Yoru - a bare tool name resolves to a `Tool`
value you can pass as a function arg, store in a list or map, or
introspect via `.name()` / `.description()` / `.input_schema()` /
`.output_schema()`. The structured `output { ... }` block form
validates returns at runtime and emits an `outputSchema` to the LLM.
See [Tools](../agents/tools.md).

**With-capability.** `with_capability(name, fn() => ...)` - grants a
runtime capability for the duration of the lambda's invocation.
