// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeGitHub struct {
	issues          []GitHubIssue
	creates, closes int
	lostResponse    bool
}

func (g *fakeGitHub) Find(ctx context.Context, repo, marker string) (GitHubIssue, bool, error) {
	for _, issue := range g.issues {
		if strings.Contains(issue.Body, marker) {
			return issue, true, nil
		}
	}
	return GitHubIssue{}, false, nil
}
func (g *fakeGitHub) Create(ctx context.Context, repo, title, body string) (GitHubIssue, error) {
	g.creates++
	issue := GitHubIssue{Number: g.creates, URL: fmt.Sprintf("https://github.com/%s/issues/%d", repo, g.creates), Body: body, State: "open"}
	g.issues = append(g.issues, issue)
	if g.lostResponse {
		g.lostResponse = false
		return GitHubIssue{}, fmt.Errorf("connection lost after creation")
	}
	return issue, nil
}
func (g *fakeGitHub) Close(ctx context.Context, repo string, number int, marker, body string) error {
	g.closes++
	for i := range g.issues {
		if g.issues[i].Number == number {
			g.issues[i].State = "closed"
		}
	}
	return nil
}
func githubWorkflow(t *testing.T) (*Store, Workflow) {
	t.Helper()
	s := workflowStore(t)
	recipe := BuiltinLifecycles()["default"]
	recipe.GitHubRepo = "org/repo"
	w, err := s.CreateWorkflow(t.TempDir(), "fix bug", "default", recipe)
	if err != nil {
		t.Fatal(err)
	}
	w.Current = 3
	w.State = "ready"
	w.Results = []StageResult{{Stage: "design", State: "pass"}, {Stage: "plan", State: "pass", Output: "```crew-issues\n[{\"title\":\"Fix bug\",\"body\":\"Bounded scope\",\"acceptance\":[\"Regression test passes\"]}]\n```"}, {Stage: "agent-review", State: "pass"}}
	if err = s.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	return s, w
}
func TestGitHubRetryFindsIssueAfterLostResponse(t *testing.T) {
	s, w := githubWorkflow(t)
	client := &fakeGitHub{lostResponse: true}
	w, err := s.RunGitHubStage(context.Background(), w.ID, client)
	if err == nil || w.State != "failed" || client.creates != 1 {
		t.Fatal(w, err)
	}
	w, err = s.DecideWorkflow(w.ID, "retry", "network recovered")
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.RunGitHubStage(context.Background(), w.ID, client)
	if err != nil || w.State != "awaiting-approval" || client.creates != 1 || len(w.Issues) != 1 {
		t.Fatal(w, err, client.creates)
	}
}
func TestGitHubClosureRequiresActualValidation(t *testing.T) {
	s, w := githubWorkflow(t)
	client := &fakeGitHub{}
	w, err := s.RunGitHubStage(context.Background(), w.ID, client)
	if err != nil {
		t.Fatal(err)
	}
	w.Current = 8
	w.State = "ready"
	s.SaveWorkflow(&w)
	if _, err = s.RunGitHubStage(context.Background(), w.ID, client); err == nil || client.closes != 0 {
		t.Fatal("unreviewed or untested issue closed")
	}
}
func TestInvalidIssuePlanCannotWrite(t *testing.T) {
	s, w := githubWorkflow(t)
	w.Results[1].Output = "freeform plan"
	s.SaveWorkflow(&w)
	client := &fakeGitHub{}
	if _, err := s.RunGitHubStage(context.Background(), w.ID, client); err == nil || client.creates != 0 {
		t.Fatal("untyped plan created issues")
	}
}

func TestFullDeliveryClosesOnlyValidatedTrackedIssue(t *testing.T) {
	s, w := githubWorkflow(t)
	w.Recipe.Checks = []Check{{Name: "unit", Kind: "unit", Command: []string{"sh", "-c", "test -f fixed"}}}
	if err := s.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	client := &fakeGitHub{}
	w, err := s.RunGitHubStage(context.Background(), w.ID, client)
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.DecideWorkflow(w.ID, "approve", "Reviewed scoped issue and checks")
	if err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, session Session, o RunOptions, emit func(Update)) (string, error) {
		current, _ := s.Workflow(w.ID)
		stage := current.Recipe.Stages[current.Current]
		if stage.Role == "implementer" {
			if _, err := s.RunCheck(ctx, w.ID, "unit", "red"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(session.Repo, "fixed"), []byte("fixed"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if stage.Role == "implementer" || stage.Role == "tester" {
			if _, err := s.RunCheck(ctx, w.ID, "unit", "green"); err != nil {
				t.Fatal(err)
			}
		}
		return "Validated acceptance criteria\nCREW_STAGE_RESULT: pass", nil
	}
	for w.Current < 8 {
		w, err = s.RunWorkflow(context.Background(), w.ID, true, run, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	w, err = s.RunGitHubStage(context.Background(), w.ID, client)
	if err != nil || w.State != "completed" || client.closes != 1 || !w.Issues[0].Closed {
		t.Fatal(w, err, client.closes)
	}
	// Completed state prevents replaying delivery side effects.
	if _, err := s.RunGitHubStage(context.Background(), w.ID, client); err == nil || client.closes != 1 {
		t.Fatal("closure replayed")
	}
}
