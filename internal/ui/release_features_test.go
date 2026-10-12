// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"encoding/json"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tesserix/tesserix-crew/internal/core"
	"strings"
	"testing"
)

func TestSummaryUIRequiresReviewConfirmationAndRejectsChangedDraft(t *testing.T) {
	m := guidedModel(t)
	m.runner.Store.Append(m.session.ID, "user", "claude", "Keep stable API")
	m.runner.Store.DraftContext(m.session.ID, "Reviewed goal: preserve API")
	m.contextSlash([]string{"/context", "apply"})
	if m.panel == nil || m.panel.kind != "summary-review" {
		t.Fatal("review missing")
	}
	m = key(m, "enter")
	active, _ := m.runner.Store.ContextSummary(m.session.ID)
	if active.Applied || m.panel.kind != "summary-confirm" {
		t.Fatal("applied before confirmation")
	}
	m.runner.Store.DraftContext(m.session.ID, "Changed summary")
	m = key(m, "enter")
	active, _ = m.runner.Store.ContextSummary(m.session.ID)
	if active.Applied {
		t.Fatal("stale draft applied")
	}
	m.contextSlash([]string{"/context", "apply"})
	m = key(m, "enter")
	m = key(m, "enter")
	active, _ = m.runner.Store.ContextSummary(m.session.ID)
	if !active.Applied {
		t.Fatal("confirmed summary not applied")
	}
}
func TestNativeApprovalUICancellationAndAllowOnce(t *testing.T) {
	m := guidedModel(t)
	m.messages = make(chan tea.Msg, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.cancel = cancel
	m.busy = true
	reply := make(chan bool, 1)
	next, _ := m.Update(approvalMsg{request: core.ToolApproval{ID: "r", Tool: "Read", Input: json.RawMessage(`{"file_path":"note.txt"}`)}, reply: reply})
	m = next.(model)
	if !strings.Contains(m.View(), "allow once") {
		t.Fatal("decision controls hidden")
	}
	m = key(m, "a")
	if !<-reply || m.pendingApproval != nil {
		t.Fatal("allow not returned")
	}
	next, _ = m.Update(approvalMsg{request: core.ToolApproval{ID: "r2", Tool: "Read", Input: json.RawMessage(`{}`)}, reply: make(chan bool, 1)})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(model)
	if ctx.Err() == nil || m.pendingApproval != nil {
		t.Fatal("cancel left a waiting approval")
	}
}
func TestModelPickerIgnoresStaleProviderAndClearsDefault(t *testing.T) {
	m := guidedModel(t)
	m.options.Agent = "claude"
	m.options.Model = "old-model"
	m.receiveModels(modelsMsg{agent: "codex", choices: []core.ModelChoice{{ID: "other"}}})
	if m.panel != nil {
		t.Fatal("stale provider result opened picker")
	}
	m.receiveModels(modelsMsg{agent: "claude", choices: []core.ModelChoice{{ID: "sonnet", Label: "sonnet"}}})
	m = key(m, "enter")
	if m.options.Model != "" {
		t.Fatal("default model did not clear override")
	}
}
