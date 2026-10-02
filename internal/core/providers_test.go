// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testProvider(command string) Provider {
	return Provider{Kind: "cli", Command: []string{command}, Format: "crew", Auth: "api-key", KeyEnv: "CREW_TEST_KEY", ReadArgs: []string{"--read"}, EditArgs: []string{"--write"}, ModelArgs: []string{"--model", "{model}"}, ResumeArgs: []string{"--resume", "{session}"}, Capabilities: []string{"read", "write", "resume", "stream"}}
}
func TestPluginProviderAndSecretRedaction(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	command := filepath.Join(t.TempDir(), "agent")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"session\",\"session\":\"native\"}'\nprintf '%s\\n' '{\"type\":\"delta\",\"text\":\"result test-secret-value\"}'\nprintf '%s\\n' '{\"type\":\"done\"}'\n"
	if err := os.WriteFile(command, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREW_TEST_KEY", "test-secret-value")
	if err := AddProvider(home, repo, "custom", testProvider(command), false); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, err := store.Create(repo, "custom", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: store, Home: home, Config: config}
	result, err := runner.Run(context.Background(), s, RunOptions{Agent: "custom", Task: "inspect test-secret-value"}, func(Update) {})
	if err != nil || result != "result [redacted]" {
		t.Fatal(result, err)
	}
	events, err := store.Events(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if strings.Contains(e.Content, "test-secret-value") {
			t.Fatal("secret journaled", e.Kind)
		}
	}
	args, err := config.Command(AgentOptions{Agent: "custom", Native: "native", Model: "model-name", Repo: repo})
	if err != nil || !strings.Contains(strings.Join(args, " "), "--resume native") {
		t.Fatal(args, err)
	}
	if err := SetProviderDisabled(home, repo, "custom", false, true); err != nil {
		t.Fatal(err)
	}
	config, err = LoadConfig(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.Provider("custom"); err == nil {
		t.Fatal("disabled provider was selected")
	}
}
func TestProviderPrerequisitesFailBeforeJournal(t *testing.T) {
	home := t.TempDir()
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, _ := store.Create(t.TempDir(), "custom", "")
	config := Config{DefaultAgent: "custom", Providers: map[string]Provider{"custom": testProvider("sh")}}
	t.Setenv("CREW_TEST_KEY", "")
	runner := Runner{Store: store, Home: home, Config: config}
	if _, err := runner.Run(context.Background(), s, RunOptions{Task: "task"}, func(Update) {}); err == nil {
		t.Fatal("missing key accepted")
	}
	events, _ := store.Events(s.ID)
	if len(events) != 0 {
		t.Fatal("failed preflight journaled task")
	}
}
func TestProviderRegistryScopeAndCapabilityValidation(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	p := testProvider("sh")
	if err := AddProvider(home, repo, "custom", p, true); err != nil {
		t.Fatal(err)
	}
	p.Command = []string{"local-agent"}
	if err := AddProvider(home, repo, "custom", p, false); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(home, repo)
	if err != nil || config.Providers["custom"].Command[0] != "local-agent" {
		t.Fatal(config, err)
	}
	p.ReadArgs = nil
	if p.Validate("custom") == nil {
		t.Fatal("plugin without read-only contract accepted")
	}
	p = testProvider("sh")
	p.ModelArgs = []string{"{api_key}"}
	if p.Validate("custom") == nil {
		t.Fatal("secret placeholder accepted")
	}
	if err := SetProviderDisabled(home, repo, "codex", false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(home, repo); err != nil {
		t.Fatal("disabled stage provider should remain structurally valid", err)
	}
}
