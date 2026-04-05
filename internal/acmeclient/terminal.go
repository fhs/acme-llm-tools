package acmeclient

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

type terminal struct {
	cmd    *exec.Cmd
	outBuf *lockedBuf
}

type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type terminalMap struct {
	mu sync.Mutex
	m  map[string]*terminal
}

func (tm *terminalMap) store(id string, t *terminal) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.m == nil {
		tm.m = make(map[string]*terminal)
	}
	tm.m[id] = t
}

func (tm *terminalMap) load(id string) (*terminal, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t, ok := tm.m[id]
	return t, ok
}

func (tm *terminalMap) delete(id string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.m, id)
}

func randomID() string {
	return fmt.Sprintf("%016x", rand.Uint64())
}

func (c *acmeClient) CreateTerminal(ctx context.Context, p acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	id := randomID()
	buf := &lockedBuf{}
	cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)
	cmd.Stdout = buf
	cmd.Stderr = buf
	if err := cmd.Start(); err != nil {
		return acp.CreateTerminalResponse{}, err
	}
	c.terminals.store(id, &terminal{cmd: cmd, outBuf: buf})
	return acp.CreateTerminalResponse{TerminalId: id}, nil
}

func (c *acmeClient) TerminalOutput(ctx context.Context, p acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	t, ok := c.terminals.load(p.TerminalId)
	if !ok {
		return acp.TerminalOutputResponse{}, fmt.Errorf("terminal %s not found", p.TerminalId)
	}
	return acp.TerminalOutputResponse{Output: t.outBuf.String()}, nil
}

func (c *acmeClient) WaitForTerminalExit(ctx context.Context, p acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	t, ok := c.terminals.load(p.TerminalId)
	if !ok {
		return acp.WaitForTerminalExitResponse{}, fmt.Errorf("terminal %s not found", p.TerminalId)
	}
	err := t.cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return acp.WaitForTerminalExitResponse{}, err
		}
	}
	return acp.WaitForTerminalExitResponse{ExitCode: &exitCode}, nil
}

func (c *acmeClient) KillTerminalCommand(ctx context.Context, p acp.KillTerminalCommandRequest) (acp.KillTerminalCommandResponse, error) {
	t, ok := c.terminals.load(p.TerminalId)
	if !ok {
		return acp.KillTerminalCommandResponse{}, fmt.Errorf("terminal %s not found", p.TerminalId)
	}
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	return acp.KillTerminalCommandResponse{}, nil
}

func (c *acmeClient) ReleaseTerminal(ctx context.Context, p acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	c.terminals.delete(p.TerminalId)
	return acp.ReleaseTerminalResponse{}, nil
}
