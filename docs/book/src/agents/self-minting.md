# Self-minting agents

A Yoru agent can **extend its own toolkit at runtime**: it emits a
Yoru `tool { ... }` declaration as a string through a meta-tool, the
runtime parses and registers it, and the new tool becomes callable on
the very next turn - or even within the same response if the model
batches `tool_use` blocks.

The mechanism is small: one new builtin (`define_tool`) and one new
hook on the agent loop (`RefreshTools`).

## The pattern

Declare a meta-tool that wraps `define_tool`, and include it in your
agent's tools list:

```yoru
tool DefineTool {
  description: "Add a new Yoru tool to your toolkit. Provide a `tool { ... }` declaration as a String. Returns the names registered."
  input {
    source: String @doc("A Yoru `tool { ... }` declaration."),
  }
  output: [String]
  fn run(self, i: DefineTool.Input) -> [String] {
    define_tool(i.source)
  }
}

agent CuriousAgent {
  model: "anthropic/claude-sonnet-4.5"
  system: "When you need a tool you don't have, mint it via DefineTool, then call it."
  tools: [DefineTool]
  config {
    max_turns: 10,
    budget_tokens: 4096,
  }
}
```

The agent loop calls a refresh hook before every LLM request and
between each successful `tool_use` block inside one response - so when
the model batches `DefineTool(...)` and the new tool's call in the
same turn, the second block sees the freshly-registered tool.

## `define_tool(source)`

Lexes and parses the source string. Any `tool { ... }`, `object { ... }`,
or `enum { ... }` declarations land in the live interpreter's decl
tables. Returns the list of tool names registered.

```yoru
let names = define_tool("tool Echo { description: \"echo\" input { msg: String } output: String fn run(self, i: Echo.Input) -> String { i.msg } }")
// names = ["Echo"]
Echo.run(msg: "hello")   // "hello"
```

Parse failures return `Result.Err{kind: "define_tool_parse_failed"}`
with the lexer/parser error messages joined.

## What the system prompt has to do

The model needs to know two things the JSON schema can't tell it:

1. **Yoru tool syntax.** Models trained mostly on Python and TypeScript
   need a concrete template in the system prompt or the first attempts
   will use the wrong shape (`Str` instead of `String`, `fn run()`
   instead of `fn run(self, i: ToolName.Input)`, `output { ... }` where
   `output: T` was meant).

2. **When to mint vs. inline.** Without a "mint when you'll call it
   ≥ 2 times" heuristic the model mints a fresh tool for every task,
   which wastes tokens on every turn.

A working system prompt for `DefineTool` looks like:

```
You can extend your toolkit at runtime by calling DefineTool.

Yoru tool template - copy this shape exactly:

  tool ToolName {
    description: "one sentence"
    input { field: String, }
    output: String
    fn run(self, i: ToolName.Input) -> String {
      // body references i.field (NOT bare field)
      FS.read(i.field)
    }
  }

Rules:
- Types are String, Int, Float, Bool, [T], Option[T] - never Str.
- `fn run` ALWAYS takes (self, i: <ToolName>.Input).
- `output:` is one type expression, not a block.
- Providers available inside fn run: FS, Path, Crypto, HTTP, Fuzzy,
  Diff. No `effect [...]` annotation needed.

Mint a tool when you'll call it more than once. Otherwise inline it.
```

The
[`examples/showcase/self_minting_agent.yr`](https://github.com/adriangitvitz/yoru/blob/master/examples/showcase/self_minting_agent.yr)
file in the repo uses exactly this shape.

## Tracing what the agent does

Set `YORU_AGENT_DEBUG=1` and the runtime logs one line per turn -
per-turn and cumulative input/output tokens - and one line per tool
call with the input prefix and result prefix. Indispensable when
wiring an agent for the first time.

```
[agent] turn=1 in=1028 out=154 cumulative_in=1028 cumulative_out=154
[agent] tool=DefineTool input={"source":"\ntool ReadFile {..."}
[agent] result(isError=false)=[ReadFile]
[agent] turn=2 in=1254 out=81 cumulative_in=2282 cumulative_out=235
[agent] tool=ReadFile input={"path":"/tmp/x/secret.txt"}
[agent] result(isError=false)=The secret word is: pumpkin.
[agent] turn=3 in=1377 out=17 cumulative_in=3659 cumulative_out=252
```

## Safety implications

A minted tool's `fn run` body runs through the same path as a
statically declared one. That means:

- **Capabilities still apply.** If the user wires the script with
  `with_capability("destructive", fn() => agent.chat(...))`, the agent
  can mint tools that declare `capability: .destructive`, but the
  enforcement check at call time still requires the capability to be
  on the runtime stack. The model cannot grant itself a capability -
  the host code decides.
- **Effects still apply** at runtime (although the type checker doesn't
  re-run on dynamically registered tools, the body is evaluated through
  the same eval path, so a `FS.write` call still resolves through the
  installed provider).
- **No new attack surface.** `define_tool` only parses Yoru. It does
  not exec arbitrary code, spawn subprocesses, or mutate any state
  outside the interpreter's declaration tables.

The structural property worth repeating: **the model can declare
requirements but cannot grant them.** If you keep that invariant in
your host code, self-minting is safer than "let the model write
Python and `exec` it" by construction.

## The minimum demo

[`examples/showcase/self_minting_agent.yr`](https://github.com/adriangitvitz/yoru/blob/master/examples/showcase/self_minting_agent.yr)
is a single-file proof. The agent starts with only `DefineTool`,
receives the task "read this file and tell me the secret word", mints
a `ReadFile` tool, calls it, and returns the word. 3 turns total. Set
`OPENROUTER_API_KEY` and `yoru run` it.

There's also an A/B benchmark against the equivalent Python harness in
[`bench/self_mint_ab/`](https://github.com/adriangitvitz/yoru/blob/master/bench/self_mint_ab/)
- the structural finding is that Yoru's output tokens consistently
land at ~60% of Python's because the JSON Schema is derived from the
declaration instead of being written by hand on every mint.
