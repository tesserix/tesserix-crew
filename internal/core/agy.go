// SPDX-License-Identifier: Apache-2.0
package core

import (
	"encoding/json"
	"fmt"
)

// AGY uses event/step_update/result envelopes rather than Claude's type events.
func normalizeAGY(raw []byte) AgentEvent {
	var v struct {
		Event        string `json:"event"`
		Conversation string `json:"conversation_id"`
		Step         struct {
			Type  string `json:"step_type"`
			State string `json:"state"`
			Delta string `json:"text_delta"`
		} `json:"step_update"`
		Result struct {
			Conversation string            `json:"conversation_id"`
			Status       string            `json:"status"`
			Response     string            `json:"response"`
			Denied       []json.RawMessage `json:"denied_actions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return AgentEvent{Error: "AGY returned malformed event JSON"}
	}
	switch v.Event {
	case "init":
		return AgentEvent{Native: v.Conversation, Status: "connected · preparing response"}
	case "step_update":
		if v.Step.Type == "agent_response" {
			return AgentEvent{Text: v.Step.Delta, Delta: true}
		}
		return AgentEvent{Status: "agy · " + v.Step.Type + " · " + v.Step.State}
	case "result":
		if len(v.Result.Denied) > 0 {
			return AgentEvent{Error: "AGY denied tool permissions; use native permission settings to review the requested action"}
		}
		if v.Result.Status != "SUCCESS" {
			return AgentEvent{Error: fmt.Sprintf("AGY result: %s", v.Result.Status)}
		}
		return AgentEvent{Native: v.Result.Conversation, Text: v.Result.Response}
	default:
		return AgentEvent{Error: "AGY returned unsupported event: " + v.Event}
	}
}
