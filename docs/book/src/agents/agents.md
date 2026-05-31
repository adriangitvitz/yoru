# Agents

An **agent** is an actor backed by an LLM reasoning loop. You give it a
system prompt and a set of tools; the runtime drives the conversation
with the model, dispatches tool calls, feeds results back, and stops
when the model produces a final text response or hits the configured
turn/token budget.

## Declaring an agent

```yoru
agent SupportAgent {
  model: "anthropic/claude-sonnet-4.5"
  system: "You are a customer support agent. Use tools to answer."
  tools: [LookupCustomer, SearchOrders, RefundOrder]
  config {
    max_turns: 10,
    budget_tokens: 4096,
  }
}
```

| Field          | Purpose                                                       |
|----------------|---------------------------------------------------------------|
| `model`        | Model name. For OpenRouter use `provider/model` form.         |
| `system`       | System prompt. Define persona, rules, output format.          |
| `tools`        | List of tool types the agent may call.                        |
| `config`       | `max_turns`, `budget_tokens`, `temperature`.                  |

## Spawning and chatting

```yoru
let assistant = spawn SupportAgent()
let reply = assistant.chat("Look up orders for a@example.com")
match reply {
  Err(e)   => println("agent failed: " + e.kind)
  Ok(text) => println(text)
}
```

`spawn AgentName()` always returns an actor reference, even when no LLM
client is installed. If the runtime has no LLM client (e.g. neither
`OPENROUTER_API_KEY` nor `ANTHROPIC_API_KEY` is set), the returned ref
is a **dead-config ref**: calls to `.chat()` on it immediately return
`Result.Err{kind: "llm_not_configured"}`. This lets you write the same
spawn-and-chat code in any environment; only the `.chat` call site
needs to match on the error.

`.chat(prompt)` returns a `Result[String, Error]` - `Result.Ok(text)`
on success, `Result.Err{kind: ...}` on any failure (timeout, missing
LLM client, model error, max-turns/budget exhaustion). Same symmetric
shape as `.ask` on an actor; same three consumption idioms:

```yoru
// 1. `?` propagates Err up
fn ask_briefly(agent, q: String) -> String {
  agent.chat(q)?
}

// 2. `??` supplies a fallback
let reply = agent.chat(prompt) ?? "(agent unavailable)"

// 3. Match on both arms when you need distinct handling
match agent.chat(prompt) {
  Ok(text) => store(text)
  Err(e)   => log_error(e.kind)
}
```

The blocking wait is bounded by `ChatTimeout` (120 seconds by default).

## What happens during `.chat(...)`

1. The runtime sends the system prompt, the tool catalogue (JSON Schema
   for each declared tool), and the user prompt to the model.
2. The model responds with either:
   - **A tool call** - the runtime executes the tool, feeds the result
     back, and loops to step 2.
   - **A final text message** - the runtime returns it.
3. If `max_turns` or `budget_tokens` is exceeded, the runtime returns
   `Result.Err{kind: "agent_error"}` with the reason.

## Failure modes

| Error `kind`           | Cause                                                  |
|------------------------|--------------------------------------------------------|
| `llm_not_configured`   | No `OPENROUTER_API_KEY` or `ANTHROPIC_API_KEY` set.    |
| `chat_timeout`         | The loop did not finish in `ChatTimeout`.              |
| `agent_error`          | The loop hit a model error or exhausted the budget.   |
| `agent_output_invalid` | `output { ... }` schema failed validation after all retries. |
| `capability_denied`    | A tool call needed a capability not in scope.          |
| `http_request_failed`  | A tool issued an HTTP call that failed.                |

Wrap `.chat` calls in `match` and decide per-`kind` whether to retry,
fall back, or surface the error.

## Structured output (`output { ... }`)

Agents can declare an `output { ... }` block - the same field syntax tools
use - to demand a structured JSON response instead of free-form text. The
runtime auto-injects a schema instruction into the system prompt, validates
the model's reply on each turn, and retries with a corrective message when
the reply does not parse or misses a required field.

```yoru
agent Classifier {
  model:  "anthropic/claude-sonnet-4.5"
  system: "Classify the user's message."
  output {
    category:   String  @doc("billing/support/sales/spam")
    confidence: Float   @doc("0.0–1.0")
  }
  config {
    retry_invalid_output: 2,  // initial attempt + 2 retries (default)
  }
}

let c = spawn Classifier()
match c.chat("My invoice is wrong, please refund me") {
  Err(e)   => log("classifier failed: " + e.kind)
  Ok(r)    => {
    // r is a typed `Classifier.Output` value - access fields directly.
    log(r.category + " @ " + to_string(r.confidence))
  }
}
```

