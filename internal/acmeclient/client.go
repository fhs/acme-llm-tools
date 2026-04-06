package acmeclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	acp "github.com/coder/acp-go-sdk"
	"9fans.net/go/acme"
)

type acmeClient struct {
	win       *acme.Win
	sessionID acp.SessionId
	writeMu   sync.Mutex
	promptMu  sync.Mutex
	inputBuf  []byte
	terminals terminalMap
}

// Run creates an acme window, spawns the agent, and runs the event loop.
func Run(ctx context.Context, agentArgs []string) error {
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
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}
	defer func() {
		_ = agentStdin.Close()
		_ = cmd.Wait()
	}()

	conn := acp.NewClientSideConnection(c, agentStdin, agentStdout)

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
				go c.prompt(ctx, conn, text)
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
			c.appendLine(content.Text.Text)
		}
	case u.AgentThoughtChunk != nil:
		// Thoughts: show abbreviated.
		content := u.AgentThoughtChunk.Content
		if content.Text != nil && content.Text.Text != "" {
			c.appendLine("[thought: " + truncate(content.Text.Text, 80) + "]\n")
		}
	case u.ToolCall != nil:
		tc := u.ToolCall
		c.appendLine("[tool: " + tc.Title + "]\n")
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// RequestPermission auto-approves all permission requests by selecting the first allow option.
func (c *acmeClient) RequestPermission(ctx context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	title := ""
	if p.ToolCall.Title != nil {
		title = *p.ToolCall.Title
	}
	c.appendLine("[permission: " + title + " — approved]\n")

	// Select the first "allow" option, or the first option if none match.
	var selected acp.PermissionOptionId
	for _, opt := range p.Options {
		if opt.Kind == acp.PermissionOptionKindAllowOnce || opt.Kind == acp.PermissionOptionKindAllowAlways {
			selected = opt.OptionId
			break
		}
	}
	if selected == "" && len(p.Options) > 0 {
		selected = p.Options[0].OptionId
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.NewRequestPermissionOutcomeSelected(selected),
	}, nil
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
