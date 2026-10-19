// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tesserix/tesserix-crew/internal/core"
)

func guidedModel(t *testing.T) model {
	t.Helper()
	home, repo := t.TempDir(), t.TempDir()
	store, err := core.OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	session, err := store.Create(repo, "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	config, err := core.LoadConfig(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	m, err := newModel(context.Background(), &core.Runner{Store: store, Home: home, Config: config}, session, core.RunOptions{Agent: "claude", Edits: true})
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return next.(model)
}

func key(m model, name string) model {
	typeOf := tea.KeyRunes
	switch name {
	case "enter":
		typeOf = tea.KeyEnter
	case "esc":
		typeOf = tea.KeyEsc
	case "down":
		typeOf = tea.KeyDown
	}
	next, _ := m.Update(tea.KeyMsg{Type: typeOf, Runes: []rune(name)})
	return next.(model)
}

func TestSessionPickerSearchCancelAndCrossRepositoryResume(t *testing.T) {
	m := guidedModel(t)
	otherRepo := t.TempDir()
	core.SetDefaultProvider(m.runner.Home, otherRepo, "codex", false)
	other, _ := m.runner.Store.Create(otherRepo, "codex", "")
	m.runner.Store.Append(other.ID, "user", "codex", "Investigate checkout failure")
	m.runner.Store.Append(other.ID, "assistant", "codex", "Saved investigation")
	original := m.session.ID
	m.input.SetValue("/sessions all")
	m = key(m, "enter")
	if m.panel == nil || m.panel.kind != "sessions" {
		t.Fatal("slash command did not open picker")
	}
	m = key(m, "esc")
	if m.panel != nil || m.session.ID != original {
		t.Fatal("cancel switched session")
	}
	m.openSessions(true)
	m = key(m, "checkout")
	if len(m.panel.picker.filtered()) != 1 {
		t.Fatal("search did not filter")
	}
	m.options.Model = "old-model"
	m = key(m, "enter")
	if m.session.ID != other.ID || m.session.Repo != otherRepo || m.runner.Config.DefaultAgent != "codex" || m.options.Model != "" || !m.options.Edits {
		t.Fatal("session config or permissions were lost", m.session, m.options)
	}
	if !strings.Contains(strings.Join(m.lines, "\n"), "Saved investigation") {
		t.Fatal("history not loaded")
	}
}

func TestWorkflowWizardPinsRunWithoutExecutingStages(t *testing.T) {
	m := guidedModel(t)
	m.input.SetValue("/workflow start")
	m = key(m, "enter")
	if m.panel == nil || m.panel.kind != "presets" {
		t.Fatal("missing preset menu")
	}
	m = key(m, "review")
	m = key(m, "enter")
	if m.panel.kind != "task" || m.panel.preset != "review" {
		t.Fatal("missing task prompt")
	}
	m = key(m, "Review checkout")
	m = key(m, "enter")
	if m.panel == nil || m.panel.kind != "review" {
		t.Fatal("missing review screen", m.lines)
	}
	w, err := m.runner.Store.Workflow(m.session.ID)
	if err != nil || w.Task != "Review checkout" || w.State != "ready" || len(w.Results) != 0 {
		t.Fatal("wizard executed stage or lost task", w, err)
	}
	// Pinned workflows can be reopened despite a subsequently invalid project config.
	os.WriteFile(core.ConfigPath(m.runner.Home, m.session.Repo, false), []byte("invalid = ["), 0600)
	if err := m.switchSession(w.Session); err != nil {
		t.Fatal("pinned workflow depended on current config", err)
	}
}

func TestWorkflowApprovalRequiresConfirmationAndReviewedRevision(t *testing.T) {
	m := guidedModel(t)
	w, err := m.runner.StartWorkflow(m.session.Repo, "Review checkout", "review")
	if err != nil {
		t.Fatal(err)
	}
	w.Current, w.State = 1, "awaiting-approval"
	w.Results = []core.StageResult{{Stage: "review", State: "pass", Output: "All acceptance criteria reviewed"}}
	if err := m.runner.Store.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	m.switchSession(w.Session)
	m.openWorkflowReview()
	if !strings.Contains(m.panel.viewport.View(), "All acceptance criteria") && !strings.Contains(workflowReviewText(w, nil, "clean"), "All acceptance criteria") {
		t.Fatal("review output missing")
	}
	m = key(m, "a")
	current, _ := m.runner.Store.Workflow(w.ID)
	if m.panel.kind != "confirm" || current.State != "awaiting-approval" {
		t.Fatal("approval happened without confirmation")
	}
	m = key(m, "esc")
	current, _ = m.runner.Store.Workflow(w.ID)
	if current.State != "awaiting-approval" || m.panel.kind != "review" {
		t.Fatal("cancel approved gate")
	}
	m = key(m, "a")
	current.Results = append(current.Results, core.StageResult{Stage: "review", Output: "Changed after review"})
	m.runner.Store.SaveWorkflow(&current)
	m = key(m, "enter")
	current, _ = m.runner.Store.Workflow(w.ID)
	if current.State != "awaiting-approval" || !strings.Contains(strings.Join(m.lines, "\n"), "changed since review") {
		t.Fatal("stale view approved", current.State, m.lines)
	}
	m.openWorkflowReview()
	m = key(m, "a")
	m = key(m, "enter")
	current, _ = m.runner.Store.Workflow(w.ID)
	if current.State != "completed" {
		t.Fatal("confirmed approval not persisted", current.State)
	}
}

func TestGuidedPanelsFitSmallTerminals(t *testing.T) {
	m := guidedModel(t)
	m.createWorkflow("review", "Review checkout")
	for _, size := range [][2]int{{32, 12}, {80, 24}, {180, 40}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(model)
		p := newPicker("Choose a session", []Choice{{ID: "x", Label: strings.Repeat("long title ", 40), Detail: m.session.Repo}})
		p.width, p.height = size[0], size[1]
		p.input.Width = max(1, size[0]-8)
		for _, view := range []string{m.View(), p.View()} {
			if lipgloss.Height(view) > size[1] {
				t.Fatal("panel exceeded terminal height")
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) >= size[0] {
					t.Fatalf("panel overflows %d columns: %q", size[0], line)
				}
			}
		}
	}
}

func TestWorkflowProgressKeepsProviderForAgentlessStage(t *testing.T) {
	m := guidedModel(t)
	provider := m.session.Agent
	next, _ := m.Update(progressMsg(core.Update{Kind: "workflow", Text: "close-github-issues"}))
	m = next.(model)
	if m.session.Agent != provider || m.workflowStage != "close-github-issues" {
		t.Fatal(m.session.Agent, m.workflowStage)
	}
}
func TestWorkflowDecisionRefreshesReviewAfterMetadataSync(t *testing.T) {
	m := guidedModel(t)
	w, err := m.runner.StartWorkflow(m.session.Repo, "Review fixture", "review")
	if err != nil {
		t.Fatal(err)
	}
	m.session.ID = w.ID
	m.reopenWorkflowReview = true
	next, _ := m.Update(finishMsg{})
	m = next.(model)
	if m.panel == nil || m.panel.workflowID != w.ID || m.reopenWorkflowReview {
		t.Fatal("review not reopened")
	}
}
