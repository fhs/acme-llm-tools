package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/fhs/acme-llm-tools/internal/acmeclient"
)

func main() {
	trace := flag.Bool("rpc.trace", false, "print RPC trace to stderr")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: acme-acp [-rpc.trace] <agent> [args...]")
		os.Exit(1)
	}
	if err := acmeclient.Run(context.Background(), flag.Args(), *trace); err != nil {
		fmt.Fprintln(os.Stderr, "acme-acp:", err)
		os.Exit(1)
	}
}
