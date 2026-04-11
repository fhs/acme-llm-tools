// Copyright © 2026 Fazlul Shahriar. All rights reserved.
// Use of this source code is governed by the MIT License
// that can be found in the LICENSE file.

package acmetools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates and configures the acme MCP server.
func NewServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "acme-mcp",
		Version: "0.1.0",
	}, nil)
	registerTools(s)
	return s
}

func registerTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_windows",
		Description: "List all open acme windows with their IDs, names, and metadata.",
	}, handleListWindows)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_body",
		Description: "Get the full text content of an acme window body.",
	}, handleGetBody)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_body",
		Description: "Replace the entire body of an acme window with new text.",
	}, handleSetBody)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "append_body",
		Description: "Append text to the end of an acme window body.",
	}, handleAppendBody)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_tag",
		Description: "Get the full tag (title bar) text of an acme window.",
	}, handleGetTag)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_tag",
		Description: "Set the user-editable portion of an acme window tag (appends to tag area after default buttons).",
	}, handleSetTag)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_addr",
		Description: "Get the current dot (selection) address as rune offsets q0 and q1.",
	}, handleGetAddr)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_addr",
		Description: "Set the address range using an acme address expression (e.g. \"1,2\", \"/foo/\", \"#0,$\"). Returns the resolved rune offsets.",
	}, handleSetAddr)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_selection",
		Description: "Get the currently selected text (dot) and its rune offsets q0 and q1.",
	}, handleGetSelection)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "replace_text",
		Description: "Replace text matched by an acme address expression with new text. Returns the resulting rune offsets.",
	}, handleReplaceText)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_window",
		Description: "Create a new acme window with the given name and optional initial body text.",
	}, handleCreateWindow)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_window",
		Description: "Delete an acme window. Use force=true to delete even if modified.",
	}, handleDeleteWindow)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_ctl",
		Description: "Send a control command to an acme window (e.g. \"show\", \"clean\", \"dirty\", \"mark\", \"nomark\").",
	}, handleSendCtl)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_window_info",
		Description: "Get detailed information about an acme window including size and font.",
	}, handleGetWindowInfo)
}
