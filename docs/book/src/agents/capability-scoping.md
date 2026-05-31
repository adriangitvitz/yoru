# Capability scoping

A tool can declare a **capability** - a named permission that must be in
scope at call time. Calling the tool without the capability returns
`Result.Err{kind: "capability_denied"}`.

This is how you safely expose dangerous tools to agents: declare the
capability on the tool, then grant it only inside a request handler
that has authenticated the caller.

## Declaring a capability requirement

```yoru
tool ReadPatient {
  description: "Read a patient record by MRN."
  input { mrn: String }
  output: String
  capability: .phi_read
  effect: [DB]

  fn run(self, i: ReadPatient.Input) -> String effect [DB] {
    DB.query_one("SELECT data FROM patients WHERE mrn = ?", [i.mrn])
  }
}
```

Calling `ReadPatient.run(mrn: "M-1")` from a context that does **not**
hold the `phi_read` capability returns `Result.Err{kind: "capability_denied"}`.

## Granting a capability

Use the `with_capability` builtin:

```yoru
let record = with_capability("phi_read", fn() => ReadPatient.run(mrn: "M-1"))
```

`with_capability` takes the capability name as a string and a
zero-argument lambda. The capability is on the call stack only for the
duration of the lambda's invocation. Nested `with_capability` calls
preserve outer capabilities additively.

## Why this design?

- **Auditability.** Every privileged call is visible in a `git grep
  with_capability`.
- **Composability.** You can write one agent that can do everything, and
  expose it through different HTTP routes with different capability
  envelopes:

  ```yoru
  // /public/* - no privileged tools
  fn public_handler(req: Request) -> Response { my_agent.chat(req.body) }

  // /admin/* - same agent, with admin capability lit up
  fn admin_handler(req: Request) -> Response {
    with_capability("admin", fn() => my_agent.chat(req.body))
  }
  ```

  Identical agent declaration, different blast radius per route.

- **Runtime enforcement.** Because an LLM can choose to call a tool at
  any moment during reasoning, the check has to happen at the call site,
  not at compile time. `with_capability` makes that check.

## Capability names are strings

Capability names are arbitrary strings - `"phi_read"`, `"admin"`,
`"billing"`, whatever you like. The convention is `snake_case`. The
runtime does not interpret them; it just compares them.

## Failure mode

```yoru
let result = ReadPatient.run(mrn: "M-1")    // no capability granted
match result {
  Err(e) if e.kind == "capability_denied" => "denied"
  r => "ok: " + to_string(r)
}
```

The error includes the capability name in `e.message`, so logs and
error responses can point at the exact missing permission.
