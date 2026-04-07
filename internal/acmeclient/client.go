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
	win       *acme.Win
	sessionID acp.SessionId
	writeMu   sync.Mutex
	promptMu  sync.Mutex
	inputBuf  []byte
	terminals terminalMap
	permMu    sync.Mutex  // guards permCh
	permCh    chan string // non-nil while RequestPermission is waiting
	inThought bool        // true while streaming a thought block
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
func Run(ctx context.Context, agentArgs []string, trace bool) error {
	w, err := acme.New()
	if err != nil {
		return fmt.Errorf("open acme window: %w", err)
	}
	agentName := filepath.Base(agentArgs[0])
	w.Name("+Acme/AI")
	w.Ctl("clean")

	c := &acmeClient{win: w}
	c.appendLine("[acme-acp: connected to " + agentName + "]\n")

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
	_, err = conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
	})
	if err != nil {
		return fmt.Errorf("ACP initialize: %w", err)
	}
	sessResp, err := conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		return fmt.Errorf("ACP new session: %w", err)
	}
	c.sessionID = sessResp.SessionId

	// Event loop.
	for e := range w.EventChan() {
		if e == nil {
			break
		}
		switch {
		case e.C1 == 'K' && e.C2 == 'I':
			// Keyboard insert.
			c.inputBuf = append(c.inputBuf, e.Text...)
			if len(c.inputBuf) > 0 && c.inputBuf[len(c.inputBuf)-1] == '\n' {
				text := string(c.inputBuf[:len(c.inputBuf)-1])
				c.inputBuf = c.inputBuf[:0]
				if !c.takePermInput(text) {
					go c.prompt(ctx, conn, text)
				}
			}
		case e.C2 == 'x' || e.C2 == 'X':
			if string(e.Text) == "Del" {
				return nil
			}
			w.WriteEvent(e)
		case e.C2 == 'l' || e.C2 == 'L':
			w.WriteEvent(e)
		}
	}
	return nil
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

// takePermInput delivers line to the pending permission channel if one exists.
// Returns true if consumed, false if normal prompt dispatch should proceed.
func (c *acmeClient) takePermInput(line string) bool {
	c.permMu.Lock()
	defer c.permMu.Unlock()
	if c.permCh == nil {
		return false
	}
	c.permCh <- line
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
		c.closeThought()
		content := u.AgentMessageChunk.Content
		if content.Text != nil {
			c.appendLine(content.Text.Text)
		}
	case u.AgentThoughtChunk != nil:
		content := u.AgentThoughtChunk.Content
		if content.Text != nil && content.Text.Text != "" {
			if !c.inThought {
				c.appendLine("[thought: ")
				c.inThought = true
			}
			c.appendLine(content.Text.Text)
		}
	case u.ToolCall != nil:
		c.closeThought()
		tc := u.ToolCall
		c.appendLine("[tool: " + tc.Title + "]\n")
	}
	return nil
}

func (c *acmeClient) closeThought() {
	if c.inThought {
		c.appendLine("]\n")
		c.inThought = false
	}
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
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, opt.Name))
	}
	sb.WriteString("Enter number: ")
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
		case line := <-ch:
			n, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || n < 1 || n > len(p.Options) {
				c.appendLine(fmt.Sprintf("[invalid — enter 1–%d]: ", len(p.Options)))
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
