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
	resume := flag.String("resume", "", "resume an existing session by UUID")
	noFS := flag.Bool("no-fs", false, "disable ACP filesystem support")
	var configs []string
	flag.Func("config", "set session config option as `id=value` (may be repeated)", func(s string) error {
		configs = append(configs, s)
		return nil
	})
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: acme-acp [-rpc.trace] [-resume uuid] [-no-fs] [-config id=value] <agent> [args...]")
		os.Exit(1)
	}
	if err := acmeclient.Run(context.Background(), flag.Args(), *trace, *resume, *noFS, configs); err != nil {
		fmt.Fprintln(os.Stderr, "acme-acp:", err)
		os.Exit(1)
	}
}
