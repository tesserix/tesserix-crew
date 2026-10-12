// SPDX-License-Identifier: Apache-2.0
package core

import (
	"fmt"
	"strings"
)

// SessionSummary leaves raw provider events out of the session browser.
type SessionSummary struct {
	Session
	Task       string `json:"task"`
	State      string `json:"state"`
	NextAction string `json:"next_action"`
}

func WorkflowNextAction(w Workflow) string {
	stage := ""
	if w.Current >= 0 && w.Current < len(w.Recipe.Stages) {
		stage = w.Recipe.Stages[w.Current].Name
	}
	return workflowNextAction(w.State, stage)
}

func workflowNextAction(state, stage string) string {
	switch state {
	case "ready":
		if stage != "" {
			return "Run " + stage
		}
	case "awaiting-approval":
		return "Review outputs, then approve or reject"
	case "awaiting-evidence":
		return "Record checkpoint evidence"
	case "failed", "blocked", "rejected":
		return "Inspect the failure and retry with feedback"
	case "running":
		return "Wait for the stage; recover only if its process has stopped"
	case "completed":
		return "Review the delivery report"
	}
	return "Inspect workflow status"
}

func (s *Store) SessionSummaries(repo string) ([]SessionSummary, error) {
	rows, err := s.db.Query(`SELECT s.id,s.repo,s.agent,s.parent,s.created,s.updated,
 COALESCE(json_extract(w.data,'$.task'),
 (SELECT substr(content,1,512) FROM events WHERE session=s.id AND kind='user' ORDER BY seq LIMIT 1),''),
 COALESCE(json_extract(w.data,'$.state'),'chat'),
 COALESCE(json_extract(w.data,'$.recipe.stages[' || json_extract(w.data,'$.current') || '].name'),'')
 FROM sessions s LEFT JOIN workflows w ON w.id=s.id
 WHERE s.parent='' AND (?='' OR s.repo=?) ORDER BY s.updated DESC`, repo, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionSummary{}
	for rows.Next() {
		var item SessionSummary
		var stage string
		if err := rows.Scan(&item.ID, &item.Repo, &item.Agent, &item.Parent, &item.Created, &item.Updated, &item.Task, &item.State, &stage); err != nil {
			return nil, err
		}
		item.NextAction = workflowNextAction(item.State, stage)
		if item.State == "chat" {
			item.NextAction = "Continue the conversation"
		}
		if strings.TrimSpace(item.Task) == "" {
			item.Task = "Untitled session"
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// StartWorkflow is shared by the CLI and terminal UI so both pin the same recipe,
// checks, providers and skill revisions. It does not execute a provider or check.
func (r *Runner) StartWorkflow(repo, task, preset string) (Workflow, error) {
	if strings.TrimSpace(task) == "" {
		return Workflow{}, fmt.Errorf("describe the workflow task")
	}
	config, err := LoadConfig(r.Home, repo)
	if err != nil {
		return Workflow{}, err
	}
	recipe, ok := config.Lifecycles[preset]
	if !ok {
		return Workflow{}, fmt.Errorf("unknown lifecycle %q; use crew workflow presets", preset)
	}
	if recipe.GitHubRepo == "" {
		recipe.GitHubRepo = config.GitHubRepo
	}
	if recipe.GitHubRepo == "" {
		recipe.GitHubRepo = InferGitHubRepo(repo)
	}
	if len(recipe.Checks) == 0 && recipe.TDD {
		recipe.Checks = config.Checks
		if len(recipe.Checks) == 0 {
			recipe.Checks = InferChecks(repo)
		}
	}
	active, err := DiscoverSkills(r.Home, repo)
	if err != nil {
		return Workflow{}, err
	}
	for i := range recipe.Stages {
		stage := &recipe.Stages[i]
		if stage.Role == "planner" {
			stage.Prompt += "\nInclude scoped issue proposals as a fenced crew-issues JSON array of objects with title, body and acceptance (array of strings). This typed block is required for GitHub creation. If no executable checks are pinned and TDD is required, also include a fenced crew-checks JSON array of check objects (name, kind, command array, optional timeout, optional artifact paths). These commands will be presented for user approval before execution."
		}
		if stage.Kind == "agent" && stage.Skills == nil {
			if name := BuiltinRoleSkill(stage.Role); name != "" {
				stage.Skills = SortedSkillNames(active)
				if _, exists := active[name]; !exists {
					stage.Skills = append(stage.Skills, name)
				}
			}
		}
	}
	w, err := r.Store.CreateWorkflowWithProviders(repo, task, preset, recipe, config.Providers)
	if err != nil {
		return w, err
	}
	session, err := r.Store.Get(w.Session)
	if err != nil {
		return w, err
	}
	return w, r.PinWorkflowSkills(session, recipe)
}
