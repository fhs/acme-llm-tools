package acmetools

import (
	"context"
	"fmt"

	"9fans.net/go/acme"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Input types ---

type listWindowsInput struct{}

type getBodyInput struct {
	WindowID int `json:"window_id" jsonschema:"acme window ID"`
}

type setBodyInput struct {
	WindowID int    `json:"window_id" jsonschema:"acme window ID"`
	Body     string `json:"body"      jsonschema:"new body text to set"`
}

type appendBodyInput struct {
	WindowID int    `json:"window_id" jsonschema:"acme window ID"`
	Text     string `json:"text"      jsonschema:"text to append"`
}

type windowIDInput struct {
	WindowID int `json:"window_id" jsonschema:"acme window ID"`
}

type setTagInput struct {
	WindowID int    `json:"window_id" jsonschema:"acme window ID"`
	Tag      string `json:"tag"       jsonschema:"tag text to write"`
}

type setAddrInput struct {
	WindowID int    `json:"window_id" jsonschema:"acme window ID"`
	Expr     string `json:"expr"      jsonschema:"acme address expression (e.g. \"1,2\", \"/foo/\", \"#0,$\")"`
}

type replaceTextInput struct {
	WindowID int    `json:"window_id" jsonschema:"acme window ID"`
	Expr     string `json:"expr"      jsonschema:"acme address expression selecting text to replace"`
	NewText  string `json:"new_text"  jsonschema:"replacement text"`
}

type createWindowInput struct {
	Name string `json:"name"           jsonschema:"window name (typically a file path)"`
	Body string `json:"body,omitempty" jsonschema:"optional initial body text"`
}

type deleteWindowInput struct {
	WindowID int  `json:"window_id" jsonschema:"acme window ID"`
	Force    bool `json:"force"     jsonschema:"if true, delete even if window is modified"`
}

type sendCtlInput struct {
	WindowID int    `json:"window_id" jsonschema:"acme window ID"`
	Command  string `json:"command"   jsonschema:"control command to send (e.g. show, clean, dirty, mark, nomark)"`
}

// --- Output types ---

type windowListItem struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Tag        string `json:"tag"`
	BodyLen    int    `json:"body_len"`
	IsDir      bool   `json:"is_dir"`
	IsModified bool   `json:"is_modified"`
}

type listWindowsOutput struct {
	Windows []windowListItem `json:"windows"`
}

type getBodyOutput struct {
	Body string `json:"body"`
}

type getTagOutput struct {
	Tag string `json:"tag"`
}

type addrOutput struct {
	Q0 int `json:"q0" jsonschema:"start rune offset"`
	Q1 int `json:"q1" jsonschema:"end rune offset"`
}

type getSelectionOutput struct {
	Text string `json:"text"`
	Q0   int    `json:"q0" jsonschema:"start rune offset"`
	Q1   int    `json:"q1" jsonschema:"end rune offset"`
}

type createWindowOutput struct {
	WindowID int `json:"window_id"`
}

type getWindowInfoOutput struct {
	ID         int    `json:"id"`
	TagLen     int    `json:"tag_len"`
	BodyLen    int    `json:"body_len"`
	IsDir      bool   `json:"is_dir"`
	IsModified bool   `json:"is_modified"`
	Width      int    `json:"width"`
	Font       string `json:"font"`
	TabWidth   int    `json:"tab_width"`
}

// --- Handlers ---

func handleListWindows(_ context.Context, _ *mcp.CallToolRequest, _ listWindowsInput) (*mcp.CallToolResult, listWindowsOutput, error) {
	wins, err := acme.Windows()
	if err != nil {
		return nil, listWindowsOutput{}, fmt.Errorf("list windows: %w", err)
	}
	items := make([]windowListItem, len(wins))
	for i, w := range wins {
		items[i] = windowListItem{
			ID:         w.ID,
			Name:       w.Name,
			Tag:        w.Tag,
			BodyLen:    w.BodyLen,
			IsDir:      w.IsDir,
			IsModified: w.IsModified,
		}
	}
	return nil, listWindowsOutput{Windows: items}, nil
}

