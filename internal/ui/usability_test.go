// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSessionShortcutsPreserveHistoryAndPreferences(t *testing.T) {
	m := guidedModel(t)
	original := m.session.ID
	m.runner.Store.Append(original, "user", "claude", "Saved task")
	m.options.Model = "chosen-model"
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	if m.panel == nil || m.panel.kind != "sessions" || !strings.Contains(m.panel.picker.items[0].Label, "Current") {
		t.Fatal("session shortcut did not identify current session")
	}
	m = key(m, "esc")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = next.(model)
	if m.session.ID == original || m.options.Model != "chosen-model" || !m.options.Edits || m.lastTask != "" {
		t.Fatal("new session did not preserve preferences or reset task history")
	}
	if err := m.switchSession(original); err != nil {
		t.Fatal(err)
	}
	if m.lastTask != "Saved task" {
		t.Fatal("original session history was lost")
	}
}

func TestRetryRestoresTaskWithoutRunningIt(t *testing.T) {
	m := guidedModel(t)
	m.lastTask = "Fix checkout"
	m.status = "failed"
	m.input.SetValue("/retry")
	m = key(m, "enter")
	if m.busy || m.input.Value() != "Fix checkout" {
		t.Fatal("retry should restore editable input without executing")
	}
}

func TestRoutingReasonSurvivesSessionResume(t *testing.T) {
	m := guidedModel(t)
	if err := m.runner.Store.Append(m.session.ID, "routing", "claude", "configured default; local prerequisites verified"); err != nil {
		t.Fatal(err)
	}
	if err := m.switchSession(m.session.ID); err != nil {
		t.Fatal(err)
	}
	m.slash("/status")
	if !strings.Contains(strings.Join(m.lines, "\n"), "configured default; local prerequisites verified") {
		t.Fatal("resumed session hid the routing reason")
	}
}

func TestEscapeCancelsAndKeepsSessionOpen(t *testing.T) {
	m := guidedModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	m.busy, m.cancel = true, cancel
	id := m.session.ID
	m = key(m, "esc")
	if ctx.Err() == nil || m.status != "cancelling" || m.session.ID != id {
		t.Fatal("escape did not cancel in place")
	}
	next, _ := m.Update(finishMsg{err: context.Canceled})
	m = next.(model)
	if m.busy || m.status != "cancelled" || !m.input.Focused() {
		t.Fatal("cancelled task did not return to editable chat")
	}
}

func TestProgressDistinguishesStreamingCancellationAndSilence(t *testing.T) {
	m := guidedModel(t)
	m.busy, m.streaming = true, true
	m.turnStarted = time.Now().Add(-45 * time.Second)
	m.lastActivity = time.Now().Add(-35 * time.Second)
	text := m.progressText()
	if !strings.Contains(text, "Receiving response") || !strings.Contains(text, "no new output") {
		t.Fatal("missing response or silence state", text)
	}
	m.status = "cancelling"
	if !strings.HasPrefix(m.progressText(), "Cancelling") {
		t.Fatal("cancellation hidden by streaming state")
	}
}
