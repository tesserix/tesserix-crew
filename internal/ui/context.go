// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"fmt"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"strconv"
	"strings"
)

func (m *model) contextSlash(fields []string) tea.Cmd {
	if m.runner == nil {
		return nil
	}
	action := "status"
	if len(fields) > 1 {
		action = fields[1]
	}
	var err error
	switch action {
	case "status":
		usage, e := m.runner.ContextUsage(m.session)
		err = e
		if err == nil {
			m.add(fmt.Sprintf("Context\nFull handoff: %d / %d bytes (%d%%) · pins %d · summary applied=%v\nUse /context draft to prepare a reviewable summary, /context pin TEXT for a decision, or /context search WORDS for history.", usage.Bytes, usage.Budget, usage.Percent, usage.Pins, usage.SummaryApplied))
		}
	case "draft":
		v, e := m.runner.Store.DraftContext(m.session.ID, "")
		err = e
		if err == nil {
			m.add("Summary draft (inactive)\n" + v.Content + "\nEdit/import a reviewed version with crew context draft --file PATH SESSION. /context apply reviews the saved draft before confirmation.")
		}
	case "apply":
		v, e := m.runner.Store.ContextDraft(m.session.ID)
		err = e
		if err == nil && v.Content == "" {
			err = fmt.Errorf("create a summary draft first")
		}
		if err == nil {
			m.panel = &panel{kind: "summary-review", title: "Review summary · Enter opens confirmation · Esc cancels", content: v.Content, workflowID: m.session.ID, preset: v.Digest(), viewport: viewport.New(80, 16)}
			m.resizePanel()
		}
	case "pin":
		var id int64
		id, err = m.runner.Store.PinContext(m.session.ID, m.runner.Config.Redact(strings.Join(fields[2:], " ")))
		if err == nil {
			m.add(fmt.Sprintf("Pinned decision %d", id))
		}
	case "unpin":
		if len(fields) != 3 {
			err = fmt.Errorf("usage: /context unpin PIN_ID")
			break
		}
		var id int64
		id, err = strconv.ParseInt(fields[2], 10, 64)
		if err == nil {
			err = m.runner.Store.UnpinContext(m.session.ID, id)
		}
	case "pins":
		pins, e := m.runner.Store.ContextPins(m.session.ID)
		err = e
		for _, pin := range pins {
			m.add(fmt.Sprintf("Pin %d\n%s", pin.ID, pin.Content))
		}
	case "clear":
		err = m.runner.Store.ClearContextSummary(m.session.ID)
		if err == nil {
			m.add("Summary cleared. Full history will be supplied again.")
		}
	case "search":
		events, e := m.runner.Store.SearchContext(m.session.ID, strings.Join(fields[2:], " "), 20)
		err = e
		for _, e := range events {
			m.add(fmt.Sprintf("%s / %s · %s\n%s", e.Kind, e.Agent, e.Created, e.Content))
		}
	default:
		err = fmt.Errorf("usage: /context [status|draft|apply|clear|pin TEXT|pins|unpin ID|search TEXT]")
	}
	if err != nil {
		m.add("Error\n" + err.Error())
	}
	return nil
}
