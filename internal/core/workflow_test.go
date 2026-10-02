// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workflowStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func testRecipe() Lifecycle {
	return Lifecycle{Stages: []Stage{
		{Name: "plan", Kind: "agent", Role: "planner", Agent: "claude", Prompt: "Plan the change"},
		{Name: "review", Kind: "approval", Prompt: "Approve plan"},
		{Name: "implement", Kind: "agent", Role: "implementer", Agent: "codex", Edits: true, Prompt: "Implement"},
		{Name: "test", Kind: "agent", Role: "tester", Agent: "claude", Edits: true, Prompt: "Test independently"},
		{Name: "issues", Kind: "checkpoint", Prompt: "Record issue evidence"},
	}}
}
func TestWorkflowGateAndIndependentProviders(t *testing.T) {
	s := workflowStore(t)
	w, err := s.CreateWorkflow(t.TempDir(), "fix the bug", "custom", testRecipe())
	if err != nil {
		t.Fatal(err)
	}
	var seen []RunOptions
	run := func(ctx context.Context, session Session, o RunOptions, emit func(Update)) (string, error) {
		seen = append(seen, o)
		return "test command: go test ./...\nCREW_STAGE_RESULT: pass", nil
	}
	w, err = s.RunWorkflow(context.Background(), w.ID, false, run, nil)
	if err != nil || w.State != "awaiting-approval" {
		t.Fatal(w, err)
	}
	if _, err = s.RunWorkflow(context.Background(), w.ID, true, run, nil); err == nil || len(seen) != 1 {
		t.Fatal("approval gate bypassed")
	}
	if _, err = s.DecideWorkflow(w.ID, "record", "evidence"); err == nil {
		t.Fatal("record bypassed gate")
	}
	w, err = s.DecideWorkflow(w.ID, "approve", "approved scope")
	if err != nil || w.State != "ready" {
		t.Fatal(w, err)
	}
	if _, err = s.RunWorkflow(context.Background(), w.ID, false, run, nil); err == nil {
		t.Fatal("edit permissions enabled implicitly")
	}
	w, err = s.RunWorkflow(context.Background(), w.ID, true, run, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen[1].Task, "approved scope") || seen[1].Agent != "codex" || !seen[1].Edits {
		t.Fatal("approval and provider handoff missing", seen[1])
	}
	w, err = s.RunWorkflow(context.Background(), w.ID, true, run, nil)
	if err != nil || w.State != "awaiting-evidence" {
		t.Fatal(w, err)
	}
	if seen[2].Agent == seen[1].Agent {
		t.Fatal("tester isn't independent")
	}
	if _, err = s.DecideWorkflow(w.ID, "record", ""); err == nil {
		t.Fatal("empty evidence accepted")
	}
	w, err = s.DecideWorkflow(w.ID, "record", "https://github.com/example/repo/issues/1 — closed after tests")
	if err != nil || w.State != "completed" {
		t.Fatal(w, err)
	}
	if _, err = s.RunWorkflow(context.Background(), w.ID, true, run, nil); err == nil {
		t.Fatal("completed workflow replayed")
	}
}
func TestWorkflowVerdictAndFailureDoNotAdvance(t *testing.T) {
	cases := []struct {
		output string
		err    error
		state  string
	}{
		{"all done", nil, "blocked"},
		{"CREW_STAGE_RESULT: fail", nil, "failed"},
		{"CREW_STAGE_RESULT: blocked", nil, "blocked"},
		{"CREW_STAGE_RESULT: pass", errors.New("agent failed"), "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.state+tc.output, func(t *testing.T) {
			s := workflowStore(t)
			w, err := s.CreateWorkflow(t.TempDir(), "goal", "test", testRecipe())
			if err != nil {
				t.Fatal(err)
			}
			run := func(context.Context, Session, RunOptions, func(Update)) (string, error) { return tc.output, tc.err }
			w, _ = s.RunWorkflow(context.Background(), w.ID, false, run, nil)
			if w.State != tc.state || w.Current != 0 {
				t.Fatal(w)
			}
			w, err = s.DecideWorkflow(w.ID, "retry", "tools available now")
			if err != nil || w.State != "ready" || len(w.Results) != 2 {
				t.Fatal(w, err)
			}
		})
	}
}
func TestWorkflowPersistsRecipeAndRejectsStaleWrites(t *testing.T) {
	home := t.TempDir()
	s, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	recipe := testRecipe()
	w, err := s.CreateWorkflow(t.TempDir(), "goal", "test", recipe)
	if err != nil {
		t.Fatal(err)
	}
	recipe.Stages[0].Agent = "codex"
	a, err := s.Workflow(w.ID)
	if err != nil || a.Recipe.Stages[0].Agent != "claude" {
		t.Fatal(a, err)
	}
	b, err := s.Workflow(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.Task = "updated"
	if err = s.SaveWorkflow(&a); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveWorkflow(&b); err == nil {
		t.Fatal("stale writer accepted")
	}
	s.Close()
	s, err = OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	loaded, err := s.Workflow(w.ID)
	if err != nil || loaded.Task != "updated" || loaded.Revision != a.Revision {
		t.Fatal(loaded, err)
	}
}
func TestWorkflowExecutionLeaseAndRecovery(t *testing.T) {
	s := workflowStore(t)
	w, err := s.CreateWorkflow(t.TempDir(), "goal", "test", testRecipe())
	if err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, session Session, o RunOptions, emit func(Update)) (string, error) {
		if _, err := s.RunWorkflow(ctx, w.ID, false, nil, nil); err == nil {
			t.Error("duplicate stage accepted")
		}
		if _, err := s.RecoverWorkflow(w.ID, "recover while live"); err == nil {
			t.Error("live stage recovered")
		}
		return "CREW_STAGE_RESULT: pass", nil
	}
	if _, err = s.RunWorkflow(context.Background(), w.ID, false, run, nil); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash leaving durable state, with no live execution lease.
	crashed, err := s.CreateWorkflow(t.TempDir(), "crashed goal", "test", testRecipe())
	if err != nil {
		t.Fatal(err)
	}
	crashed.State = "running"
	if err = s.SaveWorkflow(&crashed); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.RecoverWorkflow(crashed.ID, "process exited; inspected working tree")
	if err != nil || recovered.State != "blocked" || recovered.Current != 0 {
		t.Fatal(recovered, err)
	}
	if _, err = s.RunWorkflow(context.Background(), crashed.ID, false, run, nil); err == nil {
		t.Fatal("recovery reran stage without explicit retry")
	}
}
func TestWorkflowRejectionReplaysReview(t *testing.T) {
	s := workflowStore(t)
	w, err := s.CreateWorkflow(t.TempDir(), "goal", "test", testRecipe())
	if err != nil {
		t.Fatal(err)
	}
	run := func(context.Context, Session, RunOptions, func(Update)) (string, error) {
		return "CREW_STAGE_RESULT: pass", nil
	}
	w, err = s.RunWorkflow(context.Background(), w.ID, false, run, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.DecideWorkflow(w.ID, "reject", "change acceptance criteria")
	if err != nil || w.State != "rejected" {
		t.Fatal(w, err)
	}
	if _, err = s.DecideWorkflow(w.ID, "approve", ""); err == nil {
		t.Fatal("rejection silently approved")
	}
	w, err = s.DecideWorkflow(w.ID, "retry", "replan with feedback")
	if err != nil || w.Current != 0 {
		t.Fatal(w, err)
	}
	w, err = s.RunWorkflow(context.Background(), w.ID, false, run, nil)
	if err != nil || w.State != "awaiting-approval" {
		t.Fatal(w, err)
	}
}
func TestLifecycleValidationAndConfigPrecedence(t *testing.T) {
	recipe := testRecipe()
	recipe.Stages[3].Agent = "codex"
	if recipe.Validate() == nil {
		t.Fatal("same implementer/tester accepted")
	}
	recipe = testRecipe()
	recipe.Stages[0].Edits = true
	if recipe.Validate() == nil {
		t.Fatal("edits before approval accepted")
	}
	recipe = testRecipe()
	recipe.Stages[0].Agent = "gemini"
	if recipe.Validate() == nil {
		t.Fatal("unverified provider accepted")
	}
	home, repo := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".crew"), 0700)
	for _, target := range []struct{ path, provider string }{{filepath.Join(home, "config.toml"), "claude"}, {filepath.Join(repo, ".crew", "config.toml"), "codex"}} {
		contents := "[lifecycles.audit]\ndescription='audit'\n[[lifecycles.audit.stages]]\nname='audit'\nkind='agent'\nrole='reviewer'\nagent='" + target.provider + "'\nprompt='Review the repo'\n"
		if err := os.WriteFile(target.path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config, err := LoadConfig(home, repo)
	if err != nil || config.Lifecycles["audit"].Stages[0].Agent != "codex" || len(config.Lifecycles["default"].Stages) != 9 {
		t.Fatal(config, err)
	}
}

func TestWorkflowThroughNativeRunner(t *testing.T) {
	s := workflowStore(t)
	home, repo, bin := t.TempDir(), t.TempDir(), t.TempDir()
	script := "#!/bin/sh\n/bin/cat >/dev/null\nprintf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"Reviewed repository.\\nCREW_STAGE_RESULT: pass\"}}'\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	w, err := s.CreateWorkflow(repo, "review code", "review", BuiltinLifecycles()["review"])
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: s, Home: home, Config: Config{DefaultAgent: "claude"}}
	w, err = s.RunWorkflow(context.Background(), w.ID, false, runner.Run, func(Update) {})
	if err != nil || w.State != "awaiting-approval" {
		t.Fatal(w, err)
	}
	history, err := s.ContextEvents(w.Session, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Kind != "user" || history[1].Kind != "assistant" || !strings.Contains(history[0].Content, "Role: reviewer") {
		t.Fatal(history)
	}
	w, err = s.DecideWorkflow(w.ID, "approve", "reviewed report")
	if err != nil || w.State != "completed" {
		t.Fatal(w, err)
	}
}

func TestWorkflowSummaryAndAmbiguousVerdict(t *testing.T) {
	s := workflowStore(t)
	w, err := s.CreateWorkflow(t.TempDir(), "goal", "test", testRecipe())
	if err != nil {
		t.Fatal(err)
	}
	state, stage, err := s.WorkflowStatus(w.ID)
	if err != nil || state != "ready" || stage != "plan" {
		t.Fatal(state, stage, err)
	}
	w.Current = len(w.Recipe.Stages)
	w.settle()
	if err = s.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	state, stage, err = s.WorkflowStatus(w.ID)
	if err != nil || state != "completed" || stage != "done" {
		t.Fatal(state, stage, err)
	}
	if StageVerdict("CREW_STAGE_RESULT: fail\nCREW_STAGE_RESULT: pass") != "blocked" {
		t.Fatal("ambiguous verdict accepted")
	}
}
