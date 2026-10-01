// SPDX-License-Identifier: Apache-2.0
package core

import (
	"strings"
	"testing"
)

func TestIncrementalHandoff(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := "context-test"
	if err := store.Append(id, "user", "claude", "old question"); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(id, "assistant", "claude", "old answer"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkContext(id, "claude"); err != nil {
		t.Fatal(err)
	}
	after, err := store.ContextCursor(id, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(id, "raw", "codex", "large raw payload"); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(id, "assistant", "codex", "new cross-agent result"); err != nil {
		t.Fatal(err)
	}
	r := Runner{Store: store}
	s := Session{ID: id, Repo: t.TempDir()}
	incremental, err := r.contextSince(s, nil, after)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(incremental, "old answer") || strings.Contains(incremental, "large raw payload") || !strings.Contains(incremental, "new cross-agent result") {
		t.Fatal(incremental)
	}
	full, err := r.Context(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full, "old answer") || !strings.Contains(full, "new cross-agent result") {
		t.Fatal("full journal was lost")
	}
	untouched, err := store.ContextCursor(id, "codex")
	if err != nil || untouched != 0 {
		t.Fatal("new provider must receive full handoff")
	}
}
