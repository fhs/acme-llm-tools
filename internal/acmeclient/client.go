// Copyright © 2026 Fazlul Shahriar. All rights reserved.
// Use of this source code is governed by the MIT License
// that can be found in the LICENSE file.

package acmeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"9fans.net/go/acme"
	acp "github.com/coder/acp-go-sdk"
)

type acmeClient struct {
	win          *acme.Win
	agentName    string
	sessionID    acp.SessionId
	noFS         bool
	writeMu      sync.Mutex
	promptMu     sync.Mutex
	promptWin    *acme.Win
	promptWinMu  sync.Mutex // guards promptWin
	terminals    terminalMap
	permMu       sync.Mutex // guards permCh
	permCh       chan int    // non-nil while RequestPermission is waiting
	inThought    bool        // true while streaming a thought block
	commandsMu   sync.Mutex             // guards commands
	commands     []acp.AvailableCommand // populated on AvailableCommandsUpdate
	configsMu    sync.Mutex             // guards configOptions
	configOptions []acp.SessionConfigOption
}

// traceWriter wraps an io.WriteCloser and logs each write to stderr.
type traceWriter struct {
	io.WriteCloser
}

func (w *traceWriter) Write(p []byte) (int, error) {
	fmt.Fprintf(os.Stderr, "→ %s", p)
	return w.WriteCloser.Write(p)
}

// prefixWriter logs writes to an underlying writer with a prefix on each line.
type prefixWriter struct {
	w      io.Writer
	prefix string
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	fmt.Fprintf(w.w, "%s%s", w.prefix, p)
	return len(p), nil
}

