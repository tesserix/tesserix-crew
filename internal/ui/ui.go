// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tesserix/tesserix-crew/internal/core"
)

type progressMsg core.Update
type finishMsg struct{ err error }
type tickMsg time.Time
type repoStatusMsg struct {
	branch                       string
	changes                      int
	delegation                   string
	seq                          int64
	workflowState, workflowStage string
}
type model struct {
	runner                       *core.Runner
	session                      core.Session
	options                      core.RunOptions
	input                        textinput.Model
	output                       viewport.Model
	lines                        []string
	width, height                int
	busy                         bool
	status, branch               string
	workflowState, workflowStage string
	changes, skillCount          int
	started                      time.Time
	turnStarted                  time.Time
	timing                       core.TurnTiming
	cancel                       context.CancelFunc
	messages                     chan tea.Msg
	base                         context.Context
	streaming                    bool
	streamLine                   int
	lastActivity                 time.Time
	lastDelegation               int64
	spinner                      spinner.Model
}

func Launch(ctx context.Context, r *core.Runner, s core.Session, o core.RunOptions) error {
	input := textinput.New()
	input.Placeholder = "Describe a task, or /help"
	input.Prompt = "> "
	input.PromptStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Foreground(foreground)
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(muted)
	input.Focus()
	input.CharLimit = 100000
	m := model{runner: r, session: s, options: o, input: input, output: viewport.New(80, 16), status: "ready", started: time.Now(), base: ctx}
	m.spinner = spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(accent)))
	m.branch, m.changes = core.GitStatus(s.Repo)
	skills, e := r.Skills(s)
	if e != nil {
		return e
	}
	m.skillCount = len(skills)
	events, e := r.Store.Events(s.ID)
	if e != nil {
		return e
	}
	m.lines = []string{}
	for _, event := range events {
		if event.Kind == "timing" {
			_ = json.Unmarshal([]byte(event.Content), &m.timing)
		}
		if event.Kind == "user" || event.Kind == "assistant" || event.Kind == "error" {
			label := event.Agent
			if event.Kind == "user" {
				label = "You"
			}
			if event.Kind == "error" {
				label = "Error"
			}
			m.lines = append(m.lines, label+"\n"+event.Content)
		}
	}
	if state, stage, err := r.Store.WorkflowStatus(s.ID); err == nil {
		m.workflowState, m.workflowStage = state, stage
	}
	m.refresh()
	_, e = tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return e
}
func tick() tea.Cmd           { return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }
func (m model) Init() tea.Cmd { return tea.Batch(textinput.Blink, tick(), m.spinner.Tick) }
func (m *model) refresh() {
	follow := m.output.AtBottom() || m.output.TotalLineCount() == 0
	m.output.SetContent(m.transcript())
	if follow {
		m.output.GotoBottom()
	}
}
func (m *model) add(text string)     { m.lines = append(m.lines, text); m.refresh() }
func wait(ch <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case catalogEditedMsg:
		if v.err != nil {
			m.add(v.err.Error())
		} else {
			m.add("Skill catalog edited; session revisions remain pinned.")
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(v)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.output.Width = max(1, min(100, v.Width-4))
		m.output.Height = max(1, v.Height-8)
		m.input.Width = max(1, v.Width-10)
		m.refresh()
	case repoStatusMsg:
		m.branch, m.changes = v.branch, v.changes
		m.workflowState, m.workflowStage = v.workflowState, v.workflowStage
		if m.busy && v.seq > m.lastDelegation {
			m.status = v.delegation
			m.lastDelegation = v.seq
		}
		return m, tick()
	case tickMsg:
		repo, busy, runner, session, after := m.session.Repo, m.busy, m.runner, m.session.ID, m.lastDelegation
		return m, func() tea.Msg {
			branch, changes := core.GitStatus(repo)
			result := repoStatusMsg{branch: branch, changes: changes}
			if runner != nil {
				if state, stage, err := runner.Store.WorkflowStatus(session); err == nil {
					result.workflowState, result.workflowStage = state, stage
				}
			}
			if busy && runner != nil {
				event, err := runner.Store.LatestDelegation(session, after)
				if err == nil {
					result.delegation = event.Agent + " · " + strings.Split(event.Content, "\n")[0]
					result.seq = event.Seq
				}
			}
			return result
		}
	case progressMsg:
		if v.Kind == "timing" {
			_ = json.Unmarshal([]byte(v.Text), &m.timing)
			return m, wait(m.messages)
		}
		m.lastActivity = time.Now()
		if v.Kind == "delta" {
			if !m.streaming {
				m.add(v.Agent + "\n")
				m.streamLine = len(m.lines) - 1
				m.streaming = true
			}
			m.lines[m.streamLine] += v.Text
			m.refresh()
		} else if v.Kind == "text_end" {
			m.streaming = false
		} else if v.Kind == "text" {
			m.streaming = false
			m.add(v.Agent + "\n" + v.Text)
		} else {
			if strings.HasPrefix(v.Text, "tool:") && v.Text != m.status {
				m.add("↳ " + v.Agent + " · " + v.Text)
			}
			m.status = v.Text
		}
		m.session.Agent = v.Agent
		return m, wait(m.messages)
	case finishMsg:
		if m.runner != nil {
			if state, stage, err := m.runner.Store.WorkflowStatus(m.session.ID); err == nil {
				m.workflowState, m.workflowStage = state, stage
			}
		}
		if m.cancel != nil {
			m.cancel()
		}
		m.cancel = nil
		m.busy = false
		m.streaming = false
		if len(m.lines) > 0 {
			m.refresh()
		}
		m.status = "completed"
		if v.err != nil {
			if errors.Is(v.err, context.Canceled) {
				m.status = "cancelled"
				m.add("Task cancelled. Context is saved; enter another task or switch agents.")
			} else {
				m.status = "failed"
				m.add("Error\n" + v.err.Error())
			}
		}
		m.input.Focus()
		return m, textinput.Blink
	case tea.KeyMsg:
		if v.String() == "tab" && !m.busy {
			value := m.input.Value()
			for _, candidate := range m.completions() {
				if strings.HasPrefix(candidate, value) && candidate != value {
					m.input.SetValue(candidate)
					m.input.CursorEnd()
					return m, nil
				}
			}
		}
		if v.String() == "ctrl+c" {
			if m.busy {
				m.cancel()
				m.status = "cancelling"
				return m, nil
			}
			return m, tea.Quit
		}
		if v.String() == "ctrl+d" && !m.busy {
			return m, tea.Quit
		}
		if v.String() == "enter" && !m.busy {
			task := strings.TrimSpace(m.input.Value())
			if task == "" {
				return m, nil
			}
			m.input.SetValue("")
			if strings.HasPrefix(task, "/") {
				return m, m.slash(task)
			}
			m.add("You\n" + task)
			m.busy = true
			m.input.Blur()
			m.status = "starting"
			m.turnStarted = time.Now()
			m.timing = core.TurnTiming{}
			m.lastActivity = time.Now()
			m.messages = make(chan tea.Msg, 64)
			ctx, cancel := context.WithCancel(m.base)
			m.cancel = cancel
			ch := m.messages
			r := m.runner
			s := m.session
			o := m.options
			o.Task = task
			go func() {
				_, e := r.Run(ctx, s, o, func(u core.Update) {
					select {
					case ch <- progressMsg(u):
					case <-ctx.Done():
					}
				})
				select {
				case ch <- finishMsg{e}:
				case <-m.base.Done():
				}
			}()
			return m, wait(ch)
		}
	}
	var commands []tea.Cmd
	if !m.busy {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		commands = append(commands, cmd)
	}
	var cmd tea.Cmd
	m.output, cmd = m.output.Update(msg)
	commands = append(commands, cmd)
	return m, tea.Batch(commands...)
}
func (m *model) slash(text string) tea.Cmd {
	fields := strings.Fields(text)
	switch fields[0] {
	case "/quit", "/exit":
		return tea.Quit
	case "/help":
		m.add("/agent NAME|auto · /model NAME · /status · /context · /skills · /providers · /workflow · /quit\nCtrl+C cancels a running task. PgUp/PgDown scroll. /agent selects the next turn; handoff is sent on that turn.")
	case "/agent":
		if len(fields) == 1 {
			m.input.SetValue("/agent ")
			m.input.CursorEnd()
			m.add("Agents\n" + strings.Join(append(m.providerNames(), "auto"), " · ") + "\nType /agent NAME to select the next provider. Shared context follows you.")
			break
		}
		if len(fields) != 2 || (!m.validProvider(fields[1]) && fields[1] != "auto") {
			m.add("Usage: /agent NAME|auto")
			break
		}
		m.options.Agent = fields[1]
		m.options.Model = ""
		m.session.Agent = fields[1]
		if m.runner != nil && fields[1] != "auto" {
			if err := m.runner.Store.Switch(m.session.ID, fields[1]); err != nil {
				m.add("Could not persist agent selection: " + err.Error())
			}
		}
		m.add("Next agent: " + fields[1] + " · model reset; shared context will accompany the next task")
	case "/model":
		if len(fields) != 2 {
			m.add("Usage: /model NAME")
		} else {
			m.options.Model = fields[1]
			m.add("Model: " + fields[1])
		}
	case "/status":
		m.add(fmt.Sprintf("Session %s\nRepository %s\nAgent %s · branch %s · changed entries %d", m.session.ID, m.session.Repo, m.session.Agent, m.branch, m.changes))
		if m.timing.TotalMS > 0 {
			m.add(fmt.Sprintf("Last turn\nPreparation %.1fs · first provider event %.1fs · first output %.1fs · total %.1fs\nContext sent: %d bytes", float64(m.timing.PrepareMS)/1000, float64(m.timing.FirstEventMS)/1000, float64(m.timing.FirstOutputMS)/1000, float64(m.timing.TotalMS)/1000, m.timing.ContextBytes))
		}
	case "/skills":
		return m.skillSlash(fields)
	case "/workflow":
		return m.workflowSlash(fields)
	case "/providers":
		if m.runner != nil {
			for _, name := range m.runner.Config.ProviderNames() {
				p := m.runner.Config.Providers[name]
				state := "configured"
				if p.Disabled {
					state = "disabled/unverified"
				}
				m.add(name + " · " + p.Auth + " · " + state)
			}
		}
	case "/context":
		skills, e := m.runner.Skills(m.session)
		if e != nil {
			m.add(e.Error())
			break
		}
		text, e := m.runner.Context(m.session, skills)
		if e != nil {
			m.add(e.Error())
		} else {
			m.add(text)
		}
	default:
		m.add("Unknown command; use /help")
	}
	return nil
}
func (m model) View() string {
	if m.width == 0 {
		return "Starting Crew…"
	}
	return m.render()
}

func (m model) validProvider(name string) bool {
	config := core.Config{}
	if m.runner != nil {
		config = m.runner.Config
	}
	_, err := config.Provider(name)
	return err == nil
}
