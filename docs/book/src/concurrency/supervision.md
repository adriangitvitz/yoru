# Supervision

A long-running actor will eventually crash - a tool call will time out, a
parser will choke, an LLM will hallucinate a malformed tool argument. In
the actor model, crashing is **a normal event**, not an emergency. A
supervisor decides what to do next.

Two builtins, both backed by the same supervisor runtime:

| Builtin | Use when |
|---------|----------|
| `Supervisor.new(...)` | You want **explicit** start/stop and to supervise a mix of **actors and agents**. |
| `supervise_agents(...)` | Short, agent-only convenience that **auto-starts** and returns a name→ref map. |

## `Supervisor.new` - general-purpose

```yoru
actor Counter { state n: Int = 0; receive Inc { self.n += 1 } }
actor Logger  { state lines: Int = 0; receive Log { self.lines += 1 } }

fn main() {
  let sup = Supervisor.new(
    ["Counter", "Logger"],   // child names - actor or agent declarations
    "one_for_one",           // strategy
    5,                        // max_restarts per window
    30                        // window in seconds
  )
  sup.start()                 // explicit lifecycle - children spawn here

  let kids = sup.children()   // Map<name, ActorRef>
  let c = kids.get("Counter")
  c <- Inc
  c <- Inc

  // ... later
  sup.stop()                  // shuts down every child
}
```

**Important properties:**

- `Supervisor.new` does **not** auto-start. `children()` returns an empty
  map until `start()` runs.
- Child names can be **either actors or agents** - the lookup tries
  `actor` declarations first, then `agent`. Unknown names fail closed
  with `Result.Err{kind: "supervisor_bad_args"}`.
- `.start()` returns `Result.Ok(nil)` on success.
- `.stop()` closes every child's mailbox. Subsequent `.ask` on a
  stopped child returns `Result.Err{kind: "actor_stopped"}` instead of
  panicking.
- `.children()` returns a `Map<name, ActorRef>` containing live children
  only (empty before `start()`).
- `.add_child("Name")` dynamically registers and spawns a new child of
  the named declaration; the supervisor's restart policy applies.

## `supervise_agents` - agent-only convenience

```yoru
agent HistoryAgent     { model: "anthropic/claude-sonnet-4.5" system: "..." tools: [WikiSummary] }
agent FinanceAgent     { model: "anthropic/claude-sonnet-4.5" system: "..." tools: [FxQuote] }
agent SynthesizerAgent { model: "anthropic/claude-sonnet-4.5" system: "..." tools: [] }

fn main() {
  let team = supervise_agents(
    ["HistoryAgent", "FinanceAgent", "SynthesizerAgent"],
    "one_for_one",
    3,
    60
  )

  let h = team.get("HistoryAgent").chat("Brief me on Kyoto.")
  let f = team.get("FinanceAgent").chat("USD to JPY?")
  let s = team.get("SynthesizerAgent").chat("Combine:\n" + to_string(h) + "\n" + to_string(f))
  println(to_string(s))
}
```

`supervise_agents` is `Supervisor.new(...)` + `sup.start()` +
`sup.children()` in one call. It works only with agent declarations,
and the returned map is what you use directly. Reach for it when you
just need a team of agents up and running with no further lifecycle
management.

## Strategies

| Strategy        | On a child crash, the supervisor restarts...           |
|-----------------|--------------------------------------------------------|
| `"one_for_one"`   | Just the crashed child.                              |
| `"one_for_all"`   | Every child.                                         |
| `"rest_for_one"`  | The crashed child and every child declared after it. |

If a single child crashes more than `max_restarts` times within
`window_seconds`, the supervisor shuts the entire team down. This
prevents thrash loops.

## Stopped-actor semantics

Calling `.ask` on an actor whose supervisor has been stopped returns
`Result.Err{kind: "actor_stopped"}`. This is distinct from
`ask_timeout` (the actor is alive but slow) and lets handlers
differentiate "the team went down" from "this one query is taking too
long."
