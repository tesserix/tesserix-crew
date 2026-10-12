// SPDX-License-Identifier: Apache-2.0
package core

import (
	"strings"
	"testing"
)

func TestReviewedSummaryPreservesJournalPinsAndNewTurns(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session, _ := store.Create(t.TempDir(), "codex", "")
	runner := Runner{Store: store}
	store.Append(session.ID, "user", "codex", strings.Repeat("OLD_HISTORY ", 24000))
	store.MarkContext(session.ID, "codex")
	if _, err := runner.Context(session, nil); err == nil {
		t.Fatal("oversize context accepted")
	}
	draft, err := store.DraftContext(session.ID, "Goal: fix checkout. Tests passed; rollback remains pending.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Context(session, nil); err == nil {
		t.Fatal("unreviewed draft silently compacted context")
	}
	if err := store.ApplyContextSummary(session.ID, draft.Digest()); err != nil {
		t.Fatal(err)
	}
	cursor, _ := store.ContextCursor(session.ID, "codex")
	if cursor != 0 {
		t.Fatal("native cursor not reset")
	}
	pin, err := store.PinContext(session.ID, "Keep the public API stable")
	if err != nil {
		t.Fatal(err)
	}
	store.Append(session.ID, "user", "claude", "New requirement: cover migration rollback")
	text, err := runner.Context(session, nil)
	if err != nil || strings.Contains(text, "OLD_HISTORY") || !strings.Contains(text, "rollback remains pending") || !strings.Contains(text, "Keep the public API stable") || !strings.Contains(text, "New requirement") {
		t.Fatal(text, err)
	}
	history, _ := store.Events(session.ID)
	if !strings.Contains(history[0].Content, "OLD_HISTORY") {
		t.Fatal("journal discarded")
	}
	search, _ := store.SearchContext(session.ID, "OLD_HISTORY", 1)
	if len(search) != 1 {
		t.Fatal("compacted history is not searchable")
	}
	store.UnpinContext(session.ID, pin)
	text, _ = runner.Context(session, nil)
	if strings.Contains(text, "public API stable") {
		t.Fatal("removed pin resent")
	}
	if err := store.ClearContextSummary(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Context(session, nil); err == nil {
		t.Fatal("clearing summary did not restore full history")
	}
}

func TestSummaryReplacementRequiresReviewedDigestAndLeavesActiveSummary(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	defer store.Close()
	session, _ := store.Create(t.TempDir(), "claude", "")
	store.Append(session.ID, "user", "claude", "Initial goal")
	first, _ := store.DraftContext(session.ID, "First reviewed summary")
	store.ApplyContextSummary(session.ID, first.Digest())
	draft, _ := store.DraftContext(session.ID, "Replacement draft")
	active, _ := store.ContextSummary(session.ID)
	if active.Content != first.Content || !active.Applied {
		t.Fatal("draft replaced active summary")
	}
	store.DraftContext(session.ID, "Changed draft")
	if err := store.ApplyContextSummary(session.ID, draft.Digest()); err == nil {
		t.Fatal("stale draft applied")
	}
	w, _ := store.CreateWorkflow(session.Repo, "Review", "review", BuiltinLifecycles()["review"])
	if _, err := store.DraftContext(w.Session, "Compacted workflow"); err == nil {
		t.Fatal("lifecycle context mutated")
	}
}

func TestImportRetainsDraftBoundaryWhenConversationContinues(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	defer store.Close()
	session, _ := store.Create(t.TempDir(), "claude", "")
	store.Append(session.ID, "user", "claude", "Original task")
	draft, _ := store.DraftContext(session.ID, "")
	store.Append(session.ID, "user", "codex", "New acceptance criterion while editing")
	edited, err := store.DraftContext(session.ID, "Reviewed original task")
	if err != nil || edited.Through != draft.Through {
		t.Fatal("import moved the boundary", edited, err)
	}
	store.ApplyContextSummary(session.ID, edited.Digest())
	text, err := (&Runner{Store: store}).Context(session, nil)
	if err != nil || !strings.Contains(text, "New acceptance criterion") {
		t.Fatal("new turn silently omitted", text, err)
	}
}

func TestContextChangesCannotRaceAnActiveProviderTurn(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	defer store.Close()
	session, _ := store.Create(t.TempDir(), "claude", "")
	if err := store.Acquire(session.Repo, "active-run"); err != nil {
		t.Fatal(err)
	}
	defer store.Release(session.Repo, "active-run")
	if _, err := store.PinContext(session.ID, "New decision"); err == nil {
		t.Fatal("pin changed during a turn")
	}
	if _, err := store.DraftContext(session.ID, "New summary"); err == nil {
		t.Fatal("draft changed during a turn")
	}
	if err := store.ApplyContextSummary(session.ID, ""); err == nil {
		t.Fatal("summary applied during a turn")
	}
}
