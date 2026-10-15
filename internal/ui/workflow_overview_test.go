// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"strings"
	"testing"

	"github.com/tesserix/tesserix-crew/internal/core"
)

func TestWorkflowOverviewShowsGateAndAssignments(t *testing.T) {
	w := core.Workflow{Task: "Fix checkout", Current: 1, State: "awaiting-approval", Revision: 4,
		Recipe: core.Lifecycle{Stages: []core.Stage{
			{Name: "plan", Kind: "agent", Role: "planner", Agent: "claude", Model: "sonnet"},
			{Name: "review", Kind: "approval"},
			{Name: "implement", Kind: "agent", Agent: "codex"},
		}}, Results: []core.StageResult{{Stage: "plan", Attempt: 2, State: "pass", Output: "Plan ready\nLong details"}}}
	text := workflowOverviewText(w, nil)
	for _, want := range []string{"Current: review (2/3)", "Completed: 1/3", "Pending after current: 1", "Approval needed now", "planner · claude / sonnet", "implement · pending", "attempt 2", "Next: Review outputs"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	w.Current, w.State = 3, "completed"
	text = workflowOverviewText(w, nil)
	if strings.Contains(text, "Current:") || !strings.Contains(text, "Completed: 3/3") || !strings.Contains(text, "No remaining approval gates") {
		t.Fatal("completed workflow still has a current step or approval", text)
	}
}

func TestOverviewNavigationPreservesApprovalGate(t *testing.T) {
	m := guidedModel(t)
	w, err := m.runner.StartWorkflow(m.session.Repo, "Review checkout", "review")
	if err != nil {
		t.Fatal(err)
	}
	w.Current, w.State = 1, "awaiting-approval"
	w.Results = []core.StageResult{{Stage: "review", State: "pass", Output: "Detailed findings"}}
	if err := m.runner.Store.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	if err := m.switchSession(w.Session); err != nil {
		t.Fatal(err)
	}
	m.openWorkflowReview()
	m = key(m, "d")
	if m.panel.viewport.YOffset == 0 || !strings.Contains(m.panel.viewport.View(), "Detailed review") {
		t.Fatal("details shortcut did not navigate to review")
	}
	m = key(m, "o")
	if m.panel.viewport.YOffset != 0 {
		t.Fatal("overview shortcut did not return to top")
	}
	m = key(m, "f")
	current, err := m.runner.Store.Workflow(w.ID)
	if err != nil || current.State != "awaiting-approval" || m.panel.revision != current.Revision {
		t.Fatal("refresh changed approval state")
	}
}

func TestRoleOverviewReportsPinnedModelCapabilitiesAndPolicy(t *testing.T) {
	providers := core.BuiltinProviders()
	p := providers["claude"]
	p.DefaultModel = "pinned-default"
	providers["claude"] = p
	w := core.Workflow{State: "ready", Providers: providers, Recipe: core.Lifecycle{AllowSelfTest: true,
		Stages: []core.Stage{{Name: "plan", Kind: "agent", Role: "planner", Agent: "claude", Skills: []string{"crew-plan"}}}}}
	text := workflowOverviewText(w, nil)
	for _, want := range []string{"planner · claude / pinned-default", "Capabilities: read, write, tools", "Pinned skills: crew-plan", "independence: explicitly relaxed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing role detail %q in %s", want, text)
		}
	}
}
