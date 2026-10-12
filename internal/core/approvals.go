// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type ToolApproval struct {
	ID    string          `json:"id"`
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}
type ApprovalHandler func(context.Context, ToolApproval) (bool, error)

// Claude's SDK host protocol grants only one invocation. Crew does not forward
// permission suggestions or persist broader allow rules.
func handleClaudeControl(ctx context.Context, raw []byte, input io.Writer, approve ApprovalHandler) (bool, error) {
	var request struct {
		Type    string `json:"type"`
		ID      string `json:"request_id"`
		Request struct {
			Subtype string          `json:"subtype"`
			Tool    string          `json:"tool_name"`
			Input   json.RawMessage `json:"input"`
		} `json:"request"`
	}
	if json.Unmarshal(raw, &request) != nil || request.Type != "control_request" {
		return false, nil
	}
	if request.ID == "" {
		return true, fmt.Errorf("native approval request has no ID")
	}
	response := map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": request.ID, "error": "Crew does not support this native control request"}}
	if request.Request.Subtype == "can_use_tool" && request.Request.Tool != "" && json.Valid(request.Request.Input) {
		allowed := false
		if approve != nil {
			var err error
			allowed, err = approve(ctx, ToolApproval{ID: request.ID, Tool: request.Request.Tool, Input: request.Request.Input})
			if err != nil {
				return true, err
			}
		}
		decision := map[string]any{"behavior": "deny", "message": "Denied by the Crew user"}
		if allowed {
			decision = map[string]any{"behavior": "allow", "updatedInput": request.Request.Input}
		}
		response = map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": request.ID, "response": decision}}
	}
	return true, json.NewEncoder(input).Encode(response)
}

func PermissionRecovery(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "permission") || strings.Contains(text, "approval") || strings.Contains(text, "sandbox") {
		return "Inspect partial changes and /context before retrying. Use --allow-edits for an approved edit task. Claude can use --native-approvals in the terminal; other providers may require continuing in their native CLI. Crew will not bypass native permissions or retry automatically."
	}
	return ""
}
