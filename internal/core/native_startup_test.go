// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCancelledStartupDoesNotPersistUncreatedNativeSession(t *testing.T) {
	bin, home, repo := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\ncat >/dev/null\necho '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"uncreated-session\"}'\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session, err := store.Create(repo, "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := Runner{Store: store, Home: home, Config: Config{DefaultAgent: "claude"}}
	_, err = runner.Run(ctx, session, RunOptions{Agent: "claude", Task: "Inspect this project"}, func(u Update) {
		if u.Text == "waiting for model" {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("expected cancelled startup")
	}
	native, err := store.Native(session.ID, "claude")
	if err != nil || native != "" {
		t.Fatal("uncreated native session persisted", native, err)
	}
}

func TestUnavailableClaudeResumeClearsIDWithoutRetrying(t *testing.T) {
	bin, home, repo := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\ncat >/dev/null\necho called >> calls\necho 'No conversation found with session ID: missing-id' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session, _ := store.Create(repo, "claude", "")
	store.SetNative(session.ID, "claude", "missing-id")
	runner := Runner{Store: store, Home: home, Config: Config{DefaultAgent: "claude"}}
	_, err = runner.Run(context.Background(), session, RunOptions{Agent: "claude", Task: "Continue reviewing"}, func(Update) {})
	if err == nil || !strings.Contains(err.Error(), "Retry explicitly") {
		t.Fatal("missing actionable recovery", err)
	}
	native, err := store.Native(session.ID, "claude")
	if err != nil || native != "" {
		t.Fatal("missing native conversation remained pinned", native, err)
	}
	calls, _ := os.ReadFile(filepath.Join(repo, "calls"))
	if string(calls) != "called\n" {
		t.Fatal("runner automatically retried a partially executed task")
	}
}
