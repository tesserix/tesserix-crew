// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/tesserix/tesserix-crew/internal/core"
)

func SessionChoices(rows []core.SessionSummary) []Choice {
	out := make([]Choice, 0, len(rows))
	for _, row := range rows {
		updated := row.Updated
		if timestamp, err := time.Parse(time.RFC3339Nano, updated); err == nil {
			updated = timestamp.UTC().Format("2006-01-02 15:04 UTC")
		}
		out = append(out, Choice{ID: row.ID, Label: strings.Join(strings.Fields(row.Task), " ") + " · " + row.State,
			Detail: row.Repo + " · " + row.ID + "\n" + row.Agent + " · " + updated + "\nNext: " + row.NextAction})
	}
	return out
}

func (m *model) openSessions(all bool) tea.Cmd {
	if m.runner == nil || m.busy {
		return nil
	}
	repo := m.session.Repo
	title := "Resume a project session · /sessions all includes other projects"
	if all {
		repo, title = "", "Resume a session · all projects"
	}
	rows, err := m.runner.Store.SessionSummaries(repo)
	if err != nil {
		m.add("Error\n" + err.Error())
		return nil
	}
	choices := SessionChoices(rows)
	for i := range choices {
		if choices[i].ID == m.session.ID {
			choices[i].Label = "Current · " + choices[i].Label
		}
	}
	m.panel = &panel{kind: "sessions", picker: newPicker(title, choices)}
	m.resizePanel()
	return textinput.Blink
}

func (m *model) switchSession(id string) error {
	if m.runner == nil || m.busy {
		return fmt.Errorf("wait for or cancel the active task before switching sessions")
	}
	session, err := m.runner.Store.Get(id)
	if err != nil {
		return err
	}
	if session.Parent != "" {
		return fmt.Errorf("delegated child sessions are read-only records; resume their parent %s", session.Parent)
	}
	runner := *m.runner
	if w, err := runner.Store.Workflow(session.ID); err == nil {
		runner.Config = core.Config{DefaultAgent: session.Agent, Providers: w.Providers}
	} else {
		runner.Config, err = core.LoadConfig(runner.Home, session.Repo)
		if err != nil {
			return err
		}
	}
	options := m.options
	options.Agent, options.Model = session.Agent, ""
	next, err := newModel(m.base, &runner, session, options)
	if err != nil {
		return err
	}
	next.width, next.height = m.width, m.height
	next.output.Width, next.output.Height = m.output.Width, m.output.Height
	next.input.Width = m.input.Width
	next.refresh()
	*m = next
	return nil
}

func (m *model) openWorkflowStart(args []string) tea.Cmd {
	if m.runner == nil || m.busy {
		return nil
	}
	if len(args) > 0 {
		preset := args[0]
		if len(args) > 1 {
			m.createWorkflow(preset, strings.Join(args[1:], " "))
			return nil
		}
		m.workflowTaskPrompt(preset)
		return textinput.Blink
	}
	config, err := core.LoadConfig(m.runner.Home, m.session.Repo)
	if err != nil {
		m.add("Error\n" + err.Error())
		return nil
	}
	names := make([]string, 0, len(config.Lifecycles))
	for name := range config.Lifecycles {
		names = append(names, name)
	}
	sort.Strings(names)
	choices := []Choice{}
	for _, name := range names {
		recipe := config.Lifecycles[name]
		stages := []string{}
		for _, stage := range recipe.Stages {
			stages = append(stages, stage.Name)
		}
		choices = append(choices, Choice{ID: name, Label: name + " · " + recipe.Description, Detail: strings.Join(stages, " → ")})
	}
	m.panel = &panel{kind: "presets", picker: newPicker("Choose a workflow · creation does not run agents", choices)}
	m.resizePanel()
	return textinput.Blink
}

func (m *model) workflowTaskPrompt(preset string) {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Describe the task and acceptance criteria"
	input.CharLimit = 100000
	input.Focus()
	m.panel = &panel{kind: "task", title: "Start " + preset + " workflow", preset: preset, input: input}
	m.resizePanel()
}

func (m *model) createWorkflow(preset, task string) {
	w, err := m.runner.StartWorkflow(m.session.Repo, task, preset)
	if err == nil {
		err = m.switchSession(w.Session)
	}
	if err != nil {
		m.panel = nil
		m.add("Error\n" + err.Error())
		return
	}
	m.openWorkflowReview()
}

