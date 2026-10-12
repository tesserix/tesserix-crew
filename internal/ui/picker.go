// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type Choice struct {
	ID, Label, Detail string
}

// picker is shared by setup, session selection and the in-session preset menu.
type picker struct {
	title    string
	items    []Choice
	input    textinput.Model
	cursor   int
	width    int
	height   int
	selected string
	done     bool
}

func newPicker(title string, choices []Choice) picker {
	input := textinput.New()
	input.Placeholder = "Search…"
	input.Prompt = "/ "
	input.CharLimit = 256
	input.Focus()
	return picker{title: title, items: choices, input: input, width: 80, height: 20}
}

func (p picker) filtered() []Choice {
	query := strings.ToLower(strings.TrimSpace(p.input.Value()))
	out := []Choice{}
	for _, item := range p.items {
		if strings.Contains(strings.ToLower(item.ID+" "+item.Label+" "+item.Detail), query) {
			out = append(out, item)
		}
	}
	if query != "" {
		rank := func(item Choice) int {
			if strings.EqualFold(item.ID, query) {
				return 0
			}
			if strings.HasPrefix(strings.ToLower(item.Label), query) {
				return 1
			}
			return 2
		}
		sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	}
	return out
}

func (p picker) Init() tea.Cmd { return textinput.Blink }
func (p picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		p.width, p.height = size.Width, size.Height
		p.input.Width = max(1, size.Width-8)
		return p, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "ctrl+c", "ctrl+d":
			p.done = true
			return p, tea.Quit
		case "up", "ctrl+p":
			p.cursor = max(0, p.cursor-1)
			return p, nil
		case "down", "ctrl+n":
			p.cursor = min(max(0, len(p.filtered())-1), p.cursor+1)
			return p, nil
		case "enter":
			items := p.filtered()
			if len(items) > 0 {
				p.selected, p.done = items[min(p.cursor, len(items)-1)].ID, true
				return p, tea.Quit
			}
			return p, nil
		}
	}
	old := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != old {
		p.cursor = 0
	}
	return p, cmd
}

func (p picker) View() string {
	width := max(1, min(100, p.width-4))
	height := max(1, p.height-2)
	var b strings.Builder
	b.WriteString(ansi.Truncate(p.title, width, "…") + "\n" + p.input.View() + "\n\n")
	items := p.filtered()
	if len(items) == 0 {
		b.WriteString("No matching entries. Change the search or press Esc.\n")
	} else {
		rowsPerItem := 3
		for _, item := range items {
			rowsPerItem = max(rowsPerItem, 2+len(strings.Split(item.Detail, "\n")))
		}
		visible := max(1, (height-7)/rowsPerItem)
		start := max(0, p.cursor-visible+1)
		end := min(len(items), start+visible)
		for i := start; i < end; i++ {
			marker := "  "
			if i == p.cursor {
				marker = "› "
			}
			b.WriteString(ansi.Truncate(marker+ansi.Strip(items[i].Label), width, "…") + "\n")
			for _, detail := range strings.Split(items[i].Detail, "\n") {
				b.WriteString(ansi.Truncate("  "+strings.Join(strings.Fields(ansi.Strip(detail)), " "), width, "…") + "\n")
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%d/%d\n", p.cursor+1, len(items))
	}
	b.WriteString(ansi.Truncate("Esc cancel · Enter select · ↑/↓ choose · type to search", width, "…"))
	return lipgloss.NewStyle().Width(width).MaxWidth(width).MaxHeight(height).Padding(0, 1).Render(b.String())
}

// Select returns an empty ID when cancelled; callers must not apply changes then.
func Select(ctx context.Context, title string, choices []Choice) (string, error) {
	result, err := tea.NewProgram(newPicker(title, choices), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if err != nil {
		return "", err
	}
	return result.(picker).selected, nil
}

type panel struct {
	kind, title, preset string
	state               string
	content             string
	picker              picker
	input               textinput.Model
	viewport            viewport.Model
	workflowID          string
	revision            int
}
