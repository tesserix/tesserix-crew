// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setIssuePlan(t *testing.T, s *Store, w *Workflow, proposals []IssueProposal) {
	t.Helper()
	data, err := json.Marshal(proposals)
	if err != nil {
		t.Fatal(err)
	}
	w.Results[1].Output = "```crew-issues\n" + string(data) + "\n```"
	if err := s.SaveWorkflow(w); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubDependenciesLinkResolvedIssuesAndSession(t *testing.T) {
	s, w := githubWorkflow(t)
	setIssuePlan(t, s, &w, []IssueProposal{
		{Title: "Foundation", Body: "First scope", Acceptance: []string{"Base passes"}, Key: "base"},
		{Title: "Follow-up", Body: "Second scope", Acceptance: []string{"Dependent passes"}, Key: "followup", DependsOn: []string{"base"}},
	})
	client := &fakeGitHub{}
	w, err := s.RunGitHubStage(context.Background(), w.ID, client)
	if err != nil || len(w.Issues) != 2 {
		t.Fatal(w, err)
	}
	if !strings.Contains(client.issues[1].Body, "Dependencies:\n- https://github.com/org/repo/issues/1") ||
		!strings.Contains(client.issues[1].Body, "Crew session: `"+w.Session+"`") {
		t.Fatal("resolved dependency/session links missing", client.issues[1].Body)
	}
}

func TestInvalidGitHubDependenciesCannotWrite(t *testing.T) {
	for _, kind := range []string{"unknown", "forward", "duplicate-key", "duplicate-dependency"} {
		t.Run(kind, func(t *testing.T) {
			s, w := githubWorkflow(t)
			proposals := []IssueProposal{
				{Title: "First", Body: "First scope", Acceptance: []string{"Pass"}, Key: "first"},
				{Title: "Second", Body: "Second scope", Acceptance: []string{"Pass"}, Key: "second", DependsOn: []string{"first"}},
			}
			switch kind {
			case "unknown":
				proposals[1].DependsOn = []string{"unknown"}
			case "forward":
				proposals[0].DependsOn = []string{"second"}
			case "duplicate-key":
				proposals[1].Key = "first"
			case "duplicate-dependency":
				proposals[1].DependsOn = []string{"first", "first"}
			}
			setIssuePlan(t, s, &w, proposals)
			client := &fakeGitHub{}
			if _, err := s.RunGitHubStage(context.Background(), w.ID, client); err == nil || client.creates != 0 {
				t.Fatal("invalid dependency graph caused GitHub writes", err, client.creates)
			}
		})
	}
}

func TestPublishedScopeChangeBlocksBeforeFurtherGitHubWrites(t *testing.T) {
	s, w := githubWorkflow(t)
	client := &fakeGitHub{}
	w, err := s.RunGitHubStage(context.Background(), w.ID, client)
	if err != nil {
		t.Fatal(err)
	}
	w.Current, w.State = 3, "ready"
	setIssuePlan(t, s, &w, []IssueProposal{{Title: "Changed scope", Body: "Different work", Acceptance: []string{"New criterion"}}})
	if _, err = s.RunGitHubStage(context.Background(), w.ID, client); err == nil || !strings.Contains(err.Error(), "reconcile") || client.creates != 1 {
		t.Fatal("published scope changed without reconciliation", err, client.creates)
	}
}

type delayedVisibilityGitHub struct {
	fakeGitHub
	hidden int
}

func (g *delayedVisibilityGitHub) Find(ctx context.Context, repo, marker string) (GitHubIssue, bool, error) {
	if g.creates > 0 && g.hidden > 0 {
		g.hidden--
		return GitHubIssue{}, false, nil
	}
	return g.fakeGitHub.Find(ctx, repo, marker)
}

