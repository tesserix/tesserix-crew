// SPDX-License-Identifier: Apache-2.0
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/tesserix/tesserix-crew/internal/core"
)

type approvalMsg struct {
	request core.ToolApproval
	reply   chan bool
	view    viewport.Model
	content string
}

func approvalCallback(messages chan tea.Msg) core.ApprovalHandler {
	return func(ctx context.Context, request core.ToolApproval) (bool, error) {
		reply := make(chan bool, 1)
		select {
		case messages <- approvalMsg{request: request, reply: reply}:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		select {
		case allowed := <-reply:
			return allowed, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func (m *model) prepareApproval() {
	var formatted json.RawMessage = m.pendingApproval.request.Input
	var input any
	if json.Unmarshal(formatted, &input) == nil {
		formatted, _ = json.MarshalIndent(input, "", "  ")
	}
	m.pendingApproval.content = ansi.Strip(m.runner.Config.Redact(string(formatted)))
	m.pendingApproval.view = viewport.New(max(1, m.width-4), max(1, m.height-6))
	m.pendingApproval.view.SetContent(renderBody(m.pendingApproval.content, m.pendingApproval.view.Width))
}
func (m model) approvalView() string {
	text := fmt.Sprintf("Native permission · %s\n\n%s\na allow once · d deny · Ctrl+C cancel · PgUp/PgDn scroll", m.pendingApproval.request.Tool, m.pendingApproval.view.View())
	return lipgloss.NewStyle().Width(max(1, m.width-4)).MaxWidth(max(1, m.width-4)).MaxHeight(max(1, m.height-1)).Padding(0, 1).Render(text)
}
