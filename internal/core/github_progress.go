// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

type GitHubUpdate struct {
	Repository   string `json:"repository"`
	Number       int    `json:"number"`
	Stage        string `json:"stage"`
	Marker       string `json:"marker"`
	Body         string `json:"body,omitempty"`
	WriteStarted bool   `json:"write_started"`
	Confirmed    bool   `json:"confirmed"`
}

type GitHubProgress interface {
	HasComment(context.Context, string, int, string) (bool, error)
	PostComment(context.Context, string, int, string, string) error
}

func progressResult(w Workflow, result StageResult) bool {
	for _, stage := range w.Recipe.Stages {
		if stage.Name != result.Stage {
			continue
		}
		if stage.Kind == "approval" {
			return result.State == "approved" || result.State == "rejected"
		}
		if stage.Role != "implementer" && stage.Role != "tester" && stage.Role != "deliverer" {
			return false
		}
		return result.State == "pass" || result.State == "fail" || result.State == "blocked"
	}
	return false
}

func progressBody(w Workflow, result StageResult, evidence []Evidence) string {
	body := fmt.Sprintf("Workflow `%s` · stage `%s` · %s\nProvider: %s · stage revision %d\n", w.ID, result.Stage, result.State, result.Agent, result.Revision)
	if result.Agent != "user" {
		body += "\n" + result.Output + "\n"
	} else {
		body += "\nUser review decision recorded. Full feedback remains in the local journal.\n"
	}
	for _, item := range evidence {
		if item.Stage == result.Stage && item.Revision == result.Revision {
			body += fmt.Sprintf("\nCheck %s · %s · exit %d · passed=%v", item.Check, item.Phase, item.ExitCode, item.Passed)
		}
	}
	if len(body) > 55000 {
		body = body[:55000]
		for !utf8.ValidString(body) {
			body = body[:len(body)-1]
		}
		body += "\n[Truncated; full report remains in the Crew journal.]"
	}
	return body
}

// SyncGitHubProgress resumes only metadata writes; it never reexecutes a stage.
func (s *Store) SyncGitHubProgress(ctx context.Context, id string, client GitHub) (Workflow, error) {
	w, err := s.Workflow(id)
	if err != nil || !w.Recipe.GitHubUpdates || len(w.Issues) == 0 {
		return w, err
	}
	ctx, release, err := s.claimWorkflow(ctx, id)
	if err != nil {
		return w, err
	}
	defer release()
	w, err = s.Workflow(id)
	if err != nil {
		return w, err
	}
	progress, ok := client.(GitHubProgress)
	if !ok {
		return w, fmt.Errorf("GitHub client does not support progress updates")
	}
	if !githubSlug.MatchString(w.Recipe.GitHubRepo) {
		return w, fmt.Errorf("progress needs a pinned GitHub repository")
	}
	evidence, err := s.Evidence(id)
	if err != nil {
		return w, err
	}
	known := map[string]bool{}
	for _, update := range w.GitHubUpdates {
		known[fmt.Sprintf("%s/%d/%s", update.Repository, update.Number, update.Marker)] = true
	}
	added := false
	for _, result := range w.Results {
		if !progressResult(w, result) {
			continue
		}
		data, _ := json.Marshal(result)
		digest := sha256.Sum256(data)
		marker := "<!-- crew-progress:" + w.ID + ":" + hex.EncodeToString(digest[:]) + " -->"
		for _, issue := range w.Issues {
			if issue.CreationPending || issue.Number <= 0 {
				continue
			}
			if issue.Repository != w.Recipe.GitHubRepo {
				return w, fmt.Errorf("progress issue repository does not match pinned scope")
			}
			key := fmt.Sprintf("%s/%d/%s", issue.Repository, issue.Number, marker)
			if known[key] {
				continue
			}
			w.GitHubUpdates = append(w.GitHubUpdates, GitHubUpdate{Repository: issue.Repository, Number: issue.Number, Stage: result.Stage, Marker: marker, Body: progressBody(w, result, evidence)})
			known[key], added = true, true
		}
	}
	if added {
		if err := s.SaveWorkflow(&w); err != nil {
			return w, err
		}
	}
	for i := range w.GitHubUpdates {
		update := &w.GitHubUpdates[i]
		if update.Confirmed {
			continue
		}
		if update.Repository != w.Recipe.GitHubRepo || update.Number <= 0 {
			return w, fmt.Errorf("invalid GitHub progress identity")
		}
		exists, err := progress.HasComment(ctx, update.Repository, update.Number, update.Marker)
		if err != nil {
			return w, err
		}
		if !exists {
			if update.WriteStarted {
				return w, fmt.Errorf("GitHub status for %s on issue #%d is unconfirmed; reconcile the remote comment before retrying; no stage was rerun", update.Stage, update.Number)
			}
			update.WriteStarted = true
			if err := s.SaveWorkflow(&w); err != nil {
				return w, err
			}
			if err := progress.PostComment(ctx, update.Repository, update.Number, update.Marker, update.Body); err != nil {
				return w, err
			}
		}
		update.Confirmed, update.Body = true, ""
		if err := s.SaveWorkflow(&w); err != nil {
			return w, err
		}
	}
	return w, nil
}

// AdvanceWorkflow runs one stage or ready stages until an explicit gate/failure.
func (s *Store) AdvanceWorkflow(ctx context.Context, id string, edits, all bool, run WorkflowRunFunc, client GitHub, emit func(Update)) (Workflow, error) {
	w, err := s.Workflow(id)
	if err != nil {
		return w, err
	}
	for steps := 0; steps < len(w.Recipe.Stages); steps++ {
		if err := ctx.Err(); err != nil {
			return w, err
		}
		w, err = s.SyncGitHubProgress(ctx, id, client)
		if err != nil || w.State != "ready" {
			return w, err
		}
		if w.Current < 0 || w.Current >= len(w.Recipe.Stages) {
			return w, fmt.Errorf("invalid current workflow stage")
		}
		stage := w.Recipe.Stages[w.Current]
		if emit != nil {
			emit(Update{Kind: "workflow", Agent: stage.Agent, Text: stage.Name})
		}
		if stage.Kind == "github-create" || stage.Kind == "github-close" {
			w, err = s.RunGitHubStage(ctx, id, client)
		} else {
			w, err = s.RunWorkflow(ctx, id, edits, run, emit)
		}
		stageErr := err
		w, err = s.SyncGitHubProgress(ctx, id, client)
		if err = errors.Join(stageErr, err); err != nil || !all {
			return w, err
		}
	}
	return w, nil
}

func PendingGitHubUpdates(w Workflow) int {
	count := 0
	for _, update := range w.GitHubUpdates {
		if !update.Confirmed {
			count++
		}
	}
	return count
}