func handleGetBody(_ context.Context, _ *mcp.CallToolRequest, args getBodyInput) (*mcp.CallToolResult, getBodyOutput, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, getBodyOutput{}, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	body, err := w.ReadAll("body")
	if err != nil {
		return nil, getBodyOutput{}, fmt.Errorf("read body: %w", err)
	}
	return nil, getBodyOutput{Body: string(body)}, nil
}

func handleSetBody(_ context.Context, _ *mcp.CallToolRequest, args setBodyInput) (*mcp.CallToolResult, any, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	if err := w.Addr(","); err != nil {
		return nil, nil, fmt.Errorf("set addr: %w", err)
	}
	if _, err := w.Write("data", []byte(args.Body)); err != nil {
		return nil, nil, fmt.Errorf("write data: %w", err)
	}
	return nil, nil, nil
}

func handleAppendBody(_ context.Context, _ *mcp.CallToolRequest, args appendBodyInput) (*mcp.CallToolResult, any, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	if _, err := w.Write("body", []byte(args.Text)); err != nil {
		return nil, nil, fmt.Errorf("write body: %w", err)
	}
	return nil, nil, nil
}

func handleGetTag(_ context.Context, _ *mcp.CallToolRequest, args windowIDInput) (*mcp.CallToolResult, getTagOutput, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, getTagOutput{}, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	tag, err := w.ReadAll("tag")
	if err != nil {
		return nil, getTagOutput{}, fmt.Errorf("read tag: %w", err)
	}
	return nil, getTagOutput{Tag: string(tag)}, nil
}

func handleSetTag(_ context.Context, _ *mcp.CallToolRequest, args setTagInput) (*mcp.CallToolResult, any, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	if _, err := w.Write("tag", []byte(args.Tag)); err != nil {
		return nil, nil, fmt.Errorf("write tag: %w", err)
	}
	return nil, nil, nil
}

func handleGetAddr(_ context.Context, _ *mcp.CallToolRequest, args windowIDInput) (*mcp.CallToolResult, addrOutput, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, addrOutput{}, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	// Open the addr file by doing an initial read, then sync addr <- dot.
	if _, _, err := w.ReadAddr(); err != nil {
		return nil, addrOutput{}, fmt.Errorf("open addr: %w", err)
	}
	if err := w.Ctl("addr=dot"); err != nil {
		return nil, addrOutput{}, fmt.Errorf("addr=dot: %w", err)
	}
	q0, q1, err := w.ReadAddr()
	if err != nil {
		return nil, addrOutput{}, fmt.Errorf("read addr: %w", err)
	}
	return nil, addrOutput{Q0: q0, Q1: q1}, nil
}

func handleSetAddr(_ context.Context, _ *mcp.CallToolRequest, args setAddrInput) (*mcp.CallToolResult, addrOutput, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, addrOutput{}, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	if _, err := w.Write("addr", []byte(args.Expr)); err != nil {
		return nil, addrOutput{}, fmt.Errorf("write addr: %w", err)
	}
	q0, q1, err := w.ReadAddr()
	if err != nil {
		return nil, addrOutput{}, fmt.Errorf("read addr: %w", err)
	}
	return nil, addrOutput{Q0: q0, Q1: q1}, nil
}

