// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"github.com/tesserix/tesserix-crew/internal/core"
	"testing"
)

func TestInvalidCommands(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"run"}, {"skills", "add"}, {"skills", "unknown"}} {
		if err := run(context.Background(), args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestStreamingOutput(t *testing.T) {
	var out, diagnostics bytes.Buffer
	for _, update := range []core.Update{{Kind: "delta", Text: "Hel"}, {Kind: "delta", Text: "lo"}, {Kind: "text_end"}, {Kind: "status", Agent: "claude", Text: "thinking"}} {
		printUpdate(&out, &diagnostics, update)
	}
	if out.String() != "Hello\n" || diagnostics.String() != "[claude] thinking\n" {
		t.Fatal(out.String(), diagnostics.String())
	}
}

func TestWorkflowCommandRejectsInvalidUsage(t *testing.T) {
	for _, args := range [][]string{{"workflow"}, {"workflow", "unknown"}, {"workflow", "next"}, {"workflow", "start"}, {"workflow", "approve"}} {
		if err := run(context.Background(), args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
