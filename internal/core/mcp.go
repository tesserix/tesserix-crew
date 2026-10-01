// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func rpcResult(w io.Writer, id json.RawMessage, result any) error {
	return json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}
func rpcError(w io.Writer, id json.RawMessage, code int, message string) error {
	return json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
func toolResult(text string, failed bool) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": failed}
}

// ServeMCP uses newline-delimited JSON-RPC over stdio. Stdout is protocol-only.
// Delegation is synchronous and bounded; children cannot expose delegation tools.
func (r *Runner) ServeMCP(ctx context.Context, s Session, depth int, input io.Reader, output io.Writer) error {
	if depth != 0 {
		return fmt.Errorf("delegation MCP is available only to a parent agent")
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 65536), 1024*1024)
	for scanner.Scan() {
		var req rpcRequest
		if e := json.Unmarshal(scanner.Bytes(), &req); e != nil {
			if e = rpcError(output, json.RawMessage("null"), -32700, "Invalid JSON"); e != nil {
				return e
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			var p struct {
				Protocol string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &p)
			version := "2024-11-05"
			for _, supported := range []string{"2024-11-05", "2025-03-26", "2025-06-18"} {
				if p.Protocol == supported {
					version = p.Protocol
				}
			}
			result = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "tesserix-crew", "version": "0.1.0"}}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{"name": "delegate", "description": "Ask another subscribed agent for a bounded read-only task: research, content, code suggestions or review. Returns the child session ID and result; the parent integrates changes. No recursive delegation.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"agent": map[string]any{"type": "string", "enum": []string{"claude", "codex", "gemini"}}, "task": map[string]any{"type": "string"}}, "required": []string{"agent", "task"}, "additionalProperties": false}},
				map[string]any{"name": "session_context", "description": "Read the current Crew session transcript, including delegation results.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
			}}
		case "tools/call":
			var p struct {
				Name      string `json:"name"`
				Arguments struct {
					Agent string `json:"agent"`
					Task  string `json:"task"`
				} `json:"arguments"`
			}
			if e := json.Unmarshal(req.Params, &p); e != nil {
				result = toolResult(e.Error(), true)
				break
			}
			switch p.Name {
			case "session_context":
				skills, e := r.Skills(s)
				if e != nil {
					result = toolResult(e.Error(), true)
					break
				}
				text, e := r.Context(s, skills)
				if e != nil {
					result = toolResult(e.Error(), true)
				} else {
					result = toolResult(text, false)
				}
			case "delegate":
				if !ValidAgent(p.Arguments.Agent) || strings.TrimSpace(p.Arguments.Task) == "" {
					result = toolResult("delegate requires a valid agent and nonempty task", true)
					break
				}
				text, e := r.Delegate(ctx, s, p.Arguments.Agent, p.Arguments.Task)
				if e != nil {
					result = toolResult(e.Error(), true)
				} else {
					result = toolResult(text, false)
				}
			default:
				result = toolResult("Unknown tool: "+p.Name, true)
			}
		default:
			if e := rpcError(output, req.ID, -32601, "Method not found"); e != nil {
				return e
			}
			continue
		}
		if e := rpcResult(output, req.ID, result); e != nil {
			return e
		}
	}
	return scanner.Err()
}
func (r *Runner) Delegate(ctx context.Context, parent Session, agent, task string) (string, error) {
	child, e := r.Store.Create(parent.Repo, agent, parent.ID)
	if e != nil {
		return "", e
	}
	skills, e := r.Skills(parent)
	if e != nil {
		return "", e
	}
	if e = r.Store.SetSkills(child.ID, skills); e != nil {
		return "", e
	}
	briefing, e := r.Context(parent, skills)
	if e != nil {
		return "", e
	}
	if e = r.Store.Append(child.ID, "delegation", parent.Agent, "Parent context:\n"+briefing); e != nil {
		return "", e
	}
	if e = r.Store.Append(parent.ID, "delegation", agent, "Started child "+child.ID+": "+task); e != nil {
		return "", e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*60*1e9)
	defer cancel()
	text, runErr := r.Run(ctx, child, RunOptions{Agent: agent, Task: task, Depth: 1}, func(Update) {})
	summary := "Child session: " + child.ID + "\n" + text
	if runErr != nil {
		summary += "\nFailed: " + runErr.Error()
	}
	if e = r.Store.Append(parent.ID, "delegation", agent, summary); e != nil {
		return "", e
	}
	if runErr != nil {
		return summary, runErr
	}
	return summary, nil
}