// Run creates an acme window, spawns the agent, and runs the event loop.
// If resume is non-empty, it loads the existing session with that ID instead of creating a new one.
func Run(ctx context.Context, agentArgs []string, trace bool, resume string, noFS bool, configPairs []string) error {
	w, err := acme.New()
	if err != nil {
		return fmt.Errorf("open acme window: %w", err)
	}

	c := &acmeClient{win: w, noFS: noFS}

	agentCtx, cancelAgent := context.WithCancel(ctx)
	defer cancelAgent()

	// Spawn agent subprocess.
	cmd := exec.CommandContext(agentCtx, agentArgs[0], agentArgs[1:]...)
	agentStdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("agent stdin pipe: %w", err)
	}
	agentStdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("agent stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}
	defer func() {
		_ = agentStdin.Close()
		cancelAgent()
		_ = cmd.Wait()
	}()

	var connWriter io.Writer = agentStdin
	var connReader io.Reader = agentStdout
	if trace {
		connWriter = &traceWriter{agentStdin}
		connReader = io.TeeReader(agentStdout, &prefixWriter{w: os.Stderr, prefix: "← "})
	}
	conn := acp.NewClientSideConnection(c, connWriter, connReader)

	cwd, _ := os.Getwd()
	caps := acp.ClientCapabilities{}
	if !noFS {
		caps.Fs = acp.FileSystemCapability{
			ReadTextFile:  true,
			WriteTextFile: true,
		}
	}
	initResp, err := conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: caps,
	})
	if err != nil {
		return fmt.Errorf("ACP initialize: %w", err)
	}

	// Determine agent display name.
	agentName := filepath.Base(agentArgs[0])
	if initResp.AgentInfo != nil {
		if initResp.AgentInfo.Title != nil {
			agentName = *initResp.AgentInfo.Title
		} else {
			agentName = initResp.AgentInfo.Name
		}
	}
	agentName = strings.ReplaceAll(agentName, " ", "-")

	// Create or resume a session.
	var sessID acp.SessionId
	if resume != "" {
		if !initResp.AgentCapabilities.LoadSession {
			return fmt.Errorf("agent does not support session resume")
		}
		sessID = acp.SessionId(resume)
		loadResp, loadErr := conn.LoadSession(ctx, acp.LoadSessionRequest{
			SessionId:  sessID,
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		if loadErr != nil {
			return fmt.Errorf("ACP load session: %w", loadErr)
		}
		if len(loadResp.ConfigOptions) > 0 {
			c.configOptions = loadResp.ConfigOptions
		}
	} else {
		sessResp, err := conn.NewSession(ctx, acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		if err != nil {
			return fmt.Errorf("ACP new session: %w", err)
		}
		sessID = sessResp.SessionId
		if len(sessResp.ConfigOptions) > 0 {
			c.configOptions = sessResp.ConfigOptions
		}
	}
	c.sessionID = sessID
	c.agentName = agentName

	// Apply any config options requested via -config flags.
	for _, pair := range configPairs {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			return fmt.Errorf("invalid -config value %q: expected id=value", pair)
		}
		c.setConfigOption(ctx, conn, acp.SessionConfigId(k), acp.SessionConfigValueId(v))
	}

	// Name the window and set up the tag now that we have the session ID.
	w.Name("/ACP/%s/%s", agentName, sessID)
	w.Write("tag", []byte("Prompt Cancel"))
	if len(c.configOptions) > 0 {
		w.Write("tag", []byte(" Config"))
	}
	w.Write("tag", []byte(" Slash"))
	w.Ctl("clean")

	// Set the acme dump command so "Dump"/"Load" preserves the session.
	execPath, _ := os.Executable()
	dumpParts := []string{execPath, "-resume", string(sessID)}
	if trace {
		dumpParts = append(dumpParts, "-rpc.trace")
	}
	if noFS {
		dumpParts = append(dumpParts, "-no-fs")
	}
	for _, pair := range configPairs {
		dumpParts = append(dumpParts, "-config", pair)
	}
	dumpParts = append(dumpParts, agentArgs...)
	w.Ctl("dump " + strings.Join(dumpParts, " "))
	w.Ctl("dumpdir " + cwd)

	action := "connected to"
	if resume != "" {
		action = "resumed"
	}
	c.appendLine("[acme-acp: " + action + " " + agentName + "]\n")
	w.Ctl("clean")

	go c.openPromptWindow(ctx, conn)

	// Event loop.
	for e := range w.EventChan() {
		if e == nil {
			break
		}
		switch {
		case e.C2 == 'x' || e.C2 == 'X':
			switch string(e.Text) {
			case "Del":
				c.promptWinMu.Lock()
				if c.promptWin != nil {
					c.promptWin.Del(true)
				}
				c.promptWinMu.Unlock()
				c.win.Del(true)
				return nil
			case "Prompt":
				go c.openPromptWindow(ctx, conn)
			case "Config":
				c.printConfigs()
				c.win.Ctl("clean")
			case "Slash":
				c.printCommands()
				c.win.Ctl("clean")
			case "Cancel":
				go func() {
					if err := conn.Cancel(ctx, acp.CancelNotification{SessionId: c.sessionID}); err == nil {
						c.win.Ctl("clean")
					}
				}()
			default:
				w.WriteEvent(e)
			}
		case e.C2 == 'l' || e.C2 == 'L':
			word := strings.TrimSpace(string(e.Text))
			if c.takePermInput(word) {
				// consumed by permission handler
			} else if cfgID, valID, ok := c.configByToken(word); ok {
				go c.setConfigOption(ctx, conn, cfgID, valID)
			} else {
				w.WriteEvent(e)
			}
		}
	}
	return nil
}

func (c *acmeClient) openPromptWindow(ctx context.Context, conn *acp.ClientSideConnection) {
	c.promptWinMu.Lock()
	if c.promptWin != nil {
		c.promptWin.Ctl("show")
		c.promptWinMu.Unlock()
		return
	}
	pw, err := acme.New()
	if err != nil {
		c.appendLine("[error: could not open prompt window: " + err.Error() + "]\n")
		c.promptWinMu.Unlock()
		return
	}
	pw.Name("/ACP/%s/%s/prompt", c.agentName, c.sessionID)
	pw.Write("tag", []byte("Send"))
	pw.Ctl("clean")
	c.promptWin = pw
	c.promptWinMu.Unlock()

	c.runPromptWindow(ctx, conn, pw)

	c.promptWinMu.Lock()
	c.promptWin = nil
	c.promptWinMu.Unlock()
}

func (c *acmeClient) runPromptWindow(ctx context.Context, conn *acp.ClientSideConnection, pw *acme.Win) {
	for e := range pw.EventChan() {
		if e == nil {
			break
		}
		switch {
		case (e.C2 == 'x' || e.C2 == 'X') && string(e.Text) == "Send":
			body, err := pw.ReadAll("body")
			if err != nil {
				c.appendLine("[error: reading prompt window: " + err.Error() + "]\n")
				continue
			}
			text := strings.TrimRight(string(body), "\n")
			if text == "" {
				continue
			}
			// Copy prompt to main window.
			c.appendLine("> " + strings.ReplaceAll(text, "\n", "\n> ") + "\n")
			// Clear the prompt window body.
			pw.Addr(",")
			pw.Write("data", []byte(""))
			pw.Ctl("clean")
			// Send to agent.
			go c.prompt(ctx, conn, text)
		case (e.C2 == 'x' || e.C2 == 'X') && string(e.Text) == "Del":
			pw.Del(true)
			return
		case e.C2 == 'x' || e.C2 == 'X':
			pw.WriteEvent(e)
		case e.C2 == 'l' || e.C2 == 'L':
			pw.WriteEvent(e)
		}
	}
}

func (c *acmeClient) prompt(ctx context.Context, conn *acp.ClientSideConnection, text string) {
	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	_, err := conn.Prompt(ctx, acp.PromptRequest{
		SessionId: c.sessionID,
		Prompt:    []acp.ContentBlock{acp.TextBlock(text)},
	})
	if err != nil {
		c.appendLine("\n[error: " + err.Error() + "]\n")
	} else {
		c.appendLine("\n")
	}
	c.win.Ctl("clean")
}

func (c *acmeClient) setPermCh(ch chan int) {
	c.permMu.Lock()
	defer c.permMu.Unlock()
	c.permCh = ch
}

// takePermInput delivers word to the pending permission channel if one exists
// and word is a positive integer. Returns true if consumed, false if the event
// should be forwarded to acme.
func (c *acmeClient) takePermInput(word string) bool {
	c.permMu.Lock()
	defer c.permMu.Unlock()
	if c.permCh == nil {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(word))
	if err != nil || n < 1 {
		return false
	}
	c.permCh <- n
	return true
}

func (c *acmeClient) appendLine(s string) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.win.Write("body", []byte(s))
}

