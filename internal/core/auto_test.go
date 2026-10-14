// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func autoConfig(t *testing.T) Config {
	t.Helper()
	command := filepath.Join(t.TempDir(), "provider")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	p := Provider{Kind: "cli", Format: "crew", Command: []string{command}, Auth: "none", ReadArgs: []string{"--read"}, EditArgs: []string{"--write"}, Capabilities: []string{"read", "write"}}
	missing := p
	missing.Command = []string{filepath.Join(t.TempDir(), "missing")}
	return Config{DefaultAgent: "preferred", Providers: map[string]Provider{"preferred": missing, "backup": p}}
}

func TestAutoModeDoesNotRetryFailedExecutionOnAnotherProvider(t *testing.T) {
	c := autoConfig(t)
	marker := filepath.Join(t.TempDir(), "backup-ran")
	t.Setenv("CREW_AUTOTEST_BACKUP", marker)
	for name, body := range map[string]string{
		"preferred": "cat >/dev/null\nprintf '%s\\n' '{\"type\":\"error\",\"text\":\"execution failed after starting\"}'\nexit 1\n",
		"backup":    "cat >/dev/null\necho ran > \"$CREW_AUTOTEST_BACKUP\"\nprintf '%s\\n' '{\"type\":\"text\",\"text\":\"unexpected retry\"}'\n",
	} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
			t.Fatal(err)
		}
		p := c.Providers[name]
		p.Command = []string{path}
		c.Providers[name] = p
	}
	home, repo := t.TempDir(), t.TempDir()
	s, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	session, _ := s.Create(repo, "preferred", "")
	r := Runner{Store: s, Home: home, Config: c}
	if _, err := r.Run(context.Background(), session, RunOptions{Agent: "auto", Task: "Inspect this fixture"}, func(Update) {}); err == nil {
		t.Fatal("execution failure was hidden")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("auto mode retried a started task on another provider")
	}
}

func TestAutoRoutingSelectsEligibleProviderBeforeExecution(t *testing.T) {
	c := autoConfig(t)
	agent, reason, err := c.ResolveAgent("Fix the failing tests", AgentOptions{Agent: "auto", Edits: true})
	if err != nil || agent != "backup" || !strings.Contains(reason, "skipped preferred") {
		t.Fatal(agent, reason, err)
	}
	p := c.Providers["backup"]
	p.Capabilities = []string{"read"}
	p.EditArgs = nil
	c.Providers["backup"] = p
	if _, _, err = c.ResolveAgent("Fix tests", AgentOptions{Agent: "auto", Edits: true}); err == nil {
		t.Fatal("auto mode selected a provider without write capability")
	}
}

func TestAutoRoutingHonorsRulesAndExplicitSelections(t *testing.T) {
	c := autoConfig(t)
	c.DefaultAgent = "backup"
	c.Rules = []Rule{{Contains: []string{"review"}, Agent: "preferred"}, {Contains: []string{"review"}, Agent: "backup"}}
	agent, reason, err := c.ResolveAgent("Review this patch", AgentOptions{Agent: "auto"})
	if err != nil || agent != "backup" || !strings.HasPrefix(reason, "configured task rule") {
		t.Fatal(agent, reason, err)
	}
	if _, _, err = c.ResolveAgent("Review", AgentOptions{Agent: "preferred"}); err == nil {
		t.Fatal("explicit provider selection silently fell back")
	}
	if _, _, err = c.ResolveAgent("Review", AgentOptions{Agent: "auto", Model: "provider-specific"}); err == nil {
		t.Fatal("provider-specific model silently moved across providers")
	}
}
