// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func evidenceWorkflow(t *testing.T) (*Store, Workflow) {
	t.Helper()
	s := workflowStore(t)
	repo := t.TempDir()
	recipe := testRecipe()
	recipe.TDD = true
	recipe.MaxRepairs = 1
	recipe.Checks = []Check{{Name: "unit", Kind: "unit", Command: []string{"sh", "-c", "test -f fixed"}, Timeout: "2s"}}
	w, err := s.CreateWorkflow(repo, "fix regression", "custom", recipe)
	if err != nil {
		t.Fatal(err)
	}
	w.Current = 2
	w.State = "ready"
	if err = s.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	return s, w
}
func TestExecutableRedGreenEvidenceRequired(t *testing.T) {
	s, w := evidenceWorkflow(t)
	run := func(ctx context.Context, session Session, o RunOptions, emit func(Update)) (string, error) {
		red, err := s.RunCheck(ctx, w.ID, "unit", "red")
		if err != nil || !red.Passed || red.ExitCode == 0 {
			t.Fatal(red, err)
		}
		if err := os.WriteFile(filepath.Join(session.Repo, "fixed"), []byte("fixed"), 0600); err != nil {
			t.Fatal(err)
		}
		green, err := s.RunCheck(ctx, w.ID, "unit", "green")
		if err != nil || !green.Passed {
			t.Fatal(green, err)
		}
		return "CREW_STAGE_RESULT: pass", nil
	}
	result, err := s.RunWorkflow(context.Background(), w.ID, true, run, nil)
	if err != nil || result.Current != 3 {
		t.Fatal(result, err)
	}
	evidence, err := s.Evidence(w.ID)
	if err != nil || len(evidence) != 2 || evidence[0].Revision != evidence[1].Revision {
		t.Fatal(evidence, err)
	}
}
func TestClaimedSuccessWithoutEvidenceBlocks(t *testing.T) {
	s, w := evidenceWorkflow(t)
	run := func(context.Context, Session, RunOptions, func(Update)) (string, error) {
		return "tests passed\nCREW_STAGE_RESULT: pass", nil
	}
	w, err := s.RunWorkflow(context.Background(), w.ID, true, run, nil)
	if err == nil || w.State != "blocked" || w.Current != 2 {
		t.Fatal(w, err)
	}
}
func TestLaterFailedVerificationCannotUseEarlierPass(t *testing.T) {
	s, w := evidenceWorkflow(t)
	run := func(ctx context.Context, session Session, o RunOptions, emit func(Update)) (string, error) {
		if _, err := s.RunCheck(ctx, w.ID, "unit", "red"); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(session.Repo, "fixed"), []byte("fixed"), 0600)
		if _, err := s.RunCheck(ctx, w.ID, "unit", "green"); err != nil {
			t.Fatal(err)
		}
		os.Remove(filepath.Join(session.Repo, "fixed"))
		if _, err := s.RunCheck(ctx, w.ID, "unit", "green"); err == nil {
			t.Fatal("regression should fail")
		}
		return "CREW_STAGE_RESULT: pass", nil
	}
	w, err := s.RunWorkflow(context.Background(), w.ID, true, run, nil)
	if err == nil || w.State != "blocked" {
		t.Fatal(w, err)
	}
}
func TestUnavailableCheckAndArtifacts(t *testing.T) {
	s, w := evidenceWorkflow(t)
	w.Recipe.Checks = []Check{{Name: "browser", Kind: "browser", Command: []string{"crew-intentionally-missing-browser"}, Artifacts: []string{"shot.png"}}}
	w.State = "running"
	s.SaveWorkflow(&w)
	evidence, err := s.RunCheck(context.Background(), w.ID, "browser", "green")
	if err == nil || evidence.Passed || evidence.ExitCode != -1 {
		t.Fatal(evidence, err)
	}
	w, _ = s.Workflow(w.ID)
	w.Recipe.Checks[0].Command = []string{"sh", "-c", "printf screenshot > shot.png"}
	s.SaveWorkflow(&w)
	evidence, err = s.RunCheck(context.Background(), w.ID, "browser", "green")
	if err != nil || len(evidence.Artifacts) != 1 {
		t.Fatal(evidence, err)
	}
	os.Remove(filepath.Join(w.Repo, "shot.png"))
	if _, err := os.ReadFile(evidence.Artifacts[0].Snapshot); err != nil {
		t.Fatal("evidence artifact not durable", err)
	}
	w, _ = s.Workflow(w.ID)
	w.Recipe.Checks[0].Command = []string{"true"}
	s.SaveWorkflow(&w)
	evidence, err = s.RunCheck(context.Background(), w.ID, "browser", "green")
	if err == nil || evidence.Passed {
		t.Fatal("missing required artifact passed", evidence, err)
	}
}
func TestCheckTimeoutAndBoundedRepair(t *testing.T) {
	s, w := evidenceWorkflow(t)
	w.State = "running"
	w.Recipe.Checks[0].Command = []string{"sh", "-c", "sleep 10"}
	w.Recipe.Checks[0].Timeout = "50ms"
	s.SaveWorkflow(&w)
	evidence, err := s.RunCheck(context.Background(), w.ID, "unit", "red")
	if err == nil || evidence.Passed || !strings.Contains(evidence.Output, "timed out") {
		t.Fatal(evidence, err)
	}
	w, _ = s.Workflow(w.ID)
	w.Current = 3
	w.State = "failed"
	s.SaveWorkflow(&w)
	w, err = s.RepairWorkflow(w.ID, "seeded regression reproduced")
	if err != nil || w.Current != 2 {
		t.Fatal(w, err)
	}
	w.Current = 3
	w.State = "failed"
	s.SaveWorkflow(&w)
	if _, err = s.RepairWorkflow(w.ID, "again"); err == nil {
		t.Fatal("unbounded repair accepted")
	}
}

func TestIndependentTesterCannotQuietlyChangeCode(t *testing.T) {
	s, w := evidenceWorkflow(t)
	w.Current = 3
	w.State = "ready"
	s.SaveWorkflow(&w)
	run := func(ctx context.Context, session Session, o RunOptions, emit func(Update)) (string, error) {
		os.WriteFile(filepath.Join(session.Repo, "fixed"), []byte("tester implemented fix"), 0600)
		s.RunCheck(ctx, w.ID, "unit", "green")
		return "CREW_STAGE_RESULT: pass", nil
	}
	w, err := s.RunWorkflow(context.Background(), w.ID, true, run, nil)
	if err == nil || w.State != "failed" {
		t.Fatal(w, err)
	}
}