// SessionUpdate handles streaming agent output.
func (c *acmeClient) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	u := n.Update
	switch {
	case u.UserMessageChunk != nil:
		content := u.UserMessageChunk.Content
		if content.Text != nil {
			c.writeMu.Lock()
			if c.inThought {
				c.win.Write("body", []byte("]\n"))
				c.inThought = false
			}
			c.win.Write("body", []byte("> "+content.Text.Text+"\n"))
			c.writeMu.Unlock()
		}
	case u.AgentMessageChunk != nil:
		content := u.AgentMessageChunk.Content
		if content.Text != nil {
			c.writeMu.Lock()
			if c.inThought {
				c.win.Write("body", []byte("]\n"))
				c.inThought = false
			}
			c.win.Write("body", []byte(content.Text.Text))
			c.writeMu.Unlock()
		}
	case u.AgentThoughtChunk != nil:
		content := u.AgentThoughtChunk.Content
		if content.Text != nil && content.Text.Text != "" {
			c.writeMu.Lock()
			if !c.inThought {
				c.win.Write("body", []byte("[thought: "))
				c.inThought = true
			}
			c.win.Write("body", []byte(content.Text.Text))
			c.writeMu.Unlock()
		}
	case u.ToolCall != nil:
		tc := u.ToolCall
		var sb strings.Builder
		sb.WriteString("[tool: " + tc.Title + "]")
		if tc.RawInput != nil {
			if b, err := json.Marshal(tc.RawInput); err == nil {
				const maxLen = 200
				s := string(b)
				if len(s) > maxLen {
					s = s[:maxLen] + "…"
				}
				sb.WriteString(" ")
				sb.WriteString(s)
			}
		}
		sb.WriteByte('\n')
		c.writeMu.Lock()
		if c.inThought {
			c.win.Write("body", []byte("]\n"))
			c.inThought = false
		}
		c.win.Write("body", []byte(sb.String()))
		c.writeMu.Unlock()
	case u.AvailableCommandsUpdate != nil:
		upd := u.AvailableCommandsUpdate
		c.commandsMu.Lock()
		c.commands = upd.AvailableCommands
		c.commandsMu.Unlock()
	case u.ConfigOptionUpdate != nil:
		upd := u.ConfigOptionUpdate
		c.configsMu.Lock()
		c.configOptions = upd.ConfigOptions
		c.configsMu.Unlock()
	}
	return nil
}

