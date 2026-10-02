// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Lifecycle recipes are copied into runs so configuration edits affect only new work.
type Lifecycle struct {
	Description string  `toml:"description" json:"description"`
	Stages      []Stage `toml:"stages" json:"stages"`
}
type Stage struct {
	Name   string `toml:"name" json:"name"`
	Kind   string `toml:"kind" json:"kind"` // agent, approval, checkpoint
	Role   string `toml:"role" json:"role,omitempty"`
	Agent  string `toml:"agent" json:"agent,omitempty"`
	Model  string `toml:"model" json:"model,omitempty"`
	Prompt string `toml:"prompt" json:"prompt,omitempty"`
	Edits  bool   `toml:"allow_edits" json:"allow_edits,omitempty"`
}
type StageResult struct {
	Stage   string `json:"stage"`
	Attempt int    `json:"attempt"`
	Agent   string `json:"agent,omitempty"`
	State   string `json:"state"`
	Output  string `json:"output"`
	Created string `json:"created"`
}
type Workflow struct {
	ID       string        `json:"id"`
	Session  string        `json:"session"`
	Repo     string        `json:"repo"`
	Task     string        `json:"task"`
	Preset   string        `json:"preset"`
	Recipe   Lifecycle     `json:"recipe"`
	Current  int           `json:"current"`
	State    string        `json:"state"`
	Results  []StageResult `json:"results"`
	Revision int           `json:"revision"`
	Updated  string        `json:"updated"`
}

