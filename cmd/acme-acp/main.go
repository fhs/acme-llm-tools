// Copyright © 2026 Fazlul Shahriar. All rights reserved.
// Use of this source code is governed by the MIT License
// that can be found in the LICENSE file.

//go:generate go test github.com/fhs/acme-llm-tools/cmd/acme-acp -v -run=^TestDocsUpToDate$ -fixdocs

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
		flag.Usage()
		os.Exit(2)
	}
	if err := acmeclient.Run(context.Background(), flag.Args(), *trace, *resume, *noFS, configs); err != nil {
		fmt.Fprintln(os.Stderr, "acme-acp:", err)
		os.Exit(1)
	}
}
