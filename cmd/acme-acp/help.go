// Copyright © 2026 Fazlul Shahriar. All rights reserved.
// Use of this source code is governed by the MIT License
// that can be found in the LICENSE file.

package main

import (
	"os"
	"strings"
)

const usageMsg = `Acme-acp spawns an ACP-compatible agent process and manages its
interaction with Acme.  It creates a main window for displaying
agent output and a prompt window for composing user input.
Both windows are opened on startup.  Sessions can be resumed
across editor restarts.

Usage:

	acme-acp [-rpc.trace] [-resume uuid] [-no-fs] [-config id=value] agent [args...]

# Windows

On startup, acme-acp creates two windows.

The main window is named /ACP/agent/sessionID.  Its tag
contains the buttons Prompt, Cancel, Config, and Slash.
Agent output appears here in several forms:

  - Plain text for the agent's response.
  - Lines prefixed with "> " for echoed user messages.
  - "[thought: text]" for the agent's chain-of-thought reasoning.
  - "[tool: title]" for tool calls, followed by a truncated
    summary of their input.

When an agent requests permission to use a tool, the tool
call details are printed followed by numbered options.
Right-click the number corresponding to your choice
(e.g. 1 to allow, 2 to deny).

The prompt window is named /ACP/agent/sessionID/prompt.
This is where you type your prompt.  Its tag contains a
Send button.  Clicking Send transmits the prompt text to
the agent, echoes it in the main window prefixed with "> ",
and clears the prompt window for the next input.  Slash
commands advertised by the agent are also submitted from
the prompt window.

The prompt window may be closed and reopened via the
Prompt button at any time without losing the session.

# Commands

The following tag buttons are available in the main window:

Prompt: Reopen or bring forward the prompt window, in case it
was closed.

Cancel: Send a cancel notification to the agent, interrupting
the current operation.

Config: Print the session configuration options in the main
window.  Each option value is labelled with a token of
the form "cfg1v2".  Right-click a token to select that
value.  The current selection is marked with "*".

Slash: Print the slash commands advertised by the agent.
These are displayed as "/name" with a description.
To execute a slash command, type it in the prompt
window and click Send.

In the prompt window:

Send: Send the prompt text to the agent.

# Session resume

Each session is identified by a UUID assigned by the agent.
The main window's dump command includes the -resume flag
with this UUID, so sessions persist across Acme Dump/Load
cycles.

To resume a session manually:

	acme-acp -resume UUID agent [args...]
`

func usageText() string {
	var buf strings.Builder
	buf.WriteString(usageMsg)
	buf.WriteString("Flags:\n\n")
	appFlags.SetOutput(&buf)
	appFlags.PrintDefaults()
	appFlags.SetOutput(os.Stderr)
	return buf.String()
}