// RequestPermission displays the permission options and waits for the user to choose one.
func (c *acmeClient) RequestPermission(ctx context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	title := ""
	if p.ToolCall.Title != nil {
		title = *p.ToolCall.Title
	}

	var sb strings.Builder
	sb.WriteString("[permission: " + title + "]\n")
	if p.ToolCall.RawInput != nil {
		if b, err := json.MarshalIndent(p.ToolCall.RawInput, "  ", "  "); err == nil {
			sb.WriteString("  ")
			sb.Write(b)
			sb.WriteByte('\n')
		}
	}
	for i, opt := range p.Options {
		sb.WriteString(fmt.Sprintf("  %d/ %s\n", i+1, opt.Name))
	}
	c.appendLine(sb.String())
	c.win.Ctl("clean")

	ch := make(chan int, 1)
	c.setPermCh(ch)
	defer c.setPermCh(nil)

	for {
		select {
		case <-ctx.Done():
			return acp.RequestPermissionResponse{
				Outcome: acp.NewRequestPermissionOutcomeCancelled(),
			}, nil
		case n := <-ch:
			if n < 1 || n > len(p.Options) {
				continue
			}
			c.appendLine(fmt.Sprintf("[permission: %q selected]\n", p.Options[n-1].Name))
			return acp.RequestPermissionResponse{
				Outcome: func() acp.RequestPermissionOutcome {
					o := acp.NewRequestPermissionOutcomeSelected()
					o.Selected.OptionId = p.Options[n-1].OptionId
					return o
				}(),
			}, nil
		}
	}
}

func (c *acmeClient) printCommands() {
	c.commandsMu.Lock()
	cmds := c.commands
	c.commandsMu.Unlock()
	var sb strings.Builder
	if len(cmds) == 0 {
		c.appendLine("[commands: none]\n")
		return
	}
	sb.WriteString("[commands]\n")
	for _, cmd := range cmds {
		line := "/" + cmd.Name
		if cmd.Description != "" {
			line += "\t" + cmd.Description
		}
		if cmd.Input != nil && cmd.Input.Unstructured != nil {
			if hint := cmd.Input.Unstructured.Hint; hint != "" {
				line += " (" + hint + ")"
			}
		}
		sb.WriteString("  " + line + "\n")
	}
	c.appendLine(sb.String())
}

// flatConfigValues returns the flat (ungrouped) list of values for a SessionConfigOptionSelect.
func flatConfigValues(sel *acp.SessionConfigOptionSelect) []acp.SessionConfigSelectOption {
	if sel.Options.Ungrouped != nil {
		return []acp.SessionConfigSelectOption(*sel.Options.Ungrouped)
	}
	if sel.Options.Grouped != nil {
		var flat []acp.SessionConfigSelectOption
		for _, g := range *sel.Options.Grouped {
			flat = append(flat, g.Options...)
		}
		return flat
	}
	return nil
}

func (c *acmeClient) printConfigs() {
	c.configsMu.Lock()
	opts := c.configOptions
	c.configsMu.Unlock()
	if len(opts) == 0 {
		c.appendLine("[config: none]\n")
		return
	}
	var sb strings.Builder
	sb.WriteString("[config]\n")
	for i, opt := range opts {
		if opt.Select == nil {
			continue
		}
		sel := opt.Select
		vals := flatConfigValues(sel)
		sb.WriteString("  " + sel.Name + ":\n")
		for j, v := range vals {
			marker := "  "
			if v.Value == sel.CurrentValue {
				marker = "* "
			}
			label := v.Name
			if v.Description != nil {
				label = v.Name + ": " + *v.Description
			}
			fmt.Fprintf(&sb, "    %scfg%dv%d\t%s\n", marker, i+1, j+1, label)
		}
	}
	c.appendLine(sb.String())
}

