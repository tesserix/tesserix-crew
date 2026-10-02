// SPDX-License-Identifier: Apache-2.0
package core

import "encoding/json"

func normalizeGrok(raw []byte) AgentEvent {
	var v struct {
		Text  string          `json:"text"`
		Stop  string          `json:"stopReason"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return AgentEvent{Error: "Grok returned malformed result JSON"}
	}
	if (len(v.Error) > 0 && string(v.Error) != "null") || v.Stop != "end_turn" || v.Text == "" {
		return AgentEvent{Error: "Grok did not return a completed response"}
	}
	return AgentEvent{Text: v.Text}
}
