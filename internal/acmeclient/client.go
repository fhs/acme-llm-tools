package acmeclient

import (
	"context"
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
	permCh       chan string // non-nil while RequestPermission is waiting
	inThought    bool       // true while streaming a thought block
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

	// Create or resume a session.
	var sessID acp.SessionId
	if resume != "" {
		if !initResp.AgentCapabilities.LoadSession {
			return fmt.Errorf("agent does not support session resume")
		}
		sessID = acp.SessionId(resume)
		if _, err = conn.LoadSession(ctx, acp.LoadSessionRequest{
			SessionId:  sessID,
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		}); err != nil {
			return fmt.Errorf("ACP load session: %w", err)
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
	}
	c.sessionID = sessID
	c.agentName = agentName

	// Name the window and set up the tag now that we have the session ID.
	w.Name("+Acme/%s/%s", agentName, sessID)
	w.Write("tag", []byte("Prompt"))
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
			default:
				w.WriteEvent(e)
			}
		case e.C2 == 'l' || e.C2 == 'L':
			if !c.takePermInput(strings.TrimSpace(string(e.Text))) {
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
	pw.Name("+Acme/%s/%s/prompt", c.agentName, c.sessionID)
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

func (c *acmeClient) setPermCh(ch chan string) {
	c.permMu.Lock()
	defer c.permMu.Unlock()
	c.permCh = ch
}

// takePermInput delivers word to the pending permission channel if one exists.
// Returns true if consumed, false if the event should be forwarded to acme.
func (c *acmeClient) takePermInput(word string) bool {
	c.permMu.Lock()
	defer c.permMu.Unlock()
	if c.permCh == nil {
		return false
	}
	c.permCh <- word
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
		c.writeMu.Lock()
		if c.inThought {
			c.win.Write("body", []byte("]\n"))
			c.inThought = false
		}
		c.win.Write("body", []byte("[tool: "+tc.Title+"]\n"))
		c.writeMu.Unlock()
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
	for i, opt := range p.Options {
		sb.WriteString(fmt.Sprintf("  %d/ %s\n", i+1, opt.Name))
	}
	c.appendLine(sb.String())

	ch := make(chan string, 1)
	c.setPermCh(ch)
	defer c.setPermCh(nil)

	for {
		select {
		case <-ctx.Done():
			return acp.RequestPermissionResponse{
				Outcome: acp.NewRequestPermissionOutcomeCancelled(),
			}, nil
		case word := <-ch:
			n, err := strconv.Atoi(strings.TrimSpace(word))
			if err != nil || n < 1 || n > len(p.Options) {
				continue
			}
			c.appendLine(fmt.Sprintf("[permission: %q selected]\n", p.Options[n-1].Name))
			return acp.RequestPermissionResponse{
				Outcome: acp.NewRequestPermissionOutcomeSelected(p.Options[n-1].OptionId),
			}, nil
		}
	}
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
