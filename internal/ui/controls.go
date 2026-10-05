// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tesserix/tesserix-crew/internal/core"
)

type catalogEditedMsg struct{ err error }

func (m model) providerNames() []string {
	config := core.Config{}
	if m.runner != nil {
		config = m.runner.Config
	}
	names := []string{}
	for _, name := range config.ProviderNames() {
		if _, err := config.Provider(name); err == nil {
			names = append(names, name)
		}
	}
	return names
}
func (m model) completions() []string {
	out := []string{"/agent auto"}
	for _, name := range m.providerNames() {
		out = append(out, "/agent "+name)
	}
	return append(out, "/model", "/skills", "/skills list", "/skills refresh", "/providers", "/status", "/context", "/workflow", "/workflow next", "/workflow approve", "/workflow reject", "/workflow evidence", "/help", "/quit")
}
func (m *model) workflowSlash(fields []string) tea.Cmd {
	if m.runner == nil {
		return nil
	}
	w, err := m.runner.Store.Workflow(m.session.ID)
	if err != nil {
		m.add("No lifecycle attached. Start one with crew workflow start \"task\".")
		return nil
	}
	if len(fields) > 1 {
		action := fields[1]
		note := strings.Join(fields[2:], " ")
		switch action {
		case "approve", "reject", "retry", "record":
			w, err = m.runner.Store.DecideWorkflow(w.ID, action, note)
		case "repair":
			w, err = m.runner.Store.RepairWorkflow(w.ID, note)
		case "evidence":
			evidence, e := m.runner.Store.Evidence(w.ID)
			if e != nil {
				err = e
				break
			}
			for _, item := range evidence {
				m.add(fmt.Sprintf("Check %s · %s · exit %d · passed=%v\n%s", item.Check, item.Phase, item.ExitCode, item.Passed, item.Output))
				for _, artifact := range item.Artifacts {
					m.add("Artifact: " + artifact.Snapshot)
				}
			}
			return nil
		case "next":
			if w.State != "ready" {
				m.add("Workflow is " + w.State + "; review its current gate first.")
				return nil
			}
			stage := w.Recipe.Stages[w.Current]
			m.options.Model = stage.Model
			r := *m.runner
			r.Config.Providers = w.Providers
			edits := m.options.Edits
			return m.startOperation(func(ctx context.Context, emit func(core.Update)) error {
				if stage.Kind == "github-create" || stage.Kind == "github-close" {
					_, err := r.Store.RunGitHubStage(ctx, w.ID, core.GitHubCLI{})
					return err
				}
				_, err := r.Store.RunWorkflow(ctx, w.ID, edits, r.Run, emit)
				return err
			})
		default:
			m.add("Usage: /workflow [next|approve|reject FEEDBACK|retry NOTE|repair NOTE|record EVIDENCE|evidence]")
			return nil
		}
		if err != nil {
			m.add("Error\n" + err.Error())
			return nil
		}
	}
	m.workflowState = w.State
	m.workflowStage = "done"
	if w.Current < len(w.Recipe.Stages) {
		m.workflowStage = w.Recipe.Stages[w.Current].Name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Workflow %s · %s\n%s\n", w.ID, w.State, w.Task)
	for i, stage := range w.Recipe.Stages {
		marker := "○"
		if i < w.Current {
			marker = "✓"
		}
		if i == w.Current {
			marker = "→"
		}
		provider := stage.Agent
		if provider == "" {
			provider = "user / " + stage.Kind
		}
		if stage.Model != "" {
			provider += " / " + stage.Model
		}
		fmt.Fprintf(&b, "%s %s · %s\n", marker, stage.Name, provider)
	}
	for _, issue := range w.Issues {
		fmt.Fprintf(&b, "Issue: %s\n", issue.URL)
	}
	for _, check := range w.Recipe.Checks {
		fmt.Fprintf(&b, "Check: %s (%s) %s\n", check.Name, check.Kind, strings.Join(check.Command, " "))
	}
	if w.Recipe.AllowSelfTest {
		b.WriteString("Independent-provider policy explicitly relaxed.\n")
	}
	if w.Recipe.NoTDDReason != "" {
		b.WriteString("TDD exception: " + w.Recipe.NoTDDReason + "\n")
	}
	if len(w.Results) > 0 {
		result := w.Results[len(w.Results)-1]
		fmt.Fprintf(&b, "\nLatest %s result:\n%s\n", result.Stage, result.Output)
	}
	if w.State == "awaiting-approval" {
		b.WriteString("\nReview the plan, issue links, test commands and acceptance criteria above before /workflow approve. Use /workflow reject FEEDBACK to request changes.")
	}
	m.add(b.String())
	return nil
}
func (m *model) startOperation(operation func(context.Context, func(core.Update)) error) tea.Cmd {
	m.busy = true
	m.input.Blur()
	m.status = "starting"
	m.turnStarted = time.Now()
	m.timing = core.TurnTiming{}
	m.messages = make(chan tea.Msg, 64)
	base := m.base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	m.cancel = cancel
	ch := m.messages
	go func() {
		err := operation(ctx, func(update core.Update) {
			select {
			case ch <- progressMsg(update):
			case <-ctx.Done():
			}
		})
		select {
		case ch <- finishMsg{err}:
		case <-base.Done():
		}
	}()
	return wait(ch)
}
func (m *model) skillSlash(fields []string) tea.Cmd {
	if m.runner == nil {
		return nil
	}
	if len(fields) > 1 {
		action := fields[1]
		switch action {
		case "add":
			if len(fields) != 3 {
				m.add("Usage: /skills add PATH")
				return nil
			}
			source, err := filepath.Abs(fields[2])
			if err == nil {
				err = core.CopySkill(source, filepath.Join(core.SkillRoot(m.runner.Home, m.session.Repo, false), filepath.Base(source)))
			}
			if err != nil {
				m.add(err.Error())
			} else {
				m.add("Skill added; use refresh preview to review session changes.")
			}
			return nil
		case "list":
			catalog, states, err := core.SkillCatalog(m.runner.Home, m.session.Repo)
			if err != nil {
				m.add(err.Error())
				return nil
			}
			var b strings.Builder
			for _, name := range core.SortedSkillNames(catalog) {
				state := "enabled"
				if states[name] {
					state = "disabled"
				}
				fmt.Fprintf(&b, "%s · %s · %s\n", name, state, catalog[name])
			}
			m.add(b.String())
			return nil
		case "refresh":
			apply := len(fields) == 3 && fields[2] == "apply"
			changes, err := m.runner.RefreshSkills(m.session, apply)
			if err != nil {
				m.add(err.Error())
				return nil
			}
			m.add(strings.Join(changes, "\n"))
			if !apply {
				m.add("Preview only. /skills refresh apply explicitly updates this session's instructions.")
			} else {
				pinned, _ := m.runner.Skills(m.session)
				m.skillCount = len(pinned)
			}
			return nil
		case "select":
			if _, err := m.runner.Store.Workflow(m.session.ID); err == nil {
				m.add("Lifecycle selections are pinned per role. Change the recipe for a new run.")
				return nil
			}
			names := fields[2:]
			if len(names) == 1 && names[0] == "none" {
				names = []string{}
			}
			if len(fields) < 3 {
				m.add("Usage: /skills select NAME... or /skills select none")
				return nil
			}
			if err := m.runner.Store.SetSkillSelection(m.session.ID, names); err != nil {
				m.add(err.Error())
				return nil
			}
			// Load the persisted selection each turn so native resumes can omit
			// unchanged skill text; explicit stage selections remain separate.
			m.options.Skills = nil
			m.add("Session skill selection saved: " + strings.Join(names, ", "))
			return nil
		case "enable", "disable", "remove", "show", "edit":
			if len(fields) != 3 {
				m.add("Usage: /skills " + action + " NAME")
				return nil
			}
			name := fields[2]
			var err error
			if action == "enable" || action == "disable" {
				err = core.SetSkillEnabled(m.runner.Home, m.session.Repo, name, false, action == "enable")
			} else if action == "remove" {
				_, err = core.RemoveSkill(m.runner.Home, m.session.Repo, name, false)
			} else {
				catalog, _, e := core.SkillCatalog(m.runner.Home, m.session.Repo)
				if e != nil {
					m.add(e.Error())
					return nil
				}
				source, ok := catalog[name]
				if !ok {
					m.add("Unknown skill: " + name)
					return nil
				}
				if action == "show" {
					data, e := os.ReadFile(filepath.Join(source, "SKILL.md"))
					if e != nil {
						m.add(e.Error())
					} else {
						m.add("Skill " + name + "\n" + string(data))
					}
					return nil
				}
				target := filepath.Join(core.SkillRoot(m.runner.Home, m.session.Repo, false), name)
				if source != target {
					if err := core.CopySkill(source, target); err != nil {
						m.add(err.Error())
						return nil
					}
				}
				editor := os.Getenv("VISUAL")
				if strings.TrimSpace(editor) == "" {
					editor = os.Getenv("EDITOR")
				}
				parts := strings.Fields(editor)
				if len(parts) == 0 {
					parts = []string{"vi"}
				}
				cmd := exec.Command(parts[0], append(parts[1:], filepath.Join(target, "SKILL.md"))...)
				return tea.ExecProcess(cmd, func(err error) tea.Msg { return catalogEditedMsg{err} })
			}
			if err != nil {
				m.add(err.Error())
			} else {
				m.add("Catalog updated; this session's skill revisions remain pinned.")
			}
			return nil
		default:
			m.add("Usage: /skills [list|show|edit|enable|disable|remove NAME|refresh [apply]|select NAME...]")
			return nil
		}
	}
	pinned, _, err := m.runner.Store.SkillManifest(m.session.ID)
	if err != nil {
		m.add(err.Error())
		return nil
	}
	if len(pinned) == 0 {
		m.add("No pinned session skills. /skills list shows the catalog.")
	}
	for _, skill := range pinned {
		m.add(skill.Name + " · " + skill.Digest[:12])
	}
	return nil
}
