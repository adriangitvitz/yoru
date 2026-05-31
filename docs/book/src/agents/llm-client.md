# Configuring the LLM client

Yoru selects an LLM client from environment variables at startup.

| Variable             | Provider          | Model name format                          | Notes                                                        |
|----------------------|-------------------|--------------------------------------------|--------------------------------------------------------------|
| `OPENROUTER_API_KEY` | OpenRouter        | `anthropic/claude-sonnet-4.5`, `openai/gpt-4o`, ... | Multi-provider gateway. Takes priority if both are set. |
| `ANTHROPIC_API_KEY`  | Anthropic direct  | `claude-sonnet-4-5`                         | Used only when `OPENROUTER_API_KEY` is unset.                |

If neither is set, agents still spawn but every `.chat(...)` returns
`Result.Err{kind: "llm_not_configured"}`. Nothing else in the language is
affected - tools, pipelines, actors, services, and HTTP all work
normally.

## Quick start with OpenRouter

```sh
export OPENROUTER_API_KEY=sk-or-...
yoru run my_agent.yr
```

OpenRouter is the path of least resistance because one key gets you
access to dozens of models (Claude, GPT, Gemini, Llama, ...). Switch
models by changing the `model:` field on the agent - no code changes.

## Quick start with Anthropic direct

```sh
export ANTHROPIC_API_KEY=sk-ant-...
yoru run my_agent.yr
```

Use direct mode if you want Anthropic-specific features (e.g.
prompt-caching headers) that OpenRouter does not yet forward.

## Programmatic injection

For tests or custom providers, construct the interpreter manually in Go
and call `Interpreter.SetLLMClient(client)` before evaluating the source:

```go
interp := interpreter.NewInterpreter()
interp.SetLLMClient(agent.NewMockClient(scriptedReplies))
stdlib.InstallAll(interp, os.Stderr)
interp.EvalSourceInto(source)
```

See `agent/mock_client.go` for the test mock and
`agent/openrouter_client.go` for the production translation between
Yoru's Anthropic-style envelope and the OpenAI chat-completions wire
format.

## Timeouts

| Knob              | Default | What it bounds                          |
|-------------------|---------|-----------------------------------------|
| `interpreter.ChatTimeout` | 120s | `.chat(...)` overall timeout.       |
| `interpreter.AskTimeout`  | 5s   | `.ask(...)` on actor refs.          |

To override, set them on the `Interpreter` instance before evaluation.
