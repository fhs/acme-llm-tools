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

### SYNOPSIS

```
acme-acp [-rpc.trace] [-resume uuid] [-no-fs] [-config id=value] agent [args...]
```

### DESCRIPTION

Acme-acp spawns an ACP-compatible agent process and manages its
interaction with Acme.  It creates a main window for displaying
agent output and a prompt window for composing user input.
Both windows are opened on startup.  Sessions can be resumed
across editor restarts.

### OPTIONS

`-config id=value`
: Set a session configuration option at startup.  May be
  repeated.  The option id and available values are those
  shown by the Config command (see below).

`-no-fs`
: Disable ACP filesystem support.  The agent will not be
  able to read or write files through Acme.

`-resume uuid`
: Resume an existing session by its UUID instead of creating
  a new one.

`-rpc.trace`
: Print the ACP JSON-RPC trace to standard error.

### WINDOWS

On startup, acme-acp creates two windows.

The main window is named `/ACP/agent/sessionID`.  Its tag
contains the buttons Prompt, Cancel, Config, and Slash.
Agent output appears here in several forms:

- Plain text for the agent's response.
- Lines prefixed with `> ` for echoed user messages.
- `[thought: text]` for the agent's chain-of-thought reasoning.
- `[tool: title]` for tool calls, followed by a truncated
  summary of their input.

When an agent requests permission to use a tool, the tool
call details are printed followed by numbered options.
Right-click the number corresponding to your choice
(e.g. `1` to allow, `2` to deny).

The prompt window is named `/ACP/agent/sessionID/prompt`.
This is where you type your prompt.  Its tag contains a
Send button.  Clicking Send transmits the prompt text to
the agent, echoes it in the main window prefixed with `> `,
and clears the prompt window for the next input.  Slash
commands advertised by the agent are also submitted from
the prompt window.

The prompt window may be closed and reopened via the
Prompt button at any time without losing the session.

### COMMANDS

The following tag buttons are available in the main window:

`Prompt`
: Reopen or bring forward the prompt window, in case it
  was closed.

`Cancel`
: Send a cancel notification to the agent, interrupting
  the current operation.

`Config`
: Print the session configuration options in the main
  window.  Each option value is labelled with a token of
  the form `cfg1v2`.  Right-click a token to select that
  value.  The current selection is marked with `*`.

`Slash`
: Print the slash commands advertised by the agent.
  These are displayed as `/name` with a description.
  To execute a slash command, type it in the prompt
  window and click Send.

In the prompt window:

`Send`
: Send the prompt text to the agent.

### SESSION RESUME

Each session is identified by a UUID assigned by the agent.
The main window's dump command includes the `-resume` flag
with this UUID, so sessions persist across Acme Dump/Load
cycles.

To resume a session manually:

```
acme-acp -resume UUID agent [args...]
```

## acme-mcp

### SYNOPSIS

```
acme-mcp
```

### DESCRIPTION

Acme-mcp is an MCP server that communicates over standard
input/output.  It exposes the following tools for manipulating
Acme windows:

- `list_windows` -- list all open windows
- `get_body`, `set_body`, `append_body` -- read or write window body
- `get_tag`, `set_tag` -- read or write the window tag
- `get_addr`, `set_addr` -- get or set the address (dot)
- `get_selection` -- get selected text
- `replace_text` -- replace text matched by an address expression
- `create_window`, `delete_window` -- create or delete windows
- `send_ctl` -- send control messages (show, clean, dirty, etc.)
- `get_window_info` -- get window metadata (size, font, tab width)

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
