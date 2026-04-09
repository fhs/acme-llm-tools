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
	writeMu      sync.Mutex
	promptMu     sync.Mutex
	promptWin    *acme.Win
	promptWinMu  sync.Mutex // guards promptWin
	terminals    terminalMap
	permMu       sync.Mutex // guards permCh
	permCh       chan int   // non-nil while RequestPermission is waiting
	inThought    bool        // true while streaming a thought block
	modesMu      sync.Mutex  // guards modeState
	modeState    *acp.SessionModeState
	modelsMu     sync.Mutex // guards modelState
	modelState   *acp.SessionModelState
	commandsMu   sync.Mutex             // guards commands
	commands     []acp.AvailableCommand // populated on AvailableCommandsUpdate
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
func Run(ctx context.Context, agentArgs []string, trace bool, resume string) error {
	w, err := acme.New()
	if err != nil {
		return fmt.Errorf("open acme window: %w", err)
	}

	c := &acmeClient{win: w}

	// Spawn agent subprocess.
	cmd := exec.CommandContext(ctx, agentArgs[0], agentArgs[1:]...)
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
	initResp, err := conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
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
		if loadResp.Modes != nil {
			c.modeState = loadResp.Modes
		}
		if loadResp.Models != nil {
			c.modelState = loadResp.Models
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
		if sessResp.Modes != nil {
			c.modeState = sessResp.Modes
		}
		if sessResp.Models != nil {
			c.modelState = sessResp.Models
		}
	}
	c.sessionID = sessID
	c.agentName = agentName

	// Name the window and set up the tag now that we have the session ID.
	w.Name("/ACP/%s/%s", agentName, sessID)
	w.Write("tag", []byte("Prompt Cancel"))
	if c.modeState != nil {
		w.Write("tag", []byte(" Mode"))
	}
	if c.modelState != nil {
		w.Write("tag", []byte(" Model"))
	}
	w.Write("tag", []byte(" Slash"))
	w.Ctl("clean")

	// Set the acme dump command so "Dump"/"Load" preserves the session.
	execPath, _ := os.Executable()
	dumpParts := []string{execPath, "-resume", string(sessID)}
	if trace {
		dumpParts = append(dumpParts, "-rpc.trace")
	}
	dumpParts = append(dumpParts, agentArgs...)
	w.Ctl("dump " + strings.Join(dumpParts, " "))
	w.Ctl("dumpdir " + cwd)

	action := "connected to"
	if resume != "" {
		action = "resumed"
	}
	c.appendLine("[acme-acp: " + action + " " + agentName + "]\n")

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
				return nil
			case "Prompt":
				go c.openPromptWindow(ctx, conn)
			case "Mode":
				c.printModes()
			case "Model":
				c.printModels()
			case "Slash":
				c.printCommands()
			case "Cancel":
				go func() {
					_ = conn.Cancel(ctx, acp.CancelNotification{SessionId: c.sessionID})
				}()
			default:
				w.WriteEvent(e)
			}
		case e.C2 == 'l' || e.C2 == 'L':
			word := strings.TrimSpace(string(e.Text))
			if c.takePermInput(word) {
				// consumed by permission handler
			} else if id, ok := c.modeByToken(word); ok {
				go c.setMode(ctx, conn, id)
			} else if id, ok := c.modelByToken(word); ok {
				go c.setModel(ctx, conn, id)
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
		return
	}
	c.appendLine("\n")
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
	case u.CurrentModeUpdate != nil:
		upd := u.CurrentModeUpdate
		c.modesMu.Lock()
		if c.modeState != nil {
			c.modeState.CurrentModeId = upd.CurrentModeId
		}
		c.modesMu.Unlock()
		c.appendLine("[mode: " + c.modeNameByID(upd.CurrentModeId) + "]\n")
	case u.AvailableCommandsUpdate != nil:
		upd := u.AvailableCommandsUpdate
		c.commandsMu.Lock()
		c.commands = upd.AvailableCommands
		c.commandsMu.Unlock()
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
				Outcome: acp.NewRequestPermissionOutcomeSelected(p.Options[n-1].OptionId),
			}, nil
		}
	}
}

func (c *acmeClient) printModes() {
	c.modesMu.Lock()
	ms := c.modeState
	c.modesMu.Unlock()
	if ms == nil {
		return
	}
	var sb strings.Builder
	sb.WriteString("[modes: current=" + c.modeNameByID(ms.CurrentModeId) + "]\n")
	for i, m := range ms.AvailableModes {
		marker := "  "
		if m.Id == ms.CurrentModeId {
			marker = "* "
		}
		label := m.Name
		if m.Description != nil {
			label = m.Name + ": " + *m.Description
		}
		sb.WriteString(fmt.Sprintf("  %smode%d\t%s\n", marker, i+1, label))
	}
	c.appendLine(sb.String())
}