func handleGetSelection(_ context.Context, _ *mcp.CallToolRequest, args windowIDInput) (*mcp.CallToolResult, getSelectionOutput, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, getSelectionOutput{}, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	// Open the addr file by doing an initial read, then sync addr <- dot.
	if _, _, err := w.ReadAddr(); err != nil {
		return nil, getSelectionOutput{}, fmt.Errorf("open addr: %w", err)
	}
	if err := w.Ctl("addr=dot"); err != nil {
		return nil, getSelectionOutput{}, fmt.Errorf("addr=dot: %w", err)
	}
	q0, q1, err := w.ReadAddr()
	if err != nil {
		return nil, getSelectionOutput{}, fmt.Errorf("read addr: %w", err)
	}
	text, err := w.ReadAll("xdata")
	if err != nil {
		return nil, getSelectionOutput{}, fmt.Errorf("read xdata: %w", err)
	}
	return nil, getSelectionOutput{Text: string(text), Q0: q0, Q1: q1}, nil
}

func handleReplaceText(_ context.Context, _ *mcp.CallToolRequest, args replaceTextInput) (*mcp.CallToolResult, addrOutput, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, addrOutput{}, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	if _, err := w.Write("addr", []byte(args.Expr)); err != nil {
		return nil, addrOutput{}, fmt.Errorf("write addr: %w", err)
	}
	if _, err := w.Write("data", []byte(args.NewText)); err != nil {
		return nil, addrOutput{}, fmt.Errorf("write data: %w", err)
	}
	q0, q1, err := w.ReadAddr()
	if err != nil {
		return nil, addrOutput{}, fmt.Errorf("read addr: %w", err)
	}
	return nil, addrOutput{Q0: q0, Q1: q1}, nil
}

func handleCreateWindow(_ context.Context, _ *mcp.CallToolRequest, args createWindowInput) (*mcp.CallToolResult, createWindowOutput, error) {
	w, err := acme.New()
	if err != nil {
		return nil, createWindowOutput{}, fmt.Errorf("new window: %w", err)
	}
	if err := w.Name("%s", args.Name); err != nil {
		return nil, createWindowOutput{}, fmt.Errorf("set name: %w", err)
	}
	if args.Body != "" {
		if err := w.Addr(","); err != nil {
			return nil, createWindowOutput{}, fmt.Errorf("set addr: %w", err)
		}
		if _, err := w.Write("data", []byte(args.Body)); err != nil {
			return nil, createWindowOutput{}, fmt.Errorf("write body: %w", err)
		}
	}
	if err := w.Ctl("clean"); err != nil {
		return nil, createWindowOutput{}, fmt.Errorf("set clean: %w", err)
	}
	return nil, createWindowOutput{WindowID: w.ID()}, nil
}

func handleDeleteWindow(_ context.Context, _ *mcp.CallToolRequest, args deleteWindowInput) (*mcp.CallToolResult, any, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	if err := w.Del(args.Force); err != nil {
		return nil, nil, fmt.Errorf("delete window: %w", err)
	}
	return nil, nil, nil
}

func handleSendCtl(_ context.Context, _ *mcp.CallToolRequest, args sendCtlInput) (*mcp.CallToolResult, any, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	if err := w.Ctl("%s", args.Command); err != nil {
		return nil, nil, fmt.Errorf("send ctl: %w", err)
	}
	return nil, nil, nil
}

func handleGetWindowInfo(_ context.Context, _ *mcp.CallToolRequest, args windowIDInput) (*mcp.CallToolResult, getWindowInfoOutput, error) {
	w, err := acme.Open(args.WindowID, nil)
	if err != nil {
		return nil, getWindowInfoOutput{}, fmt.Errorf("open window %d: %w", args.WindowID, err)
	}
	info, err := w.Info()
	if err != nil {
		return nil, getWindowInfoOutput{}, fmt.Errorf("get info: %w", err)
	}
	out := getWindowInfoOutput{
		ID:         info.ID,
		TagLen:     info.TagLen,
		BodyLen:    info.BodyLen,
		IsDir:      info.IsDir,
		IsModified: info.IsModified,
	}
	if info.Size != nil {
		out.Width = info.Size.Width
		out.Font = info.Size.Font
		out.TabWidth = info.Size.TabWidth
	}
	return nil, out, nil
}