func TestLostCreationResponseAndStaleListingNeverDuplicateWrites(t *testing.T) {
	s, w := githubWorkflow(t)
	client := &delayedVisibilityGitHub{fakeGitHub: fakeGitHub{lostResponse: true}, hidden: 1}
	w, err := s.RunGitHubStage(context.Background(), w.ID, client)
	if err == nil || len(w.Issues) != 1 || !w.Issues[0].CreationPending || client.creates != 1 {
		t.Fatal("creation intent was not durable after lost response", w, err)
	}
	w, _ = s.DecideWorkflow(w.ID, "retry", "Check remote outcome")
	w, err = s.RunGitHubStage(context.Background(), w.ID, client)
	if err == nil || !strings.Contains(err.Error(), "unconfirmed") || client.creates != 1 {
		t.Fatal("stale listing caused a repeated create", err, client.creates)
	}
	w, _ = s.DecideWorkflow(w.ID, "retry", "Remote issue is now visible")
	w, err = s.RunGitHubStage(context.Background(), w.ID, client)
	if err != nil || w.State != "awaiting-approval" || w.Issues[0].CreationPending || client.creates != 1 {
		t.Fatal("confirmed outcome was not reconciled", w, err)
	}
}

func TestLegacyAmbiguousCreationRequiresReconciliation(t *testing.T) {
	s, w := githubWorkflow(t)
	w.Results = append(w.Results, StageResult{Stage: w.Recipe.Stages[w.Current].Name, State: "fail", Output: "Legacy connection lost"})
	s.SaveWorkflow(&w)
	client := &fakeGitHub{}
	if _, err := s.RunGitHubStage(context.Background(), w.ID, client); err == nil || client.creates != 0 {
		t.Fatal("legacy ambiguous outcome caused another write", err, client.creates)
	}
}

func TestLegacyIssueMarkersRemainStable(t *testing.T) {
	p := IssueProposal{Title: "Fix bug", Body: "Bounded scope", Acceptance: []string{"Regression test passes"}}
	want := "<!-- crew:run-id:ca877f4d798412c65b7a86c0c1518dbab9f8776165e44f6955c38a9efd46c6f9 -->"
	if got := issueMarker("run-id", p); got != want {
		t.Fatal("existing issue identity changed", got)
	}
}

type delayedClosureGitHub struct {
	fakeGitHub
	hidden int
}

func (g *delayedClosureGitHub) Close(ctx context.Context, repo string, number int, marker, body string) error {
	if err := g.fakeGitHub.Close(ctx, repo, number, marker, body); err != nil {
		return err
	}
	return fmt.Errorf("closure response lost")
}

func (g *delayedClosureGitHub) Find(ctx context.Context, repo, marker string) (GitHubIssue, bool, error) {
	issue, found, err := g.fakeGitHub.Find(ctx, repo, marker)
	if g.closes > 0 && g.hidden > 0 && found {
		g.hidden--
		issue.State = "open"
	}
	return issue, found, err
}

func TestLostClosureResponseAndStaleStateNeverRepeatWrites(t *testing.T) {
	client := &delayedClosureGitHub{hidden: 1}
	s, w := prepareVerifiedIssueClosure(t, client)
	w, err := s.RunGitHubStage(context.Background(), w.ID, client)
	if err == nil || !w.Issues[0].ClosurePending || client.closes != 1 {
		t.Fatal("uncertain closure intent was not retained", w, err)
	}
	w, _ = s.DecideWorkflow(w.ID, "retry", "Reconcile closure receipt")
	w, err = s.RunGitHubStage(context.Background(), w.ID, client)
	if err == nil || !strings.Contains(err.Error(), "unconfirmed") || client.closes != 1 {
		t.Fatal("stale state repeated closure/comment writes", err, client.closes)
	}
	w, _ = s.DecideWorkflow(w.ID, "retry", "Remote closure is now visible")
	w, err = s.RunGitHubStage(context.Background(), w.ID, client)
	if err != nil || w.State != "completed" || w.Issues[0].ClosurePending || client.closes != 1 {
		t.Fatal("closed receipt did not reconcile", w, err)
	}
}

func TestRepositoryChangeDuringRemoteLookupBlocksClosure(t *testing.T) {
	client := &fakeGitHub{}
	s, w := prepareVerifiedIssueClosure(t, client)
	client.onFind = func() {
		if err := os.WriteFile(filepath.Join(w.Repo, "fixed"), []byte("changed after lookup started"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RunGitHubStage(context.Background(), w.ID, client); err == nil || client.closes != 0 {
		t.Fatal("remote lookup window allowed stale evidence to close an issue", err, client.closes)
	}
}