// modeNameByID returns the human-readable name for a mode ID, falling back to the raw ID string.
// Must NOT be called with modesMu held.
func (c *acmeClient) modeNameByID(id acp.SessionModeId) string {
	c.modesMu.Lock()
	defer c.modesMu.Unlock()
	if c.modeState != nil {
		for _, m := range c.modeState.AvailableModes {
			if m.Id == id {
				return m.Name
			}
		}
	}
	return string(id)
}

// modeByToken parses an "m<n>" token and returns the corresponding mode ID.
func (c *acmeClient) modeByToken(word string) (acp.SessionModeId, bool) {
	var n int
	if _, err := fmt.Sscanf(word, "mode%d", &n); err != nil {
		return "", false
	}
	c.modesMu.Lock()
	defer c.modesMu.Unlock()
	if c.modeState == nil || n < 1 || n > len(c.modeState.AvailableModes) {
		return "", false
	}
	return c.modeState.AvailableModes[n-1].Id, true
}

func (c *acmeClient) setMode(ctx context.Context, conn *acp.ClientSideConnection, id acp.SessionModeId) {
	if _, err := conn.SetSessionMode(ctx, acp.SetSessionModeRequest{
		SessionId: c.sessionID,
		ModeId:    id,
	}); err != nil {
		c.appendLine("[error: set mode: " + err.Error() + "]\n")
		return
	}
	c.modesMu.Lock()
	if c.modeState != nil {
		c.modeState.CurrentModeId = id
	}
	c.modesMu.Unlock()
	c.appendLine("[mode: " + c.modeNameByID(id) + "]\n")
}

func (c *acmeClient) printModels() {
	c.modelsMu.Lock()
	ms := c.modelState
	c.modelsMu.Unlock()
	if ms == nil {
		return
	}
	var sb strings.Builder
	sb.WriteString("[models: current=" + c.modelNameByID(ms.CurrentModelId) + "]\n")
	for i, m := range ms.AvailableModels {
		marker := "  "
		if m.ModelId == ms.CurrentModelId {
			marker = "* "
		}
		label := m.Name
		if m.Description != nil {
			label = m.Name + ": " + *m.Description
		}
		sb.WriteString(fmt.Sprintf("  %smodel%d\t%s\n", marker, i+1, label))
	}
	c.appendLine(sb.String())
}

// modelNameByID returns the human-readable name for a model ID, falling back to the raw ID string.
// Must NOT be called with modelsMu held.
func (c *acmeClient) modelNameByID(id acp.ModelId) string {
	c.modelsMu.Lock()
	defer c.modelsMu.Unlock()
	if c.modelState != nil {
		for _, m := range c.modelState.AvailableModels {
			if m.ModelId == id {
				return m.Name
			}
		}
	}
	return string(id)
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
		if cmd.Input != nil && cmd.Input.UnstructuredCommandInput != nil {
			if hint := cmd.Input.UnstructuredCommandInput.Hint; hint != "" {
				line += " (" + hint + ")"
			}
		}
		sb.WriteString("  " + line + "\n")
	}
	c.appendLine(sb.String())
}

// modelByToken parses a "model<n>" token and returns the corresponding model ID.
func (c *acmeClient) modelByToken(word string) (acp.ModelId, bool) {
	var n int
	if _, err := fmt.Sscanf(word, "model%d", &n); err != nil {
		return "", false
	}
	c.modelsMu.Lock()
	defer c.modelsMu.Unlock()
	if c.modelState == nil || n < 1 || n > len(c.modelState.AvailableModels) {
		return "", false
	}
	return c.modelState.AvailableModels[n-1].ModelId, true
}

func (c *acmeClient) setModel(ctx context.Context, conn *acp.ClientSideConnection, id acp.ModelId) {
	if _, err := conn.SetSessionModel(ctx, acp.SetSessionModelRequest{
		SessionId: c.sessionID,
		ModelId:   id,
	}); err != nil {
		c.appendLine("[error: set model: " + err.Error() + "]\n")
		return
	}
	c.modelsMu.Lock()
	if c.modelState != nil {
		c.modelState.CurrentModelId = id
	}
	c.modelsMu.Unlock()
	c.appendLine("[model: " + c.modelNameByID(id) + "]\n")
}

// WriteTextFile writes to a file.
func (c *acmeClient) WriteTextFile(ctx context.Context, p acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if err := os.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, nil
}

// ReadTextFile reads a file.
func (c *acmeClient) ReadTextFile(ctx context.Context, p acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	return acp.ReadTextFileResponse{Content: string(data)}, nil
}
