// Copyright © 2026 Fazlul Shahriar. All rights reserved.
// Use of this source code is governed by the MIT License
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"testing"

	"github.com/fhs/acme-llm-tools/internal/gendoc"
)

var fixDocs = flag.Bool("fixdocs", false, "if true, update doc.go")

func TestDocsUpToDate(t *testing.T) {
	if !*fixDocs {
		t.Parallel()
	}
	gendoc.Check(t, usageText(), *fixDocs, "go generate ./cmd/acme-mcp")
}
