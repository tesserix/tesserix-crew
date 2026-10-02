// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tesserix/tesserix-crew/internal/core"
)

func TestStatusAndAgentSwitch(t *testing.T) {
	input := textinput.New()
	m := model{input: input, output: viewport.New(100, 8), width: 100, session: core.Session{ID: "abc", Repo: "/tmp/my-repo", Agent: "claude"}, branch: "feature/login", changes: 3, status: "ready", started: time.Now()}
	view := m.View()
	for _, expected := range []string{"my-repo", "feature/login", "3 changed", "claude", "Session abc"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("missing %s in %s", expected, view)
		}
	}
	m.options.Model = "old-model"
	m.slash("/agent codex")
	if m.options.Agent != "codex" || m.options.Model != "" {
		t.Fatal("agent switch must clear provider-specific model")
	}
}

func TestResponsiveLayout(t *testing.T) {
	for _, width := range []int{32, 80, 120} {
		m := model{input: textinput.New(), output: viewport.New(width, 10), session: core.Session{ID: "abc", Repo: "/tmp/my-repo", Agent: "claude"}, branch: "main", status: "ready", started: time.Now()}
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = next.(model)
		view := m.View()
		if lipgloss.Height(view) != 24 {
			t.Fatalf("width %d: height %d, wanted 24", width, lipgloss.Height(view))
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: overflowing line %q", width, line)
			}
		}
		if !strings.Contains(view, "One session.") {
			t.Fatal("missing welcome state")
		}
	}
}
func TestCancelKeepsSessionOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := model{busy: true, cancel: cancel}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if ctx.Err() == nil || cmd != nil || next.(model).status != "cancelling" {
		t.Fatal("active Ctrl+C should cancel, not exit")
	}
}

func TestStreamingTextIsIncremental(t *testing.T) {
	m := model{output: viewport.New(100, 8), messages: make(chan tea.Msg), session: core.Session{Agent: "claude"}}
	next, _ := m.Update(progressMsg{Kind: "delta", Agent: "claude", Text: "Hel"})
	m = next.(model)
	next, _ = m.Update(progressMsg{Kind: "delta", Agent: "claude", Text: "lo"})
	m = next.(model)
	if len(m.lines) != 1 || m.lines[0] != "claude\nHello" {
		t.Fatal(m.lines)
	}
	next, _ = m.Update(progressMsg{Kind: "text_end", Agent: "claude"})
	m = next.(model)
	if m.streaming {
		t.Fatal("stream not closed")
	}
}

func TestWideTerminalUsesReadableColumn(t *testing.T) {
	m := model{input: textinput.New(), output: viewport.New(80, 8)}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 30})
	if next.(model).output.Width != 100 {
		t.Fatal("conversation should be capped at 100 columns")
	}
}
func TestStreamingPreservesScrollPosition(t *testing.T) {
	m := model{output: viewport.New(40, 4), lines: []string{"claude\n" + strings.Repeat("line\n", 30)}}
	m.refresh()
	m.output.GotoTop()
	m.lines[0] += "more\n"
	m.refresh()
	if m.output.YOffset != 0 {
		t.Fatal("streaming moved reader away from older messages")
	}
}
func TestAgentDiscoveryAndCompletion(t *testing.T) {
	m := model{input: textinput.New(), output: viewport.New(80, 8)}
	m.slash("/agent")
	if m.input.Value() != "/agent " || !strings.Contains(m.lines[0], "codex") {
		t.Fatal("missing agent discovery")
	}
	m.input.SetValue("/agent co")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if next.(model).input.Value() != "/agent codex" {
		t.Fatal("missing completion")
	}
}

func TestLifecycleGateVisibleInFooter(t *testing.T) {
	m := model{input: textinput.New(), output: viewport.New(100, 8), width: 120, session: core.Session{ID: "abc", Repo: "/tmp/repo", Agent: "codex"}, workflowStage: "user-review", workflowState: "awaiting-approval", started: time.Now()}
	view := m.View()
	if !strings.Contains(view, "user-review") || !strings.Contains(view, "awaiting-approval") {
		t.Fatal(view)
	}
}
