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
	Description   string  `toml:"description" json:"description"`
	AllowSelfTest bool    `toml:"allow_self_test" json:"allow_self_test,omitempty"`
	GitHubRepo    string  `toml:"github_repo" json:"github_repo,omitempty"`
	Checks        []Check `toml:"checks" json:"checks,omitempty"`
	TDD           bool    `toml:"tdd" json:"tdd,omitempty"`
	NoTDDReason   string  `toml:"no_tdd_reason" json:"no_tdd_reason,omitempty"`
	MaxRepairs    int     `toml:"max_repairs" json:"max_repairs,omitempty"`
	Stages        []Stage `toml:"stages" json:"stages"`
}
type Stage struct {
	Name      string   `toml:"name" json:"name"`
	Kind      string   `toml:"kind" json:"kind"` // agent, approval, checkpoint
	Role      string   `toml:"role" json:"role,omitempty"`
	Agent     string   `toml:"agent" json:"agent,omitempty"`
	Model     string   `toml:"model" json:"model,omitempty"`
	Prompt    string   `toml:"prompt" json:"prompt,omitempty"`
	Skills    []string `toml:"skills" json:"skills"`
	Requires  []string `toml:"requires" json:"requires,omitempty"`
	DependsOn []string `toml:"depends_on" json:"depends_on,omitempty"`
	Timeout   string   `toml:"timeout" json:"timeout,omitempty"`
	Edits     bool     `toml:"allow_edits" json:"allow_edits,omitempty"`
}
type StageResult struct {
	Stage    string `json:"stage"`
	Attempt  int    `json:"attempt"`
	Agent    string `json:"agent,omitempty"`
	State    string `json:"state"`
	Output   string `json:"output"`
	Revision int    `json:"revision"`
	Created  string `json:"created"`
}
type Workflow struct {
	ID        string              `json:"id"`
	Session   string              `json:"session"`
	Repo      string              `json:"repo"`
	Task      string              `json:"task"`
	Preset    string              `json:"preset"`
	Providers map[string]Provider `json:"providers,omitempty"`
	Recipe    Lifecycle           `json:"recipe"`
	Current   int                 `json:"current"`
	State     string              `json:"state"`
	Issues    []IssueLink         `json:"issues,omitempty"`
	Results   []StageResult       `json:"results"`
	Revision  int                 `json:"revision"`
	Updated   string              `json:"updated"`
}

