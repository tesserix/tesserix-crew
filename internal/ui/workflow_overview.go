// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"fmt"
	"strings"

	"github.com/tesserix/tesserix-crew/internal/core"
)

func workflowOverviewText(w core.Workflow, evidence []core.Evidence) string {
	var b strings.Builder
	total := len(w.Recipe.Stages)
	completed := max(0, min(w.Current, total))
	fmt.Fprintf(&b, "%s\nWorkflow overview · %s · revision %d\nNext: %s\n", w.Task, w.State, w.Revision, core.WorkflowNextAction(w))
	if w.Current >= 0 && w.Current < total {
		fmt.Fprintf(&b, "Current: %s (%d/%d)\n", w.Recipe.Stages[w.Current].Name, w.Current+1, total)
	}
	pending := max(0, total-completed-1)
	if completed == total {
		pending = 0
	}
	fmt.Fprintf(&b, "Completed: %d/%d · Pending after current: %d\n\n", completed, total, pending)
	if w.Recipe.AllowSelfTest {
		b.WriteString("Implementer/tester independence: explicitly relaxed by this recipe\n\n")
	}
	for i, stage := range w.Recipe.Stages {
		state := "pending"
		if i < completed {
			state = "completed"
		} else if i == w.Current {
			state = w.State
		}
		assignment := stage.Kind
		if stage.Role != "" {
			assignment += " · " + stage.Role
		}
		if stage.Agent != "" {
			assignment += " · " + stage.Agent
			model := stage.Model
			provider, err := (core.Config{Providers: w.Providers}).Provider(stage.Agent)
			if model == "" && err == nil {
				model = provider.DefaultModel
			}
			if model == "" {
				model = "provider default"
			}
			assignment += " / " + model
			if err == nil {
				assignment += "\n   Capabilities: " + strings.Join(provider.Capabilities, ", ")
			} else {
				assignment += "\n   Unavailable: " + err.Error()
			}
		}
		if len(stage.Skills) > 0 {
			assignment += "\n   Pinned skills: " + strings.Join(stage.Skills, ", ")
		}
		fmt.Fprintf(&b, "%d. %s · %s\n   %s\n", i+1, stage.Name, state, assignment)
	}
	approval := "No remaining approval gates."
	for i := completed; i < total; i++ {
		if w.Recipe.Stages[i].Kind == "approval" {
			approval = "Upcoming approval: " + w.Recipe.Stages[i].Name
			if i == w.Current && w.State == "awaiting-approval" {
				approval = "Approval needed now: " + w.Recipe.Stages[i].Name + " · review the details before deciding"
			}
			break
		}
	}
	fmt.Fprintf(&b, "\n%s\n", approval)
	if len(w.Results) > 0 {
		last := w.Results[len(w.Results)-1]
		line := strings.SplitN(strings.TrimSpace(last.Output), "\n", 2)[0]
		fmt.Fprintf(&b, "Latest result: %s · attempt %d · %s\n%s\n", last.Stage, last.Attempt, last.State, line)
	}
	passed := 0
	for _, item := range evidence {
		if item.Passed {
			passed++
		}
	}
	fmt.Fprintf(&b, "Recorded checks: %d passed / %d runs (historical; revalidated before closure)\nTracked issues: %d\n", passed, len(evidence), len(w.Issues))
	return b.String()
}