func workflowReviewText(w core.Workflow, evidence []core.Evidence, changes string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nPreset %s · revision %d · %s\nNext: %s\n\n", w.Task, w.Preset, w.Revision, w.State, core.WorkflowNextAction(w))
	for i, stage := range w.Recipe.Stages {
		marker := "○"
		if i < w.Current {
			marker = "✓"
		} else if i == w.Current {
			marker = "→"
		}
		fmt.Fprintf(&b, "%s %s · %s %s\n", marker, stage.Name, stage.Kind, stage.Agent)
		if i == w.Current {
			fmt.Fprintf(&b, "  %s\n", stage.Prompt)
		}
	}
	fmt.Fprintf(&b, "\nRepository changes\n%s\n", changes)
	for _, issue := range w.Issues {
		fmt.Fprintf(&b, "\nIssue: %s\n", issue.URL)
	}
	b.WriteString("\nPinned checks (commands require workflow approval)\n")
	if len(w.Recipe.Checks) == 0 {
		b.WriteString("No executable checks pinned.\n")
	}
	for _, check := range w.Recipe.Checks {
		fmt.Fprintf(&b, "%s · %s · %s\n", check.Name, check.Kind, strings.Join(check.Command, " "))
	}
	if w.Recipe.NoTDDReason != "" {
		fmt.Fprintf(&b, "TDD exception: %s\n", w.Recipe.NoTDDReason)
	}
	if w.Recipe.AllowSelfTest {
		b.WriteString("Independent-provider policy explicitly relaxed.\n")
	}
	b.WriteString("\nStage outputs and decisions · includes earlier attempts\n")
	for _, result := range w.Results {
		fmt.Fprintf(&b, "\n%s · attempt %d · %s · revision %d\n%s\n", result.Stage, result.Attempt, result.State, result.Revision, result.Output)
	}
	b.WriteString("\nRecorded checks · earlier runs are historical; closure revalidates evidence\n")
	if len(evidence) == 0 {
		b.WriteString("No checks have been recorded.\n")
	}
	for _, item := range evidence {
		fmt.Fprintf(&b, "\n%s · %s · stage %s · revision %d · exit %d · passed=%v\n%s\n%s\n", item.Check, item.Phase, item.Stage, item.Revision, item.ExitCode, item.Passed, strings.Join(item.Command, " "), item.Output)
		for _, artifact := range item.Artifacts {
			fmt.Fprintf(&b, "Artifact: %s\n", artifact.Snapshot)
		}
	}
	return b.String()
}

func (m *model) openWorkflowReview() tea.Cmd {
	if m.runner == nil || m.busy {
		return nil
	}
	w, err := m.runner.Store.Workflow(m.session.ID)
	if err != nil {
		m.add("No lifecycle attached. Use /workflow start to choose a preset and task.")
		return nil
	}
	evidence, err := m.runner.Store.Evidence(w.ID)
	if err != nil {
		m.add("Error\n" + err.Error())
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "status", "--short")
	cmd.Dir = w.Repo
	output, gitErr := cmd.Output()
	changes := strings.TrimSpace(string(output))
	if gitErr != nil {
		changes = "Git status unavailable; inspect the repository before approving."
	} else if changes == "" {
		changes = "Working tree clean."
	}
	m.panel = &panel{kind: "review", title: "Workflow " + w.ID + " · " + w.State, state: w.State, workflowID: w.ID, revision: w.Revision, viewport: viewport.New(80, 16)}
	m.panel.content = ansi.Strip(workflowReviewText(w, evidence, changes))
	m.resizePanel()
	m.workflowState = w.State
	return nil
}

func (m *model) resizePanel() {
	p := m.panel
	if p == nil {
		return
	}
	p.picker.width, p.picker.height = m.width, m.height
	p.picker.input.Width = max(1, min(100, m.width-8))
	p.input.Width = max(1, min(100, m.width-8))
	p.viewport.Width, p.viewport.Height = max(1, min(100, m.width-4)), max(1, m.height-5)
	if p.content != "" {
		p.viewport.SetContent(renderBody(p.content, p.viewport.Width))
	}
}

func (m model) panelView() string {
	p := m.panel
	if p.kind == "sessions" || p.kind == "presets" || p.kind == "agents" || p.kind == "models" {
		return p.picker.View()
	}
	width := max(1, min(100, m.width-4))
	height := max(1, m.height-1)
	content := p.viewport.View()
	hint := "Esc chat · e evidence · PgUp/PgDn scroll"
	switch p.state {
	case "ready":
		hint = "n next · " + hint
	case "awaiting-approval":
		hint = "a approve · r reject · " + hint
	case "awaiting-evidence":
		hint = "c record evidence · " + hint
	case "failed", "blocked", "rejected":
		hint = "t retry · " + hint
	}
	if p.kind == "confirm" {
		content = "Approve the reviewed workflow revision?\nThis permits subsequent stages and their pinned check commands.\n\nEnter approves · Esc returns to review"
		hint = "Approval does not run the next stage."
	} else if p.kind == "task" || p.kind == "reject" || p.kind == "retry" || p.kind == "record" {
		content = p.input.View()
		hint = "Enter submit · Esc cancel"
	}
	if p.kind == "summary-review" {
		hint = "Enter confirm · Esc cancel · PgUp/PgDn scroll"
	}
	if p.kind == "summary-confirm" {
		hint = "Enter applies the reviewed summary · Esc cancels"
	}
	title := ansi.Truncate(p.title, width, "…")
	return lipgloss.NewStyle().Width(width).MaxWidth(width).MaxHeight(height).Padding(0, 1).Render(title + "\n\n" + content + "\n" + ansi.Truncate(hint, width, "…"))
}

