# Tools

A **tool** is a named, typed unit of capability that the runtime - or an
LLM agent - can invoke. The compiler auto-generates the JSON Schema that
LLMs need from the tool's input fields, so there is no documentation
drift between the tool's signature and what the model sees.

## Declaring a tool

```yoru
tool SearchOrders {
  description: "Search orders by customer email."
  input {
    email: String,
    limit: Int = 10,
  }
  output: [OrderSummary]
  effect: [DB]

  fn run(self, i: SearchOrders.Input) -> [OrderSummary] effect [DB] {
    DB.query("SELECT ... WHERE email = $1 LIMIT $2",
             [i.email, to_string(i.limit)])
  }
}
```

| Field         | Purpose                                                                       |
|---------------|-------------------------------------------------------------------------------|
| `description` | What the tool does, in prose. Goes into the LLM tool catalogue.               |
| `input`       | Typed input fields. Required unless they have a default or are `Option[T]`.   |
| `output`      | Either a type expression (`output: String`) or a structured block (`output { ... }`) - see below. |
| `effect`      | Effects the implementation may use. Enforced by the type checker.             |
| `capability`  | Optional. Locks the tool behind a runtime capability. See [Capability scoping](./capability-scoping.md). |

## Output: type expression vs structured block

Two forms:

```yoru
output: String                    // type expression - no validation, pass-through
output: [OrderSummary]
```

vs the **structured form**:

```yoru
output {
  orders:   [OrderSummary]   @doc("Matching orders, newest first")
  total:    Int              @doc("Total count, ignoring limit")
  has_more: Bool             @doc("Whether more results exist beyond limit")
}
```

When you use the block form, the runtime **validates** the `fn run`
return shape against the declared fields, **re-tags** the resulting
`ObjectVal` as `<ToolName>.Output` so downstream code sees the typed
value, and **emits an `outputSchema`** in `tools/list` so the LLM
knows the response shape.

Missing required fields surface as `Result.Err{kind: "tool_output_invalid"}`.
A `Result.Err(...)` returned from the body passes through verbatim
without being wrapped.

```yoru
let r = SearchOrders.run(email: "a@x.y")
type_of(r)        // "SearchOrders.Output"
r.orders[0].id    // typed field access, no JSON.decode dance
```

## Tools as values

A bare tool name resolves to a first-class value of type `Tool`:

```yoru
let t = SearchOrders
type_of(t)             // "Tool"
t.run(email: "...")    // method call on the value
```

This unlocks composition patterns:

```yoru
// Pass a tool as a function argument
fn invoke(t, q: String) { t.run(q: q) }

// Build registries / dispatch tables
let by_action = Map.of("search", Search, "refund", Refund)
let chosen = by_action.get(action)
chosen.run(args)

// Iterate over tools for introspection
for t in [Search, Refund] {
  println(t.name() + " - " + t.description())
}
```

Method dispatch on a `Tool` value:

| Method | Returns |
|--------|---------|
| `t.run(args...)` | The tool's return value (validated if `output { ... }`) |
| `t.name()` | The tool's declared name as `String` |
| `t.description()` | The tool's `description:` as `String` |
| `t.input_schema()` | Input JSON Schema as `String` |
| `t.output_schema()` | Output JSON Schema as `String`, or `nil` if no `output { ... }` block |
| `t.schema()` | Full MCP-shape tool schema as `String` |

The existing `MyTool.run(...)` and `MyTool.schema()` call sites
continue to work unchanged - they're now method calls on the
resolved value.

The `run` method receives `self` (the tool instance, used for stateful
tools) plus an auto-synthesised `<ToolName>.Input` object.

## Calling a tool directly

```yoru
let orders = SearchOrders.run(email: "a@example.com")
```

Fields are passed as named arguments. Defaults apply when omitted.

## Getting the JSON Schema

```yoru
let schema = SearchOrders.schema()
```

`schema()` returns the MCP-wire-compatible form:

```
{
  "name":        "SearchOrders",
  "description": "...",
  "inputSchema": { ... JSON Schema object ... }
}
```