func BuiltinLifecycles() map[string]Lifecycle {
	return map[string]Lifecycle{
		"default": {Description: "Reviewed delivery with independent testing and GitHub checkpoints", Stages: []Stage{
			{Name: "design", Kind: "agent", Role: "designer", Agent: "claude", Prompt: "Clarify the goal, constraints and acceptance criteria. Compare design options; recommend a concrete design. Do not implement."},
			{Name: "plan", Kind: "agent", Role: "planner", Agent: "claude", Prompt: "Create an implementation plan and proposed GitHub issue scope with acceptance criteria and a test strategy. Do not implement."},
			{Name: "agent-review", Kind: "agent", Role: "reviewer", Agent: "codex", Prompt: "Independently review the design and plan. Check feasibility, scope, risks and test coverage. Fail if material problems remain; give actionable feedback. Do not implement."},
			{Name: "create-github-issues", Kind: "checkpoint", Prompt: "Create the scoped GitHub issues using the reviewed plan, then record their URLs here. This version does not create issues automatically."},
			{Name: "user-review", Kind: "approval", Prompt: "Review the design, plan, agent review and issue scope. Explicitly approve before implementation, or reject with feedback."},
			{Name: "implement", Kind: "agent", Role: "implementer", Agent: "codex", Edits: true, Prompt: "Implement only the approved scope using TDD: add a meaningful failing test, implement, then run the relevant tests. Record commands, outcomes and changed files. Do not publish, merge or close issues."},
			{Name: "test", Kind: "agent", Role: "tester", Agent: "claude", Edits: true, Prompt: "Independently validate the implementation against the acceptance criteria. Run relevant tests, including E2E or browser testing when applicable and available. Record exact commands, results and limitations. Fail or block if required validation cannot be completed. Do not fix production code or publish."},
			{Name: "deliver", Kind: "agent", Role: "deliverer", Agent: "claude", Prompt: "Prepare the delivery report from implementation and independent testing evidence: changed behavior, checks performed, remaining limitations, and issue closure evidence. Do not publish, merge, or close issues."},
			{Name: "close-github-issues", Kind: "checkpoint", Prompt: "Review the delivery evidence. Close the completed GitHub issues yourself, then record the issue URLs and closure evidence. This version does not close issues automatically."},
		}},
		"review": {Description: "Read-only repository review", Stages: []Stage{
			{Name: "review", Kind: "agent", Role: "reviewer", Agent: "codex", Prompt: "Review the requested scope and report concrete findings with file references. Do not change files."},
			{Name: "user-review", Kind: "approval", Prompt: "Review the findings and approve the completed report or reject with feedback."},
		}},
	}
}
func (l Lifecycle) Validate() error {
	if len(l.Stages) == 0 {
		return fmt.Errorf("lifecycle needs at least one stage")
	}
	implementers := map[string]bool{}
	for _, stage := range l.Stages {
		if stage.Kind == "agent" && stage.Role == "implementer" {
			implementers[stage.Agent] = true
		}
	}
	seen := map[string]bool{}
	gate := false
	for _, s := range l.Stages {
		if strings.TrimSpace(s.Name) == "" || seen[s.Name] {
			return fmt.Errorf("stage names must be nonempty and unique: %q", s.Name)
		}
		seen[s.Name] = true
		if strings.TrimSpace(s.Prompt) == "" {
			return fmt.Errorf("stage %s needs a prompt", s.Name)
		}
		switch s.Kind {
		case "approval", "checkpoint":
			if s.Agent != "" || s.Model != "" || s.Edits {
				return fmt.Errorf("manual stage %s cannot select an agent or edit files", s.Name)
			}
			if s.Kind == "approval" {
				gate = true
			}
		case "agent":
			if _, err := AgentCommand(AgentOptions{Agent: s.Agent, Model: s.Model, Edits: s.Edits}); err != nil {
				return fmt.Errorf("stage %s: %w", s.Name, err)
			}
			if strings.TrimSpace(s.Role) == "" {
				return fmt.Errorf("agent stage %s needs a role", s.Name)
			}
			if s.Edits && !gate {
				return fmt.Errorf("stage %s enables edits before a user approval gate", s.Name)
			}
			if s.Role == "tester" && implementers[s.Agent] {
				return fmt.Errorf("tester must use a different provider from implementer")
			}
		default:
			return fmt.Errorf("stage %s has unknown kind %q", s.Name, s.Kind)
		}
	}
	return nil
}
func (w *Workflow) settle() {
	if w.Current >= len(w.Recipe.Stages) {
		w.State = "completed"
		return
	}
	switch w.Recipe.Stages[w.Current].Kind {
	case "approval":
		w.State = "awaiting-approval"
	case "checkpoint":
		w.State = "awaiting-evidence"
	default:
		w.State = "ready"
	}
}
func (s *Store) CreateWorkflow(repo, task, preset string, recipe Lifecycle) (Workflow, error) {
	if strings.TrimSpace(task) == "" {
		return Workflow{}, fmt.Errorf("workflow task is empty")
	}
	if err := recipe.Validate(); err != nil {
		return Workflow{}, err
	}
	agent := "claude"
	for _, stage := range recipe.Stages {
		if stage.Kind == "agent" {
			agent = stage.Agent
			break
		}
	}
	session, err := s.Create(repo, agent, "")
	if err != nil {
		return Workflow{}, err
	}
	w := Workflow{ID: session.ID, Session: session.ID, Repo: repo, Task: task, Preset: preset, Recipe: recipe, Updated: stamp()}
	w.settle()
	data, err := json.Marshal(w)
	if err != nil {
		return Workflow{}, err
	}
	_, err = s.db.Exec("INSERT INTO workflows(id,session,revision,data) VALUES(?,?,?,?)", w.ID, w.Session, w.Revision, data)
	return w, err
}
func (s *Store) Workflow(id string) (Workflow, error) {
	var data []byte
	if err := s.db.QueryRow("SELECT data FROM workflows WHERE id=?", id).Scan(&data); err != nil {
		return Workflow{}, fmt.Errorf("load workflow %q: %w", id, err)
	}
	var w Workflow
	err := json.Unmarshal(data, &w)
	return w, err
}
func (s *Store) SaveWorkflow(w *Workflow) error {
	revision := w.Revision
	next := *w
	next.Revision++
	next.Updated = stamp()
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	res, err := s.db.Exec("UPDATE workflows SET revision=?,data=? WHERE id=? AND revision=?", next.Revision, data, w.ID, revision)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("workflow changed in another process; reload before continuing")
	}
	*w = next
	return nil
}
func (s *Store) Workflows(repo string) ([]Workflow, error) {
	rows, err := s.db.Query("SELECT data FROM workflows ORDER BY rowid DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workflow
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var w Workflow
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if repo == "" || w.Repo == repo {
			out = append(out, w)
		}
	}
	return out, rows.Err()
}
func (w *Workflow) record(state, output, agent string) {
	stage := w.Recipe.Stages[w.Current]
	attempt := 1
	for _, r := range w.Results {
		if r.Stage == stage.Name {
			attempt++
		}
	}
	w.Results = append(w.Results, StageResult{Stage: stage.Name, Attempt: attempt, Agent: agent, State: state, Output: output, Created: stamp()})
}
func (s *Store) DecideWorkflow(id, action, note string) (Workflow, error) {
	w, err := s.Workflow(id)
	if err != nil {
		return w, err
	}
	if w.State == "completed" || w.State == "running" {
		return w, fmt.Errorf("cannot %s a %s workflow", action, w.State)
	}
	switch action {
	case "approve":
		if w.State != "awaiting-approval" {
			return w, fmt.Errorf("workflow is not awaiting approval")
		}
		w.record("approved", note, "user")
		w.Current++
		w.settle()
	case "record":
		if w.State != "awaiting-evidence" || strings.TrimSpace(note) == "" {
			return w, fmt.Errorf("record needs a pending checkpoint and nonempty evidence")
		}
		w.record("recorded", note, "user")
		w.Current++
		w.settle()
	case "reject":
		if w.State != "awaiting-approval" || strings.TrimSpace(note) == "" {
			return w, fmt.Errorf("reject needs a pending approval and feedback")
		}
		w.record("rejected", note, "user")
		w.State = "rejected"
	case "retry":
		if w.State != "failed" && w.State != "blocked" && w.State != "rejected" {
			return w, fmt.Errorf("retry requires a failed, blocked or rejected stage")
		}
		w.record("retry-requested", note, "user")
		if w.State == "rejected" { // Re-plan; replay downstream checkpoints and approvals.
			for w.Current > 0 {
				w.Current--
				if w.Recipe.Stages[w.Current].Kind == "agent" {
					break
				}
			}
		}
		w.settle()
	default:
		return w, fmt.Errorf("unknown workflow decision %q", action)
	}
	err = s.SaveWorkflow(&w)
	return w, err
}

