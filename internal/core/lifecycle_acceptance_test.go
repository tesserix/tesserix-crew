// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFullDefaultLifecycleResumesWithoutDuplicatingGitHubWrites(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	s, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	recipe := BuiltinLifecycles()["default"]
	recipe.GitHubRepo = "org/repo"
	recipe.Checks = []Check{{Name: "unit", Kind: "unit", Command: []string{"sh", "-c", "test -f fixed"}}}
	w, err := s.CreateWorkflow(repo, "Fix the fixture", "default", recipe)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	run := func(ctx context.Context, session Session, o RunOptions, emit func(Update)) (string, error) {
		current, err := s.Workflow(w.ID)
		if err != nil {
			return "", err
		}
		stage := current.Recipe.Stages[current.Current]
		seen[stage.Name]++
		if stage.Name == "plan" {
			return "```crew-issues\n[{\"title\":\"Fix fixture\",\"body\":\"Bounded fixture scope\",\"acceptance\":[\"Executable regression check passes\"]}]\n```\nCREW_STAGE_RESULT: pass", nil
		}
		if stage.Role == "implementer" {
			if _, err := s.RunCheck(ctx, w.ID, "unit", "red"); err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(repo, "fixed"), []byte("fixed"), 0600); err != nil {
				return "", err
			}
		}
		if stage.Role == "implementer" || stage.Role == "tester" {
			if _, err := s.RunCheck(ctx, w.ID, "unit", "green"); err != nil {
				return "", err
			}
		}
		return "Validated fixture acceptance\nCREW_STAGE_RESULT: pass", nil
	}
	client := &fakeGitHub{lostResponse: true}
	for w.State != "completed" {
		if w.State == "awaiting-approval" {
			if _, err := s.RunWorkflow(context.Background(), w.ID, true, run, nil); err == nil {
				t.Fatal("approval gate allowed implementation")
			}
			w, err = s.DecideWorkflow(w.ID, "approve", "Reviewed fixture issue and executable check")
		} else if stage := w.Recipe.Stages[w.Current]; stage.Kind == "github-create" || stage.Kind == "github-close" {
			w, err = s.RunGitHubStage(context.Background(), w.ID, client)
			if err != nil && w.State == "failed" && client.creates == 1 {
				w, err = s.DecideWorkflow(w.ID, "retry", "Creation response lost; reconcile existing marker")
			}
		} else {
			w, err = s.RunWorkflow(context.Background(), w.ID, true, run, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		if w.Current == 1 {
			s.Close()
			s, err = OpenStore(home)
			if err != nil {
				t.Fatal(err)
			}
			w, err = s.Workflow(w.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if client.creates != 1 || client.closes != 1 || len(w.Issues) != 1 || !w.Issues[0].Closed {
		t.Fatal("duplicate or incomplete GitHub writes", client.creates, client.closes, w.Issues)
	}
	for _, name := range []string{"design", "plan", "agent-review", "implement", "test", "deliver"} {
		if seen[name] != 1 {
			t.Fatal("agent stage lost or replayed", name, seen[name])
		}
	}
}