func BuiltinLifecycles() map[string]Lifecycle {
	presets := map[string]Lifecycle{
		"default": {TDD: true, MaxRepairs: 2, Description: "Reviewed delivery with independent testing and GitHub checkpoints", Stages: []Stage{
			{Name: "design", Kind: "agent", Role: "designer", Agent: "claude", Prompt: "Clarify the goal, constraints and acceptance criteria. Compare design options; recommend a concrete design. Do not implement."},
			{Name: "plan", Kind: "agent", Role: "planner", Agent: "claude", Prompt: "Create an implementation plan and proposed GitHub issue scope with acceptance criteria and a test strategy. Do not implement."},
			{Name: "agent-review", Kind: "agent", Role: "reviewer", Agent: "codex", Prompt: "Independently review the design and plan. Check feasibility, scope, risks and test coverage. Fail if material problems remain; give actionable feedback. Do not implement."},
			{Name: "create-github-issues", Kind: "github-create", Prompt: "Create the scoped GitHub issues from the reviewed typed plan, preserving durable issue identities."},
			{Name: "user-review", Kind: "approval", Prompt: "Review the design, plan, agent review and issue scope. Explicitly approve before implementation, or reject with feedback."},
			{Name: "implement", Kind: "agent", Role: "implementer", Agent: "codex", Edits: true, Prompt: "Implement only the approved scope using TDD: add a meaningful failing test, implement, then run the relevant tests. Record commands, outcomes and changed files. Do not publish, merge or close issues."},
			{Name: "test", Kind: "agent", Role: "tester", Agent: "claude", Edits: true, Prompt: "Independently validate the implementation against the acceptance criteria. Run relevant tests, including E2E or browser testing when applicable and available. Record exact commands, results and limitations. Fail or block if required validation cannot be completed. Do not fix production code or publish."},
			{Name: "deliver", Kind: "agent", Role: "deliverer", Agent: "claude", Prompt: "Prepare the delivery report from implementation and independent testing evidence: changed behavior, checks performed, remaining limitations, and issue closure evidence. Do not publish, merge, or close issues."},
			{Name: "close-github-issues", Kind: "github-close", Prompt: "Close only the tracked GitHub issues after delivery and required executable checks pass."},
		}},
		"review": {Description: "Read-only repository review", Stages: []Stage{
			{Name: "review", Kind: "agent", Role: "reviewer", Agent: "codex", Prompt: "Review the requested scope and report concrete findings with file references. Do not change files."},
			{Name: "user-review", Kind: "approval", Prompt: "Review the findings and approve the completed report or reject with feedback."},
		}},
	}
	for _, name := range []string{"bugfix", "maintenance"} {
		recipe := presets["default"]
		recipe.Description = "Reviewed " + name + " with TDD and independent testing"
		recipe.Stages = nil
		for _, stage := range presets["default"].Stages {
			if stage.Kind != "github-create" && stage.Kind != "github-close" {
				recipe.Stages = append(recipe.Stages, stage)
			}
		}
		presets[name] = recipe
	}
	return presets
}
func (l Lifecycle) Validate() error { return l.ValidateWithProviders(BuiltinProviders()) }
func (l Lifecycle) ValidateWithProviders(providers map[string]Provider) error {
	if l.GitHubRepo != "" && !githubSlug.MatchString(l.GitHubRepo) {
		return fmt.Errorf("github_repo must be owner/repo")
	}
	if l.MaxRepairs < 0 || l.MaxRepairs > 10 {
		return fmt.Errorf("max_repairs must be between 0 and 10")
	}
	checks := map[string]bool{}
	for _, check := range l.Checks {
		if err := check.Validate(); err != nil {
			return err
		}
		if checks[check.Name] {
			return fmt.Errorf("duplicate check %s", check.Name)
		}
		checks[check.Name] = true
	}
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
		for _, dependency := range s.DependsOn {
			if !seen[dependency] {
				return fmt.Errorf("stage %s depends on missing or later stage %s", s.Name, dependency)
			}
		}
		if s.Timeout != "" {
			duration, err := time.ParseDuration(s.Timeout)
			if err != nil || duration <= 0 {
				return fmt.Errorf("stage %s needs a positive timeout duration", s.Name)
			}
		}
		seen[s.Name] = true
		if strings.TrimSpace(s.Prompt) == "" {
			return fmt.Errorf("stage %s needs a prompt", s.Name)
		}
		switch s.Kind {
		case "approval", "checkpoint", "github-create", "github-close":
			if s.Agent != "" || s.Model != "" || s.Edits {
				return fmt.Errorf("manual stage %s cannot select an agent or edit files", s.Name)
			}
			if s.Kind == "approval" {
				gate = true
			}
		case "agent":
			p, ok := providers[s.Agent]
			if !ok {
				return fmt.Errorf("stage %s: unknown provider %s", s.Name, s.Agent)
			}
			p.Disabled = false
			for _, required := range s.Requires {
				if !p.Has(required) {
					return fmt.Errorf("stage %s requires capability %s from %s", s.Name, required, s.Agent)
				}
			}
			if _, err := (Config{Providers: map[string]Provider{s.Agent: p}}).Command(AgentOptions{Agent: s.Agent, Model: s.Model, Edits: s.Edits}); err != nil {
				return fmt.Errorf("stage %s: %w", s.Name, err)
			}
			if strings.TrimSpace(s.Role) == "" {
				return fmt.Errorf("agent stage %s needs a role", s.Name)
			}
			if s.Edits && !gate {
				return fmt.Errorf("stage %s enables edits before a user approval gate", s.Name)
			}
			if s.Role == "tester" && implementers[s.Agent] && !l.AllowSelfTest {
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
	return s.CreateWorkflowWithProviders(repo, task, preset, recipe, BuiltinProviders())
}
func (s *Store) CreateWorkflowWithProviders(repo, task, preset string, recipe Lifecycle, providers map[string]Provider) (Workflow, error) {
	task = (Config{Providers: providers}).Redact(task)
	if strings.TrimSpace(task) == "" {
		return Workflow{}, fmt.Errorf("workflow task is empty")
	}
	if err := recipe.ValidateWithProviders(providers); err != nil {
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
	w := Workflow{ID: session.ID, Session: session.ID, Repo: repo, Task: task, Preset: preset, Recipe: recipe, Providers: providers, Updated: stamp()}
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
	w.Results = append(w.Results, StageResult{Stage: stage.Name, Attempt: attempt, Agent: agent, State: state, Output: output, Created: stamp(), Revision: w.Revision})
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
		if w.State == "rejected" { // Re-plan and replay downstream gates. Preserve every rejected decision.
			target := -1
			for i := w.Current - 1; i >= 0; i-- {
				if w.Recipe.Stages[i].Role == "planner" {
					target = i
					break
				}
			}
			if target < 0 {
				for i := w.Current - 1; i >= 0; i-- {
					if w.Recipe.Stages[i].Kind == "agent" {
						target = i
						break
					}
				}
			}
			if target >= 0 {
				w.Current = target
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
	if stage.Kind != "agent" {
		return w, fmt.Errorf("current stage %s requires its dedicated lifecycle action", stage.Kind)
	}
	if stage.Timeout != "" {
		duration, _ := time.ParseDuration(stage.Timeout)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	}
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
	for _, check := range w.Recipe.Checks {
		fmt.Fprintf(&prompt, "\nPinned %s check %s: %s. Use Crew run_check tool (name=%s, phase=red or green) or execute %s workflow check --name %s --phase red|green %s to record actual evidence.\n", check.Kind, check.Name, strings.Join(check.Command, " "), check.Name, "crew", check.Name, w.ID)
	}
	if w.Recipe.NoTDDReason != "" {
		fmt.Fprintf(&prompt, "\nUser-configured TDD exception: %s\n", w.Recipe.NoTDDReason)
	}
	prompt.WriteString("\nEnd your response with exactly one standalone line: CREW_STAGE_RESULT: pass, CREW_STAGE_RESULT: fail, or CREW_STAGE_RESULT: blocked. Pass means the requested stage work is complete with concrete evidence; do not claim tests passed without running them. Use blocked for unavailable tools or unresolved questions.\n")
	testerTree := ""
	var output string
	var runErr error
	if stage.Role == "tester" {
		testerTree, runErr = s.treeDigest(ctx, w.Repo, w.Recipe.Checks)
	}
	if runErr == nil {
		output, runErr = run(ctx, session, RunOptions{Agent: stage.Agent, Model: stage.Model, Task: prompt.String(), Edits: stage.Edits, Skills: stage.Skills}, emit)
	}
	if runErr == nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}
	state := StageVerdict(output)
	if state == "pass" && stage.Role == "tester" {
		after, err := s.treeDigest(ctx, w.Repo, w.Recipe.Checks)
		if err != nil || after != testerTree {
			state = "fail"
			output += "\nIndependent tester changed repository files outside declared artifacts; route changes through implementation."
		}
	}
	if state == "pass" && stage.Role == "planner" && w.Recipe.TDD && len(w.Recipe.Checks) == 0 {
		checks, err := ParseCheckProposal(output)
		if err != nil {
			state = "blocked"
			output += "\nCheck plan: " + err.Error()
		} else {
			candidate := w.Recipe
			candidate.Checks = checks
			if err := candidate.ValidateWithProviders(w.Providers); err != nil {
				state = "blocked"
				output += "\nCheck plan: " + err.Error()
			} else {
				w.Recipe.Checks = checks
			}
		}
	}
	if state == "pass" && len(w.Recipe.Checks) > 0 {
		if err := s.VerifyStageEvidence(w, stage, w.Revision); err != nil {
			state = "blocked"
			output += "\nEvidence: " + err.Error()
		}
	}
	if state == "pass" && stage.Role == "implementer" && w.Recipe.TDD && len(w.Recipe.Checks) == 0 && w.Recipe.NoTDDReason == "" {
		state = "blocked"
		output += "\nNo test checks are pinned for required TDD."
	}
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
	return s.claimLease(ctx, "workflow:"+id)
}
func (s *Store) claimLease(ctx context.Context, key string) (context.Context, func(), error) {
	owner := key + ":" + stamp()
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
