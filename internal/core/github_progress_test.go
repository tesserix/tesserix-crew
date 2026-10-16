// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type progressGitHub struct {
	fakeGitHub
	comments   map[string]string
	posts      int
	hidden     bool
	loseReply  bool
	beforePost func()
}

func (g *progressGitHub) HasComment(_ context.Context, _ string, _ int, marker string) (bool, error) {
	return !g.hidden && g.comments[marker] != "", nil
}
func (g *progressGitHub) PostComment(_ context.Context, _ string, _ int, marker, body string) error {
	if g.beforePost != nil {
		g.beforePost()
	}
	g.posts++
	if g.comments == nil {
		g.comments = map[string]string{}
	}
	g.comments[marker] = body
	if g.loseReply {
		return fmt.Errorf("lost comment response")
	}
	return nil
}
func progressWorkflow(t *testing.T) (*Store, Workflow) {
	t.Helper()
	s, w := githubWorkflow(t)
	w.Issues = []IssueLink{{Repository: "org/repo", Number: 42}}
	w.Results = append(w.Results, StageResult{Stage: "implement", Agent: "claude", State: "pass", Output: "implementation ready", Revision: 2})
	if err := s.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	return s, w
}
func TestProgressLostReplyNeverRepeatsWriteOrProvider(t *testing.T) {
	s, w := progressWorkflow(t)
	client := &progressGitHub{loseReply: true}
	client.beforePost = func() {
		saved, err := s.Workflow(w.ID)
		if err != nil || len(saved.GitHubUpdates) != 1 || !saved.GitHubUpdates[0].WriteStarted {
			t.Fatalf("intent not durable: %+v %v", saved, err)
		}
	}
	w, err := s.SyncGitHubProgress(context.Background(), w.ID, client)
	if err == nil || client.posts != 1 || PendingGitHubUpdates(w) != 1 {
		t.Fatal(w, err, client.posts)
	}
	client.hidden = true
	runs := 0
	_, err = s.AdvanceWorkflow(context.Background(), w.ID, true, true, func(context.Context, Session, RunOptions, func(Update)) (string, error) { runs++; return "", nil }, client, nil)
	if err == nil || runs != 0 || client.posts != 1 {
		t.Fatal("replayed pending work", err, runs, client.posts)
	}
	client.hidden = false
	for i := 0; i < 2; i++ {
		w, err = s.SyncGitHubProgress(context.Background(), w.ID, client)
		if err != nil || PendingGitHubUpdates(w) != 0 || client.posts != 1 {
			t.Fatal(w, err, client.posts)
		}
	}
	if w.GitHubUpdates[0].Body != "" {
		t.Fatal("confirmed body retained")
	}
}
func TestProgressFailuresAndDecisionsExcludePrivateFeedback(t *testing.T) {
	s, w := progressWorkflow(t)
	w.Results = []StageResult{
		{Stage: "test", Agent: "codex", State: "fail", Output: "Actual browser assertion failed", Revision: 3},
		{Stage: "user-review", Agent: "user", State: "approved", Output: "PRIVATE_USER_FEEDBACK", Revision: 4},
	}
	// Use pinned names explicitly, so this test covers the policy rather than preset spelling.
	w.Recipe.Stages = []Stage{{Name: "test", Kind: "agent", Role: "tester"}, {Name: "user-review", Kind: "approval"}}
	if err := s.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	client := &progressGitHub{}
	w, err := s.SyncGitHubProgress(context.Background(), w.ID, client)
	if err != nil || client.posts != 2 {
		t.Fatal(w, err, client.posts)
	}
	found := false
	for _, body := range client.comments {
		if strings.Contains(body, "PRIVATE_USER_FEEDBACK") {
			t.Fatal("private user note published")
		}
		found = found || strings.Contains(body, "Actual browser assertion failed")
	}
	if !found {
		t.Fatal("failure omitted")
	}
}
func TestProgressValidatesScopeAndOldPinnedRecipes(t *testing.T) {
	s, w := progressWorkflow(t)
	w.Issues[0].Repository = "other/repo"
	if err := s.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	client := &progressGitHub{}
	if _, err := s.SyncGitHubProgress(context.Background(), w.ID, client); err == nil || client.posts != 0 {
		t.Fatal("invalid scope posted")
	}
	w.Recipe.GitHubUpdates = false
	if err := s.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncGitHubProgress(context.Background(), w.ID, &fakeGitHub{}); err != nil {
		t.Fatal(err)
	}
}
func TestProgressBodyBoundsUTF8(t *testing.T) {
	body := progressBody(Workflow{ID: "run"}, StageResult{Output: strings.Repeat("界", 30000)}, nil)
	if len(body) > 55100 || !strings.Contains(body, "Truncated") {
		t.Fatal("unbounded report", len(body))
	}
}

func TestAdvanceWorkflowStopsAtGatesAndFailure(t *testing.T) {
	s := workflowStore(t)
	w, err := s.CreateWorkflow(t.TempDir(), "fixture", "custom", testRecipe())
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	run := func(context.Context, Session, RunOptions, func(Update)) (string, error) {
		runs++
		return "CREW_STAGE_RESULT: pass", nil
	}
	w, err = s.AdvanceWorkflow(context.Background(), w.ID, true, true, run, nil, nil)
	if err != nil || runs != 1 || w.State != "awaiting-approval" {
		t.Fatal(w, err, runs)
	}
	w, err = s.AdvanceWorkflow(context.Background(), w.ID, true, true, run, nil, nil)
	if err != nil || runs != 1 {
		t.Fatal("approval bypassed", err, runs)
	}
	w, err = s.DecideWorkflow(w.ID, "approve", "approved")
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.AdvanceWorkflow(context.Background(), w.ID, true, true, func(context.Context, Session, RunOptions, func(Update)) (string, error) {
		runs++
		return "CREW_STAGE_RESULT: fail", nil
	}, nil, nil)
	if err == nil || runs != 2 || w.State != "failed" {
		t.Fatal(w, err, runs)
	}
}
func TestAdvanceDoesNotRerunStageAfterProgressFailure(t *testing.T) {
	s := workflowStore(t)
	recipe := Lifecycle{GitHubRepo: "org/repo", GitHubUpdates: true, Stages: []Stage{{Name: "implement", Kind: "agent", Role: "implementer", Agent: "codex", Prompt: "Implement fixture"}, {Name: "review", Kind: "approval", Prompt: "Review fixture"}}}
	w, err := s.CreateWorkflow(t.TempDir(), "fixture", "custom", recipe)
	if err != nil {
		t.Fatal(err)
	}
	w.Issues = []IssueLink{{Repository: "org/repo", Number: 42}}
	if err = s.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	runs := 0
	run := func(context.Context, Session, RunOptions, func(Update)) (string, error) {
		runs++
		return "CREW_STAGE_RESULT: pass", nil
	}
	client := &progressGitHub{loseReply: true}
	w, err = s.AdvanceWorkflow(context.Background(), w.ID, true, true, run, client, nil)
	if err == nil || runs != 1 || w.State != "awaiting-approval" {
		t.Fatal(w, err, runs)
	}
	w, err = s.AdvanceWorkflow(context.Background(), w.ID, true, true, run, client, nil)
	if err != nil || runs != 1 || client.posts != 1 || PendingGitHubUpdates(w) != 0 {
		t.Fatal(w, err, runs, client.posts)
	}
}
