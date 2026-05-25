# MCP servers

An **MCP server** exposes Yoru tools to any
[Model Context Protocol](https://modelcontextprotocol.io/) client —
Claude Desktop, `mcp-cli`, custom integrations. The Yoru compiler emits
a self-contained binary that speaks JSON-RPC 2.0 over stdio (or HTTP).

## Declaring an MCP server

```yoru
tool LookupTopic {
  description: "Look up a Wikipedia topic by canonical title."
  input { topic: String }
  output: String
  effect: [HTTP]
  fn run(self, i: LookupTopic.Input) -> String {
    let r = HTTP.get("https://en.wikipedia.org/api/rest_v1/page/summary/" + i.topic)
    match r {
      Err(_) => "request failed"
      resp   => to_string(JSON.get(JSON.decode(resp.body), "extract"))
    }
  }
}

mcp ResearchServer {
  name: "yoru-research"
  version: "1.0.0"
  tools: [LookupTopic]
  transport: .stdio
}
```

| Field         | Purpose                                          |
|---------------|--------------------------------------------------|
| `name`        | MCP server name returned in `initialize`.        |
| `version`     | Server version string.                           |
| `tools`       | List of tool types to expose.                    |
| `transport`   | `.stdio` for desktop clients, `.http(port: N)` for network clients. |
| `auth`        | Optional. Bare label (`auth: .api_key`, `auth: .jwt`) or parameterised form (`auth: .jwt(secret: env("JWT_SECRET"))`). Args land in `MCPDecl.AuthArgs` for the build step to consume. |

## Building the binary

```sh
yoru build --target mcp --output ./bin/research-mcp research_mcp.yr
```

This produces a standalone binary. No runtime dependencies beyond the
network calls the tools themselves make.

> Use the **long flag** `--output`. The short `-o` form is silently
> ignored and the binary will land at `./<basename>` in the current
> working directory.

## Smoke-testing from the shell

```sh
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"LookupTopic","arguments":{"topic":"Linux"}}}' \
  | ./bin/research-mcp
```

You should see three JSON-RPC responses: the initialize handshake, the
tool catalogue, and the tool result.

## Wiring into Claude Desktop

Add this entry to `~/Library/Application Support/Claude/claude_desktop_config.json`
(macOS) or the equivalent on your platform:

```json
{
  "mcpServers": {
    "research": {
      "command": "/absolute/path/to/research-mcp"
    }
  }
}
```

Restart Claude Desktop. The tools appear in the model's tool drawer.

## What the schema looks like

The Yoru compiler turns each tool's `input` block into a JSON Schema and
serves it under `tools/list`. The model sees:

```json
{
  "name": "LookupTopic",
  "description": "Look up a Wikipedia topic by canonical title.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "topic": { "type": "string" }
    },
    "required": ["topic"]
  }
}
```

You never write this by hand; it follows the Yoru type declaration.
(`Tool.schema()` inside Yoru returns the same data with the field
spelled `input_schema` — see [Tools](./tools.md). The MCP build
converts on the way out.)

## Effects in an MCP context

Every tool call runs in the MCP server process, which means the server
must satisfy every effect the tools declare. For `HTTP`, this is
automatic. For `DB`, you must configure the connection at startup
(typically in `fn main()` before the server begins listening).
