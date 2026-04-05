package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fhs/acme-llm-tools/internal/acmetools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	s := acmetools.NewServer()
	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "acme-mcp: %v\n", err)
		os.Exit(1)
	}
}