func (m model) updatePanel(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.panel
	if p.kind == "sessions" || p.kind == "presets" || p.kind == "agents" || p.kind == "models" {
		next, cmd := p.picker.Update(key)
		p.picker = next.(picker)
		if !p.picker.done {
			return m, cmd
		}
		id, kind := p.picker.selected, p.kind
		m.panel = nil
		if id == "" {
			return m, nil
		}
		if kind == "sessions" {
			if err := m.switchSession(id); err != nil {
				m.add("Error\n" + err.Error())
			}
		} else if kind == "agents" {
			m.slash("/agent " + id)
		} else if kind == "models" {
			if id == "__crew_default" {
				id = ""
			}
			m.options.Model = id
			m.add("Model: " + id)
		} else {
			m.workflowTaskPrompt(id)
			return m, textinput.Blink
		}
		return m, nil
	}
	if key.String() == "esc" || key.String() == "ctrl+c" {
		m.panel = nil
		if p.kind == "confirm" || p.kind == "reject" || p.kind == "retry" || p.kind == "record" {
			m.openWorkflowReview()
		}
		return m, nil
	}
	if p.kind == "summary-review" {
		if key.String() == "enter" {
			p.kind = "summary-confirm"
			p.title = "Apply this reviewed summary? · Enter applies · Esc cancels"
			return m, nil
		}
		var cmd tea.Cmd
		p.viewport, cmd = p.viewport.Update(key)
		return m, cmd
	}
	if p.kind == "summary-confirm" {
		if key.String() == "enter" {
			err := m.runner.Store.ApplyContextSummary(p.workflowID, p.preset)
			m.panel = nil
			if err != nil {
				m.add("Error\n" + err.Error())
			} else {
				m.add("Reviewed summary applied; the full history remains saved.")
			}
		}
		return m, nil
	}
	if p.kind == "review" {
		switch key.String() {
		case "n":
			if p.state != "ready" {
				return m, nil
			}
			m.panel = nil
			cmd := m.workflowSlash([]string{"/workflow", "next"})
			return m, cmd
		case "a":
			if p.state != "awaiting-approval" {
				return m, nil
			}
			p.kind, p.title = "confirm", "Confirm approval"
			return m, nil
		case "r", "t", "c":
			if (key.String() == "r" && p.state != "awaiting-approval") ||
				(key.String() == "t" && p.state != "failed" && p.state != "blocked" && p.state != "rejected") ||
				(key.String() == "c" && p.state != "awaiting-evidence") {
				return m, nil
			}
			p.kind, p.title = "reject", "Reject with feedback"
			if key.String() == "t" {
				p.kind, p.title = "retry", "Retry with feedback"
			} else if key.String() == "c" {
				p.kind, p.title = "record", "Record checkpoint evidence"
			}
			p.input = textinput.New()
			p.input.Prompt = "> "
			p.input.CharLimit = 100000
			p.input.Focus()
			m.resizePanel()
			return m, textinput.Blink
		case "e":
			// The review includes all evidence; jump directly to that section.
			p.viewport.GotoBottom()
			return m, nil
		}
		var cmd tea.Cmd
		p.viewport, cmd = p.viewport.Update(key)
		return m, cmd
	}
	if key.String() == "enter" {
		if p.kind == "task" {
			if task := strings.TrimSpace(p.input.Value()); task != "" {
				m.createWorkflow(p.preset, task)
			}
			return m, nil
		}
		action, note := p.kind, strings.TrimSpace(p.input.Value())
		if action == "confirm" {
			action = "approve"
		} else if note == "" {
			return m, nil
		}
		_, err := m.runner.Store.DecideWorkflowRevision(p.workflowID, action, note, p.revision)
		m.panel = nil
		if err != nil {
			m.add("Error\n" + err.Error())
		} else {
			m.openWorkflowReview()
		}
		return m, nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(key)
	return m, cmd
}
