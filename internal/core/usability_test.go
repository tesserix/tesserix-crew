// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionSummariesFilterChildrenAndShowNextAction(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := t.TempDir()
	chat, _ := store.Create(repo, "codex", "")
	store.Append(chat.ID, "raw", "codex", "provider-internal-output")
	store.Append(chat.ID, "user", "codex", "Fix checkout")
	store.Create(repo, "claude", chat.ID)
	store.Create(t.TempDir(), "codex", "")
	w, err := store.CreateWorkflow(repo, "Review payment flow", "review", BuiltinLifecycles()["review"])
	if err != nil {
		t.Fatal(err)
	}
	w.Current = 1
	w.settle()
	if err := store.SaveWorkflow(&w); err != nil {
		t.Fatal(err)
	}
	rows, err := store.SessionSummaries(repo)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	for _, row := range rows {
		if row.ID == chat.ID && row.Task != "Fix checkout" {
			t.Fatal(row)
		}
		if row.ID == w.Session && (row.State != "awaiting-approval" || !strings.Contains(row.NextAction, "approve")) {
			t.Fatal(row)
		}
		if row.Parent != "" || strings.Contains(row.Task, "provider-internal") {
			t.Fatal("child or raw output included", row)
		}
	}
	all, err := store.SessionSummaries("")
	if err != nil || len(all) != 3 {
		t.Fatal(all, err)
	}
}

func TestReviewedRevisionCannotApproveChangedWorkflow(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	w, err := store.CreateWorkflow(t.TempDir(), "Review", "review", BuiltinLifecycles()["review"])
	if err != nil {
		t.Fatal(err)
	}
	w.Current = 1
	w.settle()
	store.SaveWorkflow(&w)
	reviewed := w.Revision
	w.record("note", "Plan changed", "user")
	store.SaveWorkflow(&w)
	if _, err := store.DecideWorkflowRevision(w.ID, "approve", "", reviewed); err == nil {
		t.Fatal("stale reviewed output approved")
	}
	current, _ := store.Workflow(w.ID)
	if current.State != "awaiting-approval" {
		t.Fatal("stale approval changed state", current.State)
	}
	if _, err := store.DecideWorkflowRevision(w.ID, "approve", "", current.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestSetupPreservesConfigAndRejectsUnverifiedProvider(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	path := ConfigPath(home, repo, false)
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("default_agent = 'claude'\ngithub_repo = 'org/project'\n[[rules]]\ncontains = ['review']\nagent = 'codex'\n"), 0600)
	if err := SetDefaultProvider(home, repo, "codex", false); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(home, repo)
	if err != nil || config.DefaultAgent != "codex" || config.GitHubRepo != "org/project" || len(config.Rules) != 1 {
		t.Fatal(config, err)
	}
	before, _ := os.ReadFile(path)
	if err := SetDefaultProvider(home, repo, "gemini", false); err == nil {
		t.Fatal("unverified provider accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("rejected setup modified config")
	}
}

func TestDoctorDoesNotRunTasksOrExposeLoginOutput(t *testing.T) {
	home, repo, bin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("PATH", bin)
	marker := filepath.Join(bin, "unexpected-task")
	// The fake native CLI only permits its login-status contract.
	body := "#!/bin/sh\nif [ \"$1 $2\" = 'auth status' ]; then\n echo '{\"loggedIn\":false,\"secret\":\"PRIVATE_TOKEN\"}'\n exit 0\nfi\n: > '" + marker + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	report := InspectSetup(context.Background(), home, repo, false)
	for _, item := range report.Checks {
		if item.Name == "login:claude" && item.State != "unverified" {
			t.Fatal("unprobed login claimed ready", item)
		}
	}
	report = InspectSetup(context.Background(), home, repo, true)
	found := false
	for _, item := range report.Checks {
		if item.Name == "login:claude" {
			found = true
			if item.State != "error" {
				t.Fatal("logged-out native status marked ready", item)
			}
		}
	}
	encoded, _ := json.Marshal(report)
	if !found || strings.Contains(string(encoded), "PRIVATE_TOKEN") {
		t.Fatal(string(encoded))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("doctor ran a task")
	}
	if _, err := os.Stat(home + "/sessions.db"); !os.IsNotExist(err) {
		t.Fatal("doctor created session data")
	}
}

func TestWorkflowStartPinsChecksAndRoleSkills(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example\n"), 0600)
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r := Runner{Store: store, Home: home}
	w, err := r.StartWorkflow(repo, "Fix regression", "bugfix")
	if err != nil {
		t.Fatal(err)
	}
	if w.State != "ready" || w.Current != 0 || len(w.Recipe.Checks) != 1 || w.Recipe.Checks[0].Command[0] != "go" {
		t.Fatal(w)
	}
	manifest, pinned, err := store.SkillManifest(w.Session)
	if err != nil || !pinned || len(manifest) == 0 {
		t.Fatal(manifest, pinned, err)
	}
	for _, stage := range w.Recipe.Stages {
		if stage.Kind == "agent" && len(stage.Skills) == 0 {
			t.Fatal("missing role skills", stage)
		}
	}
}
