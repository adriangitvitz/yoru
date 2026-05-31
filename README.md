# Yoru

<img width="2560" height="2560" alt="yoru_agent" src="https://github.com/user-attachments/assets/6ea81dfa-0259-4917-8f95-f5992704171e" />

A statically typed programming language for backend services, ETL pipelines, and LLM agent orchestration. Tools, agents, MCP servers, supervised actors, and HTTP services are first-class language primitives rather than libraries.

## Install

Requires Go 1.22+.

```sh
git clone https://github.com/adriangitvitz/yoru
cd yoru
go build ./cmd/yoru
```

The resulting binary is at `./yoru`. Add it to your PATH or invoke it directly.

## Hello world

Create `hello.yr`:

```yoru
fn main() {
  println("hello, yoru")
}
```

Run it:

```sh
./yoru run hello.yr
```

## CLI commands

| Command | What it does |
|---------|--------------|
| `yoru run <file.yr>` | Lex, parse, type check, evaluate. If the file declares a `service`, the HTTP server starts. If `fn main()` exists, it is auto-called. |
| `yoru check <file.yr>` | Lex, parse, type check only. Does not evaluate. |
| `yoru repl` | Interactive read-eval-print loop. |
| `yoru fmt <file.yr>` | Format a source file. |
| `yoru build --target mcp -o <name> <file.yr>` | Build a standalone MCP server binary that speaks JSON-RPC 2.0 on stdio. |
| `yoru build --target http -o <name> <file.yr>` | Build a standalone HTTP service binary. |
| `yoru version` | Print the version. |

## Environment variables

| Variable | Purpose |
|----------|---------|
| `OPENROUTER_API_KEY` | LLM client uses OpenRouter (recommended, multi-provider). Takes precedence. |
| `ANTHROPIC_API_KEY` | LLM client uses Anthropic directly. Used only when `OPENROUTER_API_KEY` is unset. |

If neither variable is set, agents still spawn but `.chat()` returns `Result.Err{kind: "llm_not_configured"}`. The rest of the language is unaffected.

## What you can build today

Concurrent backends with supervised actors. HTTP REST services with middleware, JWT auth, and OpenAPI generation. ETL pipelines with parallel partitioning. Tools whose JSON Schema is generated from Yoru type annotations. LLM agents that drive a reasoning loop with capability-gated tool access. MCP servers that any MCP client (Claude Desktop, mcp-cli) can consume. Multi-agent teams under a supervisor that restarts crashed children.

## Build a standalone MCP server

A `tool` block plus an `mcp` block compile to a binary that any MCP client (Claude Desktop, mcp-cli, custom) speaks to over JSON-RPC. The `output { ... }` schema becomes the wire `outputSchema` clients use to parse return values, so you author no JSON Schema by hand.

```yoru
tool FxRate {
  description: "Current FX rate. 3-letter ISO currency codes."
  input  { from: String, to: String }
  output {
    from:  String  @doc("3-letter ISO source currency")
    to:    String  @doc("3-letter ISO target currency")
    rate:  Float   @doc("How many `to` units one `from` unit buys")
    as_of: String  @doc("YYYY-MM-DD")
  }
  effect: [HTTP]
  fn run(self, i: FxRate.Input) -> Object {
    let r = HTTP.get("https://api.frankfurter.dev/v1/latest?base=" + i.from + "&symbols=" + i.to)
    let body = JSON.decode(r.body)
    let rates = JSON.get(body, "rates")
    {
      from:  i.from,
      to:    i.to,
      rate:  float(to_string(JSON.get(rates, i.to))),
      as_of: to_string(JSON.get(body, "date"))
    }
  }
}

mcp ResearchServer {
  name: "yoru-research"
  version: "1.0.0"
  tools: [FxRate]
  transport: .stdio
}
```

Build the binary:

```sh
./yoru build --target mcp --output research-mcp research_mcp.yr
```

## Multi-agent with structured handoff

Two agents from different vendors. The first emits a typed object via its `output { ... }` block. The runtime validates the JSON reply, retries on schema failure, and re-tags it as `TripExtractor.Output` so the second agent receives plain Yoru fields, not a string to parse.

```yoru
agent TripExtractor {
  model:  "anthropic/claude-3.5-haiku"
  system: "Extract trip parameters. Respond ONLY with valid JSON."
  output {
    destination: String  @doc("city + country")
    days:        Int     @doc("trip length in days")
    budget_usd:  Int     @doc("total budget in USD")
    vibe:        String  @doc("e.g. 'food + culture', 'adventure'")
  }
}

agent ItineraryWriter {
  model:  "openai/gpt-4o-mini"
  system: "Given a structured trip brief, write a concise day-by-day itinerary."
}

fn main() {
  let extractor = spawn TripExtractor()
  let writer    = spawn ItineraryWriter()

  match extractor.chat("4 days in Lisbon, ~$1500, food + history") {
    Err(e)   => println("extractor failed: " + e.kind)
    Ok(trip) => {
      let brief = "Destination: " + trip.destination +
                  "\nDays: " + to_string(trip.days) +
                  "\nBudget: $" + to_string(trip.budget_usd) +
                  "\nVibe: " + trip.vibe
      match writer.chat(brief) {
        Err(e)        => println("writer failed: " + e.kind)
        Ok(itinerary) => println(itinerary)
      }
    }
  }
}
```

Run it:

```sh
OPENROUTER_API_KEY=sk-or-... ./yoru run trip.yr
```

## Multi-agent orchestration with a critic

Three specialists wired by the coordinator function. The historian has a tool, the synthesizer composes prior outputs with no tools of its own, and the critic reviews the result. Each `spawn` is an independent goroutine, so a misbehaving agent cannot corrupt the others.

```yoru
tool WikiSummary {
  description: "Look up a topic on Wikipedia."
  input { title: String }
  output: String
  effect: [HTTP]
  fn run(self, i: WikiSummary.Input) -> String {
    let r = HTTP.get("https://en.wikipedia.org/api/rest_v1/page/summary/" + i.title)
    match r {
      Err(_) => "Wikipedia request failed"
      resp   => to_string(JSON.get(JSON.decode(resp.body), "extract"))
    }
  }
}

agent HistoryAgent {
  model: "anthropic/claude-sonnet-4.5"
  system: "History specialist. Use WikiSummary. Answer in 2 sentences."
  tools: [WikiSummary]
  config { max_turns: 4, budget_tokens: 1024 }
}

agent Synthesizer {
  model: "anthropic/claude-sonnet-4.5"
  system: "Combine specialist reports into a 3-sentence briefing. Invent nothing."
  tools: []
}

agent Critic {
  model: "anthropic/claude-sonnet-4.5"
  system: "Reply 'OK: <reason>' or 'ISSUE: <reason>'. No prose."
  tools: []
}

fn main() {
  let historian = spawn HistoryAgent()
  let synth     = spawn Synthesizer()
  let critic    = spawn Critic()

  let history  = historian.chat("Brief historical context for Berlin.") ?? "n/a"
  let briefing = synth.chat("Compose a traveler briefing:\n" + history) ?? "n/a"
  let verdict  = critic.chat("Review this briefing:\n" + briefing)      ?? "n/a"

  println("BRIEFING:\n" + briefing + "\n\nVERDICT: " + verdict)
}
```