The camelCase `inputSchema` field matches what MCP servers emit, so the
output of `MyTool.schema()` can be piped straight into a hand-rolled MCP
response without renaming. (The Anthropic tools API uses snake_case
`input_schema`; that translation happens inside `agent/anthropic_client.go`
and isn't exposed to user code.)

## Input fields and JSON Schema

| Yoru type            | JSON Schema |
|----------------------|-------------|
| `String`             | `{"type": "string"}` |
| `Int`                | `{"type": "integer"}` |
| `Float`              | `{"type": "number"}` |
| `Bool`               | `{"type": "boolean"}` |
| `Option[T]`          | Same shape as `T`, but absent from `required` |
| `[T]`                | `{"type": "array", "items": <T>}` |
| User `object Foo`    | `{"type": "object", "properties": ..., "required": [...]}` - recurses into each field |
| User `enum E` (all unit variants) | `{"type": "string", "enum": [...]}` |
| User `enum E` (with payloads)     | `{"anyOf": [...]}` with a `kind` discriminator - see below |

Defaults become the `default` in the schema. `@doc("...")` annotations
become the `description` of a field.

```yoru
input {
  query: String @doc("Search query, max 200 chars"),
  limit: Int = 10 @doc("Max results, 1-100"),
}
```

## Tagged unions as tool inputs

When a tool input is typed as a Yoru enum whose variants carry fields,
the schema generator emits a JSON-Schema `anyOf` with a `kind`
discriminator. Each variant becomes one branch in `anyOf`, the `kind`
field is a `const` carrying the variant name, and the payload fields
follow.

```yoru
enum PatchOp {
  Fuzzy(old_text: String, new_text: String, count: Int),
  Insert(line: Int, content: String),
  Delete(start: Int, end: Int),
}

tool ApplyPatches {
  description: "Apply ordered patches to a text file."
  input {
    path: String,
    ops:  [PatchOp],
  }
  output: String
  fn run(self, i: ApplyPatches.Input) -> String {
    mut content = FS.read(i.path) ?? ""
    for op in i.ops {
      content = match op {
        PatchOp.Fuzzy(o, n, c) => Fuzzy.find_replace(content, o, n, c).result,
        PatchOp.Insert(line, text) => insert_line(content, line, text),
        PatchOp.Delete(s, e) => delete_lines(content, s, e),
      }
    }
    content
  }
}
```

The LLM sees `ops` as an array of `anyOf`-shaped objects:

```json
{
  "type": "array",
  "items": {
    "anyOf": [
      {"type": "object", "properties": {"kind": {"const": "Fuzzy"}, "old_text": {"type":"string"}, "new_text": {"type":"string"}, "count": {"type":"integer"}}, "required": ["kind","old_text","new_text","count"]},
      {"type": "object", "properties": {"kind": {"const": "Insert"}, "line": {"type":"integer"}, "content": {"type":"string"}}, "required": ["kind","line","content"]},
      {"type": "object", "properties": {"kind": {"const": "Delete"}, "start": {"type":"integer"}, "end": {"type":"integer"}}, "required": ["kind","start","end"]}
    ]
  }
}
```

The model then sends, for example:

```json
{"path": "/tmp/x.txt", "ops": [
  {"kind": "Fuzzy", "old_text": "old", "new_text": "new", "count": 0},
  {"kind": "Delete", "start": 5, "end": 7}
]}
```

The runtime reconstructs each element as an `EnumVal` of the right
variant before `fn run` observes it - including recursion into nested
object or enum types. Wrong/missing `kind`, unknown variant, or
missing required payload fields surface as `Result.Err{kind:
"tool_invocation_failed"}` with a precise message ("variant
'PatchOp.Insert' missing field 'line'") that the agent can recover
from on the next turn.

## When to make something a tool

- An LLM agent should be able to call it.
- You want it exposed over MCP.
- It is a discrete capability with a name worth knowing - not a private
  helper.

For internal helpers, a plain `fn` is enough.
