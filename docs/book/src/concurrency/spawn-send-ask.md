# Spawn, send, ask

## `spawn`

`spawn ActorName(args)` starts a new goroutine running the actor and
returns a reference (a `tag`) you can send messages to.

```yoru
actor Echo {
  receive Say(msg: String) -> String { msg }
}

let e = spawn Echo()
```

## `<-` (fire-and-forget send)

```yoru
e <- Say(msg: "hello")
```

Returns immediately. The message goes onto the actor's inbox and will be
processed in arrival order. There is no acknowledgement and no way to
retrieve a reply.

Use `<-` for everything where you don't need a response:

- Event notifications (`logger <- Log("user signed in")`).
- Background work submissions (`worker <- Process(record)`).
- Broadcasts.

## `.ask(MessageType)` (synchronous reply)

```yoru
match e.ask(Say(msg: "hello")) {
  Ok(text) => println(text)
  Err(err) => println("timed out: " + err.kind)
}

// or, inside a function returning Result, just propagate:
fn echo(e, msg: String) -> String {
  let reply = e.ask(Say(msg: msg))?
  reply
}
```

`.ask` blocks the caller until the actor produces a reply or
`AskTimeout` (5 seconds by default) fires. The result is **always** a
`Result` - `Ok(reply)` on success, `Err{kind: "ask_timeout"}` on
timeout. The symmetric shape lets `?` propagate, lets `??` provide
fallbacks, and keeps `.ask` consistent with the rest of the language's
fallible operations (`HTTP.get`, `DB.query_one`, `JSON.decode(..., T)`).

Use `.ask` when the caller genuinely cannot proceed without the answer.

## Picking between `<-` and `.ask`

| Question | Answer |
|----------|--------|
| Do you need the actor's response? | `.ask` |
| Will the actor mutate state and you don't care about a confirmation? | `<-` |
| Is the actor's work slow (>5s by default)? | Spawn an intermediate actor or raise `AskTimeout`. |
| Are you broadcasting? | `<-` to each child. |

## Backpressure

An actor's inbox is unbounded by default. If you are producing messages
faster than the actor can consume them, you will accumulate memory. For
production, prefer:

- A bounded pipeline (`partition: N` fans out the work).
- A pull-based worker pool (workers `<-` for a `RequestWork` message
  when ready).
