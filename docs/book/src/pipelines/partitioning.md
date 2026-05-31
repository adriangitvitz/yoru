# Partitioning and back-pressure

## `partition: N`

The `partition` stage fans data out to `N` worker goroutines for the
next transform.

```yoru
pipeline EnrichOrders {
  source: orders_today
  |> partition: 8
  |> transform: enrich_order
  |> sink: Warehouse.upsert
}
```

Use `partition` when:

- A transform is CPU-heavy.
- A transform issues outbound I/O that benefits from concurrency.
- You can tolerate out-of-order completion.

Avoid `partition` when downstream consumers require strict ordering.

## `on_error:` policy

Pipelines accept an `on_error:` clause to declare what to do when a
transform fails. Today the parser captures the policy as a tagged
value; the runtime can introspect it but does not yet enforce
DLQ semantics automatically. Most transform-level failures should still
be returned as `Result.Err(...)` and routed downstream with a tagged
enum until runtime enforcement lands.

```yoru
pipeline Ingest {
  source: kafka_stream
  |> transform: parse
  |> sink: warehouse_sink
  on_error: .dead_letter_queue(
    topic: "ingest-dlq",
    max_retries: 3,
    backoff: .exponential(base: 1s, max: 60s)
  )
}
```

The `.tag(args...)` form is **leading-dot enum shorthand**. Each one
evaluates to a `Policy` object with the shape:

```
Policy {
  tag:   "dead_letter_queue",
  args:  [],
  named: { topic: "ingest-dlq", max_retries: 3, backoff: Policy { tag: "exponential", ... } }
}
```

Anything you put in there is fair game - `.retry(3)`, `.skip`,
`.escalate("ops-pager")`, whatever fits the runtime you're aiming at.

## Time-unit literals

Durations like `1s`, `500ms`, `5m`, `2h`, `1.5s` are first-class
literals. They desugar at parse time to the canonical **millisecond
count as an `Int`**:

| Literal | Value (Int ms) |
|---------|----------------|
| `1ms`   | `1`            |
| `1s`    | `1000`         |
| `1m`    | `60_000`       |
| `1h`    | `3_600_000`    |
| `1.5s`  | `1500`         |

Recognized units: `ns`, `us`, `ms`, `s`, `m`, `h`. The suffix must
immediately follow the number - `1seconds` lexes as `1` plus identifier
`seconds`, not as a malformed duration.

## `back_pressure:` policy

Same leading-dot shape as `on_error:`. Parsed today; the runtime
captures the declaration but the eager pipeline engine does not yet
apply the constraint automatically. For bounded sinks today, size your
source manually.

```yoru
pipeline Ingest {
  source: kafka_stream
  |> transform: parse
  |> sink: warehouse_sink
  back_pressure: .bounded(capacity: 1024)
}
```

## When to use a pipeline vs. actors

| Need | Reach for |
|------|----------|
| Stateless transform over a stream | Pipeline |
| Stateful long-running worker | Actor |
| Pure fan-out for parallelism | Pipeline (`partition`) |
| Coordinating actors with replies | `.ask` on actor refs |

The two compose: a pipeline's sink can be `actor_ref <- Emit(item)` to
push results into an actor for further reduction.
