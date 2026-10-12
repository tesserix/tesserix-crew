// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tesserix/tesserix-crew/internal/core"
	"strings"
	"time"
)

type modelsMsg struct {
	agent   string
	choices []core.ModelChoice
	err     error
}

func (m *model) openAgents() tea.Cmd {
	if m.runner == nil {
		m.input.SetValue("/agent ")
		m.input.CursorEnd()
		m.add("Agents\n" + strings.Join(append(m.providerNames(), "auto"), " · "))
		return nil
	}
	choices := []Choice{}
	for _, name := range m.runner.Config.ProviderNames() {
		if err := m.runner.Config.CheckProvider(core.AgentOptions{Agent: name, Repo: m.session.Repo, Edits: m.options.Edits}); err != nil {
			continue
		}
		p, _ := m.runner.Config.Provider(name)
		choices = append(choices, Choice{ID: name, Label: name, Detail: strings.Join(p.Capabilities, ", ")})
	}
	m.panel = &panel{kind: "agents", picker: newPicker("Choose available provider · filtered for the current edit mode", choices)}
	m.resizePanel()
	return textinput.Blink
}
func (m *model) openModels() tea.Cmd {
	if m.runner == nil {
		return nil
	}
	name := m.options.Agent
	if name == "auto" || name == "" {
		name = m.session.Agent
	}
	config := m.runner.Config
	m.add("Models\nLoading catalog for " + name + "…")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		choices, err := config.ModelChoices(ctx, name)
		return modelsMsg{name, choices, err}
	}
}
func (m *model) receiveModels(v modelsMsg) tea.Cmd {
	name := m.options.Agent
	if name == "auto" || name == "" {
		name = m.session.Agent
	}
	if name != v.agent || m.busy {
		return nil
	}
	if v.err != nil {
		m.add("Model discovery unavailable\n" + v.err.Error() + "\nUse /model NAME with the provider's native identifier.")
		return nil
	}
	choices := []Choice{{ID: "__crew_default", Label: "Provider default", Detail: "Clear the model override"}}
	for _, item := range v.choices {
		choices = append(choices, Choice{ID: item.ID, Label: item.Label, Detail: item.Description})
	}
	m.panel = &panel{kind: "models", picker: newPicker("Choose model for "+name, choices)}
	m.resizePanel()
	return textinput.Blink
}
