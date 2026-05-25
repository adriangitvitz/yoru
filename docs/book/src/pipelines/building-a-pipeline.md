# Building a pipeline

A **pipeline** is a typed chain of source → transforms → sink. Pipelines
are declarative: you describe the stages, the runtime takes care of
threading data through them.

## A minimal pipeline

```yoru
pipeline Squares {
  source: List.of([1, 2, 3, 4, 5, 6])
  |> transform: fn(x: Int) -> Int { x * x }
  |> sink: Collector.collect()
}

fn main() {
  let results = Squares.run()
  println(to_string(results))   // [1, 4, 9, 16, 25, 36]
}
```

The pieces:

| Stage       | Purpose                                                     |
|-------------|-------------------------------------------------------------|
| `source:`   | Where data comes from. Any expression producing a list or stream. |
| `transform:` | Apply a function to each element. |
| `sink:`     | Where the result lands. `Collector.collect()` returns a list. |

`|>` is the **pipe-forward** operator. It composes stages left to right.

### Filtering

A transform that returns `Bool` does **not** act as a filter — its
output (a list of booleans) just flows downstream. To filter, drop
items by returning `nil` from a transform (the runtime skips nil
results before the next stage):

```yoru
pipeline EvenSquares {
  source: List.of([1, 2, 3, 4, 5, 6])
  |> transform: fn(x: Int) -> Int { x * x }
  |> transform: fn(x: Int) { if x % 2 == 0 { x } else { nil } }
  |> sink: Collector.collect()
}
```

For richer filter logic, run the pipeline to completion and post-filter
with the top-level `filter(xs, pred)` builtin.

## Naming things

Each stage can be a `fn` literal or a reference to a named function or
tool's `.run` method:

```yoru
fn parse_record(raw: String) -> Record { ... }
fn enrich(rec: Record) -> EnrichedRecord effect [DB] { ... }

pipeline Ingest {
  source: File.lines("input.csv")
  |> transform: parse_record
  |> transform: enrich
  |> sink: Collector.collect()
}
```

This makes each stage independently testable.

## Running a pipeline

```yoru
let out = Ingest.run()
```

Pipelines run **eagerly** today — the entire source materialises before
transforms apply. Use bounded sources for large datasets, or chunk your
input manually.

## Errors inside a stage

If a transform returns `Result.Err(...)`, the runtime forwards it as a
failure event. The handling is governed by the pipeline's `on_error:`
clause (see [Partitioning and back-pressure](./partitioning.md)).
