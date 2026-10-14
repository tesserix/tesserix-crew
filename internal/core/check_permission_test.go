// SPDX-License-Identifier: Apache-2.0
package core

import (
	"strings"
	"testing"
)

func TestCheckToolApprovalRequiresEditEnabledMCP(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		for _, edits := range []bool{false, true} {
			args, err := (Config{}).Command(AgentOptions{Agent: agent, Edits: edits, MCPPath: "fixture.json", Executable: "crew", Home: "/tmp/data", Session: "session"})
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, " ")
			permission := "mcp__crew__run_check"
			if agent == "codex" {
				permission = `mcp_servers.crew.tools.run_check.approval_mode="approve"`
			}
			if strings.Contains(joined, permission) != edits {
				t.Fatalf("%s edits=%v has wrong check permission: %s", agent, edits, joined)
			}
			if strings.Contains(joined, "tools.delegate.approval_mode") || strings.Contains(joined, "--dangerously") {
				t.Fatal("check permission expanded unrelated access")
			}
		}
	}
}
