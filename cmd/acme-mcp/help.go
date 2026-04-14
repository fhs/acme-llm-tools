// Copyright © 2026 Fazlul Shahriar. All rights reserved.
// Use of this source code is governed by the MIT License
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
)

const usageMsg = `Acme-mcp is an MCP server that communicates over standard
input/output.  It exposes the following tools for manipulating
Acme windows:

  - list_windows — list all open windows
  - get_body, set_body, append_body — read or write window body
  - get_tag, set_tag — read or write the window tag
  - get_addr, set_addr — get or set the address (dot)
  - get_selection — get selected text
  - replace_text — replace text matched by an address expression
  - create_window, delete_window — create or delete windows
  - send_ctl — send control messages (show, clean, dirty, etc.)
  - get_window_info — get window metadata (size, font, tab width)

Usage:

	acme-mcp
`

func usageText() string {
	return usageMsg
}

func init() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usageMsg)
	}
}