// configByToken parses a "cfg{i}v{j}" token and returns the config ID and value ID.
func (c *acmeClient) configByToken(word string) (acp.SessionConfigId, acp.SessionConfigValueId, bool) {
	var i, j int
	if _, err := fmt.Sscanf(word, "cfg%dv%d", &i, &j); err != nil {
		return "", "", false
	}
	c.configsMu.Lock()
	defer c.configsMu.Unlock()
	if i < 1 || i > len(c.configOptions) {
		return "", "", false
	}
	sel := c.configOptions[i-1].Select
	if sel == nil {
		return "", "", false
	}
	vals := flatConfigValues(sel)
	if j < 1 || j > len(vals) {
		return "", "", false
	}
	return sel.Id, vals[j-1].Value, true
}

func (c *acmeClient) setConfigOption(ctx context.Context, conn *acp.ClientSideConnection, cfgID acp.SessionConfigId, valID acp.SessionConfigValueId) {
	resp, err := conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
		SessionId: c.sessionID,
		ConfigId:  cfgID,
		Value:     valID,
	})
	if err != nil {
		c.appendLine("[error: set config: " + err.Error() + "]\n")
		return
	}
	c.configsMu.Lock()
	c.configOptions = resp.ConfigOptions
	c.configsMu.Unlock()
	c.appendLine("[config: " + string(cfgID) + "=" + string(valID) + "]\n")
}

// findWindowByPath returns the acme window ID whose name matches path, or -1 if none.
func findWindowByPath(path string) int {
	wins, err := acme.Windows()
	if err != nil {
		return -1
	}
	for _, w := range wins {
		if w.Name == path {
			return w.ID
		}
	}
	return -1
}

// applyLineLimit applies the optional line (1-based start) and limit (max lines)
// parameters to content, as specified by the ACP ReadTextFile protocol.
func applyLineLimit(content string, line, limit *int) string {
	if line == nil && limit == nil {
		return content
	}
	lines := strings.SplitAfter(content, "\n")
	start := 0
	if line != nil && *line > 1 {
		start = *line - 1
	}
	if start >= len(lines) {
		return ""
	}
	lines = lines[start:]
	if limit != nil && *limit < len(lines) {
		lines = lines[:*limit]
	}
	return strings.Join(lines, "")
}

// ReadTextFile reads a file, preferring an open acme window's body when available.
func (c *acmeClient) ReadTextFile(ctx context.Context, p acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	var content string
	if !c.noFS {
		if id := findWindowByPath(p.Path); id >= 0 {
			if w, err := acme.Open(id, nil); err == nil {
				if body, err := w.ReadAll("body"); err == nil {
					content = string(body)
				}
			}
		}
	}
	if content == "" {
		data, err := os.ReadFile(p.Path)
		if err != nil {
			return acp.ReadTextFileResponse{}, err
		}
		content = string(data)
	}
	return acp.ReadTextFileResponse{Content: applyLineLimit(content, p.Line, p.Limit)}, nil
}

// WriteTextFile writes a file and reflects the change in an acme window.
func (c *acmeClient) WriteTextFile(ctx context.Context, p acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if err := os.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if !c.noFS {
		if id := findWindowByPath(p.Path); id >= 0 {
			if w, err := acme.Open(id, nil); err == nil {
				w.Addr(",")
				w.Write("data", []byte(p.Content))
				w.Ctl("clean")
			}
		} else {
			if w, err := acme.New(); err == nil {
				w.Name("%s", p.Path)
				w.Addr(",")
				w.Write("data", []byte(p.Content))
				w.Ctl("clean")
			}
		}
	}
	return acp.WriteTextFileResponse{}, nil
}
