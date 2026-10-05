// SPDX-License-Identifier: Apache-2.0
package core

import (
	"strings"
	"testing"
)

func TestGreetingToolsAreDisabledOnlyForSocialTurns(t *testing.T) {
	for _, task := range []string{"hello", "Hi!", " thank you. "} {
		if !isGreeting(task) {
			t.Fatal(task)
		}
	}
	for _, task := range []string{"hello, fix the checkout", "thanks, now run tests", "review this code", "hello.go"} {
		if isGreeting(task) {
			t.Fatal("coding request classified as greeting", task)
		}
	}
	args, err := AgentCommand(AgentOptions{Agent: "claude", Conversational: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i, arg := range args {
		if arg == "--tools" && i+1 < len(args) && args[i+1] == "" {
			found = true
		}
	}
	if !found || !strings.Contains(strings.Join(args, " "), "--strict-mcp-config") {
		t.Fatal(args)
	}
	args, err = AgentCommand(AgentOptions{Agent: "claude"})
	if err != nil || strings.Contains(strings.Join(args, " "), "--tools") {
		t.Fatal("coding tool access changed", args, err)
	}
}
