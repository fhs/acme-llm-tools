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

var appFlags = flag.NewFlagSet("acme-acp", flag.ContinueOnError)

var (
	trace   = appFlags.Bool("rpc.trace", false, "print the ACP JSON-RPC trace to standard error")
	resume  = appFlags.String("resume", "", "resume an existing session by its `UUID` instead of creating a new one")
	noFS    = appFlags.Bool("no-fs", false, "disable ACP filesystem support;\nthe agent will not be able to read or write files through Acme")
	configs []string
)

func init() {
	appFlags.Func("config", "set a session configuration option at startup as `id=value`;\nmay be repeated; the option id and available values are those\nshown by the Config command", func(s string) error {
		configs = append(configs, s)
		return nil
	})
}

func main() {
	appFlags.Usage = func() {
		fmt.Fprint(os.Stderr, usageText())
	}
	if err := appFlags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if appFlags.NArg() < 1 {
		appFlags.Usage()
		os.Exit(2)
	}
	if err := acmeclient.Run(context.Background(), appFlags.Args(), *trace, *resume, *noFS, configs); err != nil {
		fmt.Fprintln(os.Stderr, "acme-acp:", err)
		os.Exit(1)
	}
}
