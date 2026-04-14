# acme-llm-tools

Tools for integrating LLM coding agents with the Acme text editor.

This repository currently provides two commands:

- **acme-acp** -- an [ACP](https://agentclientprotocol.com/) (Agent Client
  Protocol) client that runs coding agents inside Acme windows
- **acme-mcp** -- an [MCP](https://modelcontextprotocol.io/) (Model
  Context Protocol) server that exposes Acme window manipulation
  tools to LLM agents

More tools may be added in the future.

## Installation

```
go install github.com/fhs/acme-llm-tools/cmd/acme-acp@latest
go install github.com/fhs/acme-llm-tools/cmd/acme-mcp@latest
```

## acme-acp

See the [acme-acp documentation](cmd/acme-acp/doc.go) or run
[`go doc github.com/fhs/acme-llm-tools/cmd/acme-acp`](https://pkg.go.dev/github.com/fhs/acme-llm-tools/cmd/acme-acp).

## acme-mcp

See the [acme-mcp documentation](cmd/acme-mcp/doc.go) or run
[`go doc github.com/fhs/acme-llm-tools/cmd/acme-mcp`](https://pkg.go.dev/github.com/fhs/acme-llm-tools/cmd/acme-mcp).

## Getting Started with Coding Agents

Authentication for each agent may need to be set up before
using acme-acp.  For example, Claude Code requires logging
in to your Anthropic account using the `claude` command.

### Claude Code

Claude Code does not include built-in ACP support.  Install
the [claude-agent-acp](https://www.npmjs.com/package/@agentclientprotocol/claude-agent-acp)
bridge, then run:

```
acme-acp claude-agent-acp
```

Optionally, to give Claude Code access to Acme windows via
MCP, add acme-mcp as an MCP server in your Claude Code
configuration.  This is not required to use acme-acp.

```json
{
  "mcpServers": {
    "acme-mcp": {
      "command": "acme-mcp"
    }
  }
}
```

### Coder Copilot

[Coder Copilot](https://github.com/features/copilot/cli/) supports
ACP natively.  Run it with:

```
acme-acp copilot --acp
```

### Gemini CLI

[Gemini CLI](https://geminicli.com/)
supports ACP natively.  Run it with:

```
acme-acp gemini --acp
```

## Code Generation

A significant part of this codebase was generated using LLM
coding agents, particularly Claude Code.  All generated code
has been carefully reviewed and tested.

## License

MIT.  See [LICENSE](LICENSE).