type WorkflowRunFunc func(context.Context, Session, RunOptions, func(Update)) (string, error)

func StageVerdict(output string) string {
	verdict := "blocked"
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "CREW_STAGE_RESULT:") {
			count++
			v := strings.TrimSpace(strings.TrimPrefix(line, "CREW_STAGE_RESULT:"))
			switch v {
			case "pass", "fail", "blocked":
				verdict = v
			default:
				verdict = "blocked"
			}
		}
	}
	if count != 1 {
		return "blocked"
	}
	return verdict
}

// Status polling should not load every historical stage output.
func (s *Store) WorkflowStatus(id string) (state, stage string, err error) {
	err = s.db.QueryRow(`SELECT json_extract(data,'$.state'),
	 COALESCE(json_extract(data,'$.recipe.stages[' || json_extract(data,'$.current') || '].name'),'done')
	 FROM workflows WHERE id=?`, id).Scan(&state, &stage)
	return
}
func (s *Store) RunWorkflow(ctx context.Context, id string, allowEdits bool, run WorkflowRunFunc, emit func(Update)) (Workflow, error) {
	w, err := s.Workflow(id)
	if err != nil {
		return w, err
	}
	if w.State != "ready" {
		return w, fmt.Errorf("workflow is %s; inspect its status before continuing", w.State)
	}
	stage := w.Recipe.Stages[w.Current]
	if stage.Edits && !allowEdits {
		return w, fmt.Errorf("stage %s needs --allow-edits; user approval remains recorded", stage.Name)
	}
	session, err := s.Get(w.Session)
	if err != nil {
		return w, err
	}
	ctx, release, err := s.claimWorkflow(ctx, id)
	if err != nil {
		return w, err
	}
	defer release()
	w.State = "running"
	if err := s.SaveWorkflow(&w); err != nil {
		return w, err
	}
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Crew workflow %s. Stage: %s. Role: %s.\nGoal: %s\n\nStage instructions:\n%s\n", w.ID, stage.Name, stage.Role, w.Task, stage.Prompt)
	prompt.WriteString("\nPrior stage outputs and user decisions are historical context. Follow repository instructions. Do not advance stages, approve gates, or modify Crew's workflow data.\n")
	for _, r := range w.Results {
		fmt.Fprintf(&prompt, "\n[%s attempt %d: %s / %s]\n%s\n", r.Stage, r.Attempt, r.State, r.Agent, r.Output)
	}
	prompt.WriteString("\nEnd your response with exactly one standalone line: CREW_STAGE_RESULT: pass, CREW_STAGE_RESULT: fail, or CREW_STAGE_RESULT: blocked. Pass means the requested stage work is complete with concrete evidence; do not claim tests passed without running them. Use blocked for unavailable tools or unresolved questions.\n")
	output, runErr := run(ctx, session, RunOptions{Agent: stage.Agent, Model: stage.Model, Task: prompt.String(), Edits: stage.Edits}, emit)
	if runErr == nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}
	state := StageVerdict(output)
	if runErr != nil {
		state = "fail"
		output += "\nError: " + runErr.Error()
	}
	w.record(state, output, stage.Agent)
	switch state {
	case "pass":
		w.Current++
		w.settle()
	case "fail":
		w.State = "failed"
	default:
		w.State = "blocked"
	}
	if err := s.SaveWorkflow(&w); err != nil {
		return w, err
	}
	if runErr == nil && (w.State == "failed" || w.State == "blocked") {
		runErr = fmt.Errorf("stage %s is %s; inspect its result before retrying", stage.Name, w.State)
	}
	return w, runErr
}

// A separate renewable lease protects stage execution and crash recovery. The
// runner still holds its repository lease while the native CLI is executing.
func (s *Store) claimWorkflow(ctx context.Context, id string) (context.Context, func(), error) {
	key, owner := "workflow:"+id, id+":"+stamp()
	if err := s.Acquire(key, owner); err != nil {
		return ctx, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Renew(key, owner); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(); <-done; _ = s.Release(key, owner) }, nil
}

func (s *Store) RecoverWorkflow(id, note string) (Workflow, error) {
	w, err := s.Workflow(id)
	if err != nil {
		return w, err
	}
	if w.State != "running" || strings.TrimSpace(note) == "" {
		return w, fmt.Errorf("recover needs an interrupted running stage and a note explaining what happened")
	}
	_, release, err := s.claimWorkflow(context.Background(), id)
	if err != nil {
		return w, fmt.Errorf("stage execution lease is still held; stop the process or wait for expiry: %w", err)
	}
	defer release()
	w.record("interrupted", note, "user")
	w.State = "blocked"
	err = s.SaveWorkflow(&w)
	return w, err
}
