# Actors

An **actor** is an object with private state and a message inbox. It
runs on its own goroutine. The only way to interact with an actor from
outside is to send it a message.

## Declaring an actor

```yoru
actor Counter {
  state count: Int = 0

  receive Increment {
    self.count += 1
  }

  receive Add(n: Int) {
    self.count += n
  }

  receive GetCount -> Int {
    self.count
  }
}
```

- `state` declares a mutable field. State is only mutable inside `receive`
  blocks.
- Each `receive` declares one message type and the body that runs when
  that message arrives.
- A `receive` may declare a reply type with `-> T`. Callers retrieve it
  with `.ask(...)`.

## Spawning and messaging

`spawn` launches the actor and returns an actor reference (a `tag`):

```yoru
let c = spawn Counter()

c <- Increment             // fire-and-forget
c <- Add(n: 5)

let n = c.ask(GetCount)?   // blocking ask + propagate timeout via ?
println(to_string(n))      // 6
```

| Op | Direction | Blocking? | Return |
|----|-----------|-----------|--------|
| `<-` | Sender → actor | No | nothing |
| `.ask(M)` | Caller → actor → reply | Yes (5s default) | `Result[T, Error]`. On success: `Ok(reply)`. On timeout: `Err{kind: "ask_timeout"}`. |

`.ask` returns a symmetric `Result`. Three idiomatic ways to consume it:

```yoru
// 1. `?` propagates the timeout up - clean inside any function returning Result.
fn current(c) -> Int {
  let n = c.ask(GetCount)?
  n
}

// 2. `??` supplies a fallback at the call site.
let n = c.ask(GetCount) ?? 0

// 3. Match on both arms when you want different handling per case.
match c.ask(GetCount) {
  Ok(n)  => println("got " + to_string(n))
  Err(e) => println("timed out: " + e.kind)
}
```

If the actor's `receive` body itself returns a `Result`, `.ask` passes
it through without double-wrapping - so `actor.ask(M)` is always the
same `Result` shape callers expect.

## The Isolated Turn Principle

An actor processes **exactly one message at a time, to completion**.
There is no inter-message preemption. This is what makes `state` safe to
mutate inside `receive` without locks: the body holds the only thread of
control over the actor's state until it returns.

Within a `receive` body you can:

- Mutate `self.state`.
- Send messages to other actors (they queue up; processed in order).
- Spawn child actors.
- Call any pure function.

## When to use an actor

- You need **mutable shared state** that more than one part of the
  program touches.
- You need a **long-running background worker** (queue consumer, timer,
  cache warmer).
- You need to model **independent collaborating entities** (one agent
  per user session, one worker per shard, one pipeline stage per
  partition).

If you just need to compute a value once, a function is enough. Reach
for an actor only when the lifetime is genuinely longer than a single
call.

## Next

[Spawn, send, ask](./spawn-send-ask.md) covers the messaging primitives
in detail; [Supervision](./supervision.md) shows how to recover from
crashes.
