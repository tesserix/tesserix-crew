// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type apiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type apiMessage struct {
	Role       string        `json:"role"`
	Content    *string       `json:"content"`
	ToolCalls  []apiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Refusal    string        `json:"refusal,omitempty"`
}

func apiText(role, text string) apiMessage { return apiMessage{Role: role, Content: &text} }
func apiTool(name, description string, properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "function", "function": map[string]any{"name": name, "description": description, "parameters": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}}
}
func stringProperty() map[string]any { return map[string]any{"type": "string"} }
func (r *Runner) executeAPI(ctx context.Context, p Provider, o AgentOptions, prompt string, emit func([]byte)) error {
	model := o.Model
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return fmt.Errorf("API provider %s needs a model", o.Agent)
	}
	timeout := 60 * time.Second
	if p.RequestTimeout != "" {
		timeout, _ = time.ParseDuration(p.RequestTimeout)
	}
	client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("API redirects are unsupported") }}
	messages := []apiMessage{apiText("system", "You are a coding agent inside Crew. Use only the supplied repository tools. Do not access Crew state or git internals. Respect read-only permissions and user review gates. Execute pinned checks for concrete test evidence. Do not add AI attribution signatures to commits or GitHub messages."), apiText("user", prompt)}
	tools := []map[string]any{
		apiTool("list_files", "List repository files", map[string]any{}, []string{}),
		apiTool("read_skill_file", "Read a selected pinned skill supporting file", map[string]any{"skill": stringProperty(), "path": stringProperty()}, []string{"skill", "path"}),
		apiTool("read_file", "Read a repository-relative file", map[string]any{"path": stringProperty()}, []string{"path"}),
	}
	allowFileWrites := o.Edits && p.Has("write")
	if workflow, err := r.Store.Workflow(o.Session); err == nil && workflow.Current < len(workflow.Recipe.Stages) && workflow.Recipe.Stages[workflow.Current].Role == "tester" {
		allowFileWrites = false
	}
	if allowFileWrites {
		tools = append(tools, apiTool("write_file", "Replace a repository-relative file; requires edit permission", map[string]any{"path": stringProperty(), "content": stringProperty()}, []string{"path", "content"}))
	}
	if o.Edits && p.Has("tools") {
		tools = append(tools, apiTool("run_check", "Execute a named pinned lifecycle check and save evidence", map[string]any{"check": stringProperty(), "phase": map[string]any{"type": "string", "enum": []string{"red", "green"}}}, []string{"check", "phase"}))
	}
	if o.Depth == 0 && p.Has("delegate") {
		tools = append(tools, apiTool("delegate", "Ask another configured provider for a bounded read-only task", map[string]any{"agent": stringProperty(), "task": stringProperty()}, []string{"agent", "task"}))
	}
	sendEvent := func(kind, text string) {
		data, _ := json.Marshal(map[string]string{"type": kind, "text": r.Config.Redact(text)})
		emit(data)
	}
	maxTokens := p.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	rounds := p.MaxRounds
	if rounds == 0 {
		rounds = 20
	}
	for round := 0; round < rounds; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		sendEvent("status", "API request · waiting for response")
		data, err := json.Marshal(map[string]any{"model": model, "messages": messages, "tools": tools, "max_completion_tokens": maxTokens})
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("cannot create API request")
		}
		request.Header.Set("Content-Type", "application/json")
		if p.Auth == "api-key" {
			request.Header.Set("Authorization", "Bearer "+os.Getenv(p.KeyEnv))
		}
		response, err := client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("API request failed: %s", r.Config.Redact(err.Error()))
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("API provider returned HTTP %d", response.StatusCode)
		}
		if readErr != nil || len(body) > 4*1024*1024 {
			return fmt.Errorf("API response unreadable or exceeds 4 MB")
		}
		var completion struct {
			Choices []struct {
				Message apiMessage `json:"message"`
			} `json:"choices"`
			Usage struct {
				Input  int `json:"prompt_tokens"`
				Output int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(body, &completion); err != nil || len(completion.Choices) != 1 {
			return fmt.Errorf("API provider returned an invalid completion")
		}
		message := completion.Choices[0].Message
		message.Role = "assistant"
		messages = append(messages, message)
		if message.Refusal != "" {
			return fmt.Errorf("API provider declined the task")
		}
		if message.Content != nil && *message.Content != "" {
			sendEvent("text", *message.Content)
		}
		if completion.Usage.Input > 0 || completion.Usage.Output > 0 {
			usage, _ := json.Marshal(map[string]any{"type": "usage", "input_tokens": completion.Usage.Input, "output_tokens": completion.Usage.Output})
			emit(usage)
		}
		if len(message.ToolCalls) == 0 {
			if message.Content == nil || *message.Content == "" {
				return fmt.Errorf("API provider returned no task result")
			}
			return nil
		}
		if len(message.ToolCalls) > 20 {
			return fmt.Errorf("API provider exceeded 20 tool calls in one response")
		}
		seen := map[string]bool{}
		for _, call := range message.ToolCalls {
			if call.Type != "function" || call.ID == "" || seen[call.ID] {
				return fmt.Errorf("API provider returned invalid tool-call identities")
			}
			seen[call.ID] = true
			sendEvent("status", "tool: "+call.Function.Name)
			result, toolErr := r.apiTool(ctx, p, o, call)
			if toolErr != nil {
				result = "Tool error: " + toolErr.Error()
			}
			messages = append(messages, apiMessage{Role: "tool", Content: ptrString(r.Config.Redact(result)), ToolCallID: call.ID})
		}
	}
	return fmt.Errorf("API tool-round limit reached (%d); inspect the saved session before continuing", rounds)
}
func ptrString(value string) *string { return &value }
func repoToolPath(repo, path string, write bool) (string, error) {
	clean := filepath.Clean(path)
	if path == "" || clean == "." || filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must stay inside the repository")
	}
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, clean)
	existing := target
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("invalid path")
		}
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes repository through a symlink")
	}
	for _, candidate := range []string{clean, relative} {
		first := strings.Split(candidate, string(filepath.Separator))[0]
		if first == ".git" || first == ".crew" {
			return "", fmt.Errorf("repository tool cannot access git internals or Crew state")
		}
	}
	if write {
		for node := target; node != root; node = filepath.Dir(node) {
			info, err := os.Lstat(node)
			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("writes through symlinks are unsupported")
			}
			if err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
	}
	return target, nil
}
func (r *Runner) apiTool(ctx context.Context, p Provider, o AgentOptions, call apiToolCall) (string, error) {
	var args struct {
		Skill   string `json:"skill"`
		Path    string `json:"path"`
		Content string `json:"content"`
		Check   string `json:"check"`
		Phase   string `json:"phase"`
		Agent   string `json:"agent"`
		Task    string `json:"task"`
	}
	decoder := json.NewDecoder(strings.NewReader(call.Function.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return "", fmt.Errorf("invalid tool arguments")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return "", fmt.Errorf("tool arguments contain trailing data")
	}
	switch call.Function.Name {
	case "list_files":
		files := []string{}
		err := filepath.WalkDir(o.Repo, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", ".crew", "node_modules", ".venv", "vendor":
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() {
				rel, _ := filepath.Rel(o.Repo, path)
				files = append(files, rel)
			}
			if len(files) >= 1000 {
				return filepath.SkipAll
			}
			return ctx.Err()
		})
		if err != nil {
			return "", err
		}
		sort.Strings(files)
		data, _ := json.Marshal(files)
		return string(data), nil
	case "read_skill_file":
		session, err := r.Store.Get(o.Session)
		if err != nil {
			return "", err
		}
		skills, err := r.Skills(session)
		if err != nil {
			return "", err
		}
		skills, err = FilterSkills(skills, o.Skills)
		if err != nil {
			return "", err
		}
		root := ""
		for _, skill := range skills {
			if skill.Name == args.Skill {
				root = skill.Path
			}
		}
		if root == "" {
			return "", fmt.Errorf("skill is not selected for this role")
		}
		path, err := repoToolPath(root, args.Path, false)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() > 128*1024 {
			return "", fmt.Errorf("supporting file exceeds read limit")
		}
		data, err := os.ReadFile(path)
		return r.Config.Redact(string(data)), err
	case "read_file":
		path, err := repoToolPath(o.Repo, args.Path, false)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() > 128*1024 {
			return "", fmt.Errorf("read_file supports regular files up to 128 KB")
		}
		data, err := os.ReadFile(path)
		return r.Config.Redact(string(data)), err
	case "write_file":
		if workflow, err := r.Store.Workflow(o.Session); err == nil && workflow.Current < len(workflow.Recipe.Stages) && workflow.Recipe.Stages[workflow.Current].Role == "tester" {
			return "", fmt.Errorf("independent testers cannot write production files")
		}
		if !o.Edits || !p.Has("write") {
			return "", fmt.Errorf("read-only permission denies writes")
		}
		if len(args.Content) > 512*1024 {
			return "", fmt.Errorf("write content exceeds 512 KB")
		}
		path, err := repoToolPath(o.Repo, args.Path, true)
		if err != nil {
			return "", err
		}
		mode := os.FileMode(0600)
		if info, err := os.Stat(path); err == nil {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("write target must be a regular file")
			}
			mode = info.Mode().Perm()
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(r.Config.Redact(args.Content)), mode); err != nil {
			return "", err
		}
		return "File written: " + args.Path, nil
	case "run_check":
		if !o.Edits || !p.Has("tools") {
			return "", fmt.Errorf("permission denies executable checks")
		}
		evidence, err := r.Store.RunCheck(ctx, o.Session, args.Check, args.Phase)
		data, _ := json.Marshal(evidence)
		if err != nil {
			return string(data), err
		}
		return string(data), nil
	case "delegate":
		if o.Depth != 0 || !p.Has("delegate") {
			return "", fmt.Errorf("delegation is unavailable")
		}
		parent, err := r.Store.Get(o.Session)
		if err != nil {
			return "", err
		}
		if _, err := r.Config.Provider(args.Agent); err != nil {
			return "", err
		}
		return r.Delegate(ctx, parent, args.Agent, args.Task)
	default:
		return "", fmt.Errorf("unknown repository tool")
	}
}