When the schema is satisfied, `.chat` resolves to `Result.Ok(<AgentName>.Output)` -
the JSON payload is re-tagged with the agent's name so handlers can pattern-match
on the type just like any other object. When retries exhaust, `.chat` returns
`Result.Err{kind: "agent_output_invalid", message: "..."}` carrying the last
validator complaint, distinct from generic `agent_error`. This is the building
block for safe agent-to-agent handoffs: Agent B can declare the contract Agent A
must satisfy, and the runtime enforces it before the handoff happens.

Field types follow the same mapping the tool input/output blocks use
(`String`, `Int`, `Float`, `Bool`, `[T]`, `Object`). `@doc("...")` annotations
flow through to the generated schema so the model sees what each field means.

## Chaining agents across vendors

Because `.chat` returns `Result.Ok(<AgentName>.Output)` for any agent with
an `output { ... }` block, agent-to-agent handoffs are just nested `match`
expressions - and the runtime guarantees a downstream agent never sees
malformed input from an upstream one.

```yoru
agent Researcher {                          // Anthropic
  model:  "anthropic/claude-3.5-haiku"
  system: "You scope research questions. Respond ONLY with valid JSON."
  output {
    topic:    String  @doc("crisp topic name")
    audience: String  @doc("who the brief is for")
    angle:    String  @doc("specific angle")
  }
}

agent Outliner {                            // Google
  model:  "google/gemini-2.0-flash-001"
  system: "Turn a research plan into a 3-section outline. JSON only."
  output {
    title:  String
    thesis: String
    s1:     String
    s2:     String
    s3:     String
  }
}

agent Writer {                              // Meta
  model:  "meta-llama/llama-3.3-70b-instruct"
  system: "Render an outline as a 3-paragraph brief. Plain prose."
}

let researcher = spawn Researcher()
let outliner   = spawn Outliner()
let writer     = spawn Writer()

match researcher.chat(user_request) {
  Err(e)   => log("research stage failed: " + e.kind)
  Ok(plan) => match outliner.chat("Plan: " + plan.topic + " / " + plan.angle) {
    Err(e)      => log("outline stage failed: " + e.kind)
    Ok(outline) => match writer.chat(outline.title + " - " + outline.thesis) {
      Err(e)     => log("write stage failed: " + e.kind)
      Ok(brief)  => println(brief)
    }
  }
}
```

Two properties matter here:

**Cross-vendor contracts.** Three different model families (Anthropic /
Google / Meta) participate, and none of them know about each other. The
contract between them is the `output { ... }` block on each upstream
agent - the runtime translates that into a per-provider system-prompt
augmentation, validates the reply, and re-tags the JSON as
`<AgentName>.Output` before the downstream stage can read it.

**Fail-fast on contract violations.** If any agent with an `output`
block fails validation after its retries are exhausted, that stage
returns `Result.Err{kind: "agent_output_invalid"}` and the `match`
short-circuits - downstream stages never run. A weak middle link
cannot poison the rest of the chain:

```
=== Agent A (Claude) - Researcher ===
  topic: Open Source AI Project Sustainability
  ...
=== Agent B (Llama 3.2 1B) - Outliner ===
  Chain halted at B with kind: agent_output_invalid
  ✓ runtime caught malformed JSON before Agent C ever ran
```

## Multi-turn conversations

`.chat()` is stateless from the agent's point of view - each call starts
a fresh reasoning loop. To maintain conversation state across turns,
either:

1. Concatenate prior turns into the prompt manually, or
2. Wrap the agent in a parent actor that owns a transcript and forwards
   `chat` calls.

The parent-actor pattern keeps the conversation log isolated from the
agent's reasoning loop and survives across crashes when paired with a
supervisor.

## When to use an agent

- The task requires **decision-making across multiple tool calls** that
  you cannot enumerate upfront.
- The user's input is **natural language** and you want the model to
  interpret intent.

If the workflow is a deterministic sequence (validate → enrich →
persist), use a pipeline. If it's a single tool invocation, just call
the tool. Agents are for *autonomous reasoning over tool sets*.
