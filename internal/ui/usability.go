// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tesserix/tesserix-crew/internal/core"
)

func (m *model) newSession() tea.Cmd {
	if m.runner == nil || m.busy {
		return nil
	}
	runner := *m.runner
	config, err := core.LoadConfig(runner.Home, m.session.Repo)
	if err != nil {
		m.add("Could not start session: " + err.Error())
		return nil
	}
	runner.Config = config
	session, err := runner.Store.Create(m.session.Repo, m.session.Agent, "")
	if err != nil {
		m.add("Could not start session: " + err.Error())
		return nil
	}
	next, err := newModel(m.base, &runner, session, m.options)
	if err != nil {
		m.add("Could not open session: " + err.Error())
		return nil
	}
	next.width, next.height = m.width, m.height
	next.output.Width, next.output.Height = m.output.Width, m.output.Height
	next.input.Width = m.input.Width
	next.refresh()
	*m = next
	return textinput.Blink
}

func (m model) progressLabel() string {
	if m.status == "cancelling" {
		return "Cancelling · waiting for the agent to stop"
	}
	if m.pendingApproval != nil {
		return "Waiting for your permission"
	}
	if m.streaming {
		return "Receiving response from " + m.session.Agent
	}
	if m.status == "starting" || m.status == "preparing context" {
		return "Preparing task"
	}
	if m.status == "starting provider" {
		return "Starting " + m.session.Agent
	}
	if m.status == "waiting for model" {
		return "Waiting for " + m.session.Agent + " to respond"
	}
	if strings.HasPrefix(m.status, "tool:") {
		return "Running " + strings.TrimSpace(strings.TrimPrefix(m.status, "tool:"))
	}
	return m.status
}

func (m model) progressText() string {
	text := m.progressLabel()
	if !m.turnStarted.IsZero() {
		text += " · " + time.Since(m.turnStarted).Round(time.Second).String()
	}
	if !m.lastActivity.IsZero() && time.Since(m.lastActivity) >= 30*time.Second {
		text += fmt.Sprintf(" · no new output for %s", time.Since(m.lastActivity).Round(time.Second))
	}
	return text
}
