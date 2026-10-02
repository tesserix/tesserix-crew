// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	accent     = lipgloss.AdaptiveColor{Light: "26", Dark: "75"}
	foreground = lipgloss.AdaptiveColor{Light: "235", Dark: "252"}
	muted      = lipgloss.AdaptiveColor{Light: "243", Dark: "244"}
	edge       = lipgloss.AdaptiveColor{Light: "250", Dark: "238"}
	success    = lipgloss.AdaptiveColor{Light: "28", Dark: "114"}
	danger     = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
)

func (m model) transcript() string {
	width := max(1, min(100, m.output.Width))
	if len(m.lines) == 0 {
		title := lipgloss.NewStyle().Bold(true).Foreground(foreground).Render("One session. Your coding crew.")
		subtitle := lipgloss.NewStyle().Foreground(muted).Render("Describe what you want to build, fix, or explore.")
		examples := []string{
			"Explain how this repository works",
			"Review the latest changes",
			"Find the cause of the failing tests",
		}
		var b strings.Builder
		b.WriteString(title + "\n" + subtitle + "\n\n")
		for _, example := range examples {
			b.WriteString(lipgloss.NewStyle().Foreground(muted).Render("  › "+example) + "\n")
		}
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(accent).Render("/agent codex") + "  " + lipgloss.NewStyle().Foreground(muted).Render("switch agent") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(accent).Render("/skills") + "       " + lipgloss.NewStyle().Foreground(muted).Render("inspect shared skills") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(accent).Render("/help") + "         " + lipgloss.NewStyle().Foreground(muted).Render("all commands"))
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}
	blocks := make([]string, 0, len(m.lines))
	for _, raw := range m.lines {
		raw = ansi.Strip(raw)
		label, body, hasBody := strings.Cut(raw, "\n")
		if strings.HasPrefix(label, "↳") {
			blocks = append(blocks, lipgloss.NewStyle().Foreground(muted).Width(width).Render(raw))
			continue
		}
		color := muted
		switch label {
		case "You":
			color = accent
		case "claude", "codex", "gemini":
			color = success
		case "Error":
			color = danger
		}
		if !hasBody {
			blocks = append(blocks, lipgloss.NewStyle().Foreground(muted).Width(width).Render(raw))
			continue
		}
		heading := lipgloss.NewStyle().Bold(true).Foreground(color).Render("● " + label)
		content := renderBody(body, max(1, width-2))
		blocks = append(blocks, heading+"\n"+lipgloss.NewStyle().PaddingLeft(2).Render(content))
	}
	return strings.Join(blocks, "\n\n")
}

// Keep code visibly separate while retaining its whitespace and copyable text.
func renderBody(body string, width int) string {
	var lines []string
	code := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			code = !code
			language := strings.TrimPrefix(strings.TrimSpace(line), "```")
			if code {
				if language == "" {
					language = "code"
				}
				lines = append(lines, lipgloss.NewStyle().Foreground(muted).Width(width).Render("┌ "+language))
			} else {
				lines = append(lines, lipgloss.NewStyle().Foreground(muted).Render("└"))
			}
			continue
		}
		style := lipgloss.NewStyle().Foreground(foreground).Width(width)
		if code {
			style = style.Foreground(accent).PaddingLeft(2).Width(width)
		} else if strings.HasPrefix(line, "#") {
			line = strings.TrimLeft(line, "# ")
			style = style.Bold(true)
		}
		lines = append(lines, style.Render(line))
	}
	return strings.Join(lines, "\n")
}

func (m model) render() string {
	width := max(1, m.width)
	brand := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("◈ Tesserix Crew")
	hint := lipgloss.NewStyle().Foreground(muted).Render("Tab complete · /help · PgUp/PgDn")
	inner := max(1, width-4)
	header := brand
	if inner >= lipgloss.Width(brand)+lipgloss.Width(hint)+4 {
		header += strings.Repeat(" ", inner-lipgloss.Width(brand)-lipgloss.Width(hint)) + hint
	}
	header = lipgloss.NewStyle().Padding(0, 2).Width(width).MaxWidth(width).Render(header)

	input := m.input.View()
	if m.busy {
		input = lipgloss.NewStyle().Foreground(muted).Render("Working…  Ctrl+C to cancel")
	}
	borderColor := edge
	if m.busy {
		borderColor = accent
	}
	inputBox := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderColor).Padding(0, 1).Width(max(1, width-6)).Render(input)
	inputBox = lipgloss.NewStyle().Padding(0, 2).MaxWidth(width).Render(inputBox)

	state := m.status
	if !m.busy && m.workflowState != "" {
		state = m.workflowStage + " · " + m.workflowState
	}
	stateColor := muted
	indicator := "○"
	switch state {
	case "completed":
		indicator = "✓"
		stateColor = success
	case "failed":
		indicator = "!"
		stateColor = danger
	case "cancelled":
		indicator = "–"
	}
	if m.busy {
		indicator = m.spinner.View()
		stateColor = accent
	}
	state = lipgloss.NewStyle().Foreground(stateColor).Render(indicator + " " + state)
	if m.busy && !m.lastActivity.IsZero() {
		state += " · last event " + time.Since(m.lastActivity).Round(time.Second).String() + " ago"
	}
	agent := m.session.Agent
	if m.options.Agent == "auto" {
		agent += " (auto)"
	}
	if m.options.Model != "" {
		agent += " / " + m.options.Model
	}
	mode := "read-only"
	if m.options.Edits {
		mode = "edits enabled"
	}
	top := fmt.Sprintf("%s · %s · %d changed   |   %s", filepath.Base(m.session.Repo), m.branch, m.changes, agent)
	bottom := fmt.Sprintf("%s   |   %s · skills %d · %s · Session %s", state, mode, m.skillCount, time.Since(m.started).Round(time.Second), m.session.ID)
	// Clamp before rendering so narrow terminals retain a stable two-row footer.
	top = ansi.Truncate(top, inner, "…")
	bottom = ansi.Truncate(bottom, inner, "…")
	footer := lipgloss.NewStyle().Foreground(muted).Padding(0, 2).Width(width).MaxWidth(width).Render(top + "\n" + bottom)
	conversation := lipgloss.NewStyle().Padding(0, 2).MaxWidth(width).Render(m.output.View())
	return lipgloss.JoinVertical(lipgloss.Left, header, "", conversation, "", inputBox, footer)
}
