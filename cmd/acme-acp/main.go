package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fhs/acme-llm-tools/internal/acmeclient"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: acme-acp <agent> [args...]")
		os.Exit(1)
	}
	if err := acmeclient.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "acme-acp:", err)
		os.Exit(1)
	}
}
