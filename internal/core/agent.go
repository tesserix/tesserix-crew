// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type AgentOptions struct {
	Agent, Model, Native, Repo, MCPPath, Executable, Home, Session string
	Skills                                                         []string
	Edits                                                          bool
	Depth                                                          int
	Conversational                                                 bool
}

func AgentCommand(o AgentOptions) ([]string, error) {
	var args []string
	switch o.Agent {
	case "claude":
		mode := "default"
		if o.Edits {
			mode = "acceptEdits"
		}
		args = []string{"claude", "--print", "--verbose", "--include-partial-messages", "--output-format", "stream-json", "--permission-mode", mode}
		if o.Conversational {
			args = append(args, "--tools", "", "--strict-mcp-config")
		}
		// Plan mode expects interactive plan approval. Headless read-only turns
		// instead disable built-in mutation tools and shell execution.
		if !o.Edits {
			args = append(args, "--disallowedTools", "Edit,Write,NotebookEdit,Bash")
		}
		if o.Native != "" {
			args = append(args, "--resume", o.Native)
		}
		if o.MCPPath != "" {
			args = append(args, "--mcp-config", o.MCPPath)
		}
	case "codex":
		sandbox := "read-only"
		if o.Edits {
			sandbox = "workspace-write"
		}
		args = []string{"codex", "exec", "-c", `sandbox_mode="` + sandbox + `"`}
		if o.MCPPath != "" {
			command, _ := json.Marshal(o.Executable)
			mcpArgs, _ := json.Marshal([]string{"mcp", "--home", o.Home, "--repo", o.Repo, "--session", o.Session, "--depth", fmt.Sprint(o.Depth)})
			args = append(args, "-c", "mcp_servers.crew.command="+string(command), "-c", "mcp_servers.crew.args="+string(mcpArgs))
		}
		if o.Native != "" {
			args = append(args, "resume", o.Native)
		}
		args = append(args, "--json", "--skip-git-repo-check")
	case "gemini":
		return nil, fmt.Errorf("Gemini adapter awaits local CLI compatibility verification; install Gemini CLI, then run crew doctor")
	default:
		return nil, fmt.Errorf("unknown agent: %s", o.Agent)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	return args, nil
}

type AgentEvent struct {
	Text, Native, Error, Status string
	Delta                       bool
	Raw                         json.RawMessage
}

func Normalize(agent string, raw []byte) AgentEvent {
	out := AgentEvent{Raw: append([]byte(nil), raw...)}
	var v struct {
		Type    string          `json:"type"`
		Session string          `json:"session_id"`
		Thread  string          `json:"thread_id"`
		Result  string          `json:"result"`
		IsError bool            `json:"is_error"`
		Errors  []string        `json:"errors"`
		Message json.RawMessage `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
		Item struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Command string `json:"command"`
		} `json:"item"`
		Denials []json.RawMessage `json:"permission_denials"`
		Subtype string            `json:"subtype"`
		Event   struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		out.Status = "Received non-JSON CLI diagnostic"
		return out
	}
	if agent == "claude" {
		out.Native = v.Session
		if v.Type == "system" && v.Subtype == "init" {
			out.Status = "connected · preparing response"
		}
		if v.Type == "system" && v.Subtype == "thinking_tokens" {
			out.Status = "thinking"
		}
		if v.Type == "stream_event" {
			if v.Event.Delta.Type == "text_delta" {
				out.Text = v.Event.Delta.Text
				out.Delta = true
				out.Status = "responding"
			}
			if v.Event.Delta.Type == "thinking_delta" {
				out.Status = "thinking"
			}
		}
		if v.Type == "user" {
			var message struct {
				Content []struct {
					Type    string `json:"type"`
					IsError bool   `json:"is_error"`
				} `json:"content"`
			}
			_ = json.Unmarshal(v.Message, &message)
			for _, block := range message.Content {
				if block.Type == "tool_result" {
					out.Status = "tool completed · waiting for response"
					if block.IsError {
						out.Status = "tool failed · agent evaluating result"
					}
				}
			}
		}
		if v.Type == "assistant" {
			var msg struct {
				Content []struct {
					Type, Text, Name string
					Input            struct {
						Command string `json:"command"`
					} `json:"input"`
				} `json:"content"`
			}
			_ = json.Unmarshal(v.Message, &msg)
			var texts []string
			for _, b := range msg.Content {
				if b.Type == "text" {
					texts = append(texts, b.Text)
				} else if b.Type == "tool_use" {
					out.Status = "tool: " + b.Name
					if b.Input.Command != "" {
						command := strings.Split(b.Input.Command, "\n")[0]
						if len(command) > 160 {
							command = command[:160] + "…"
						}
						out.Status += " · " + command
					}
				} else if b.Type == "thinking" {
					out.Status = "thinking"
				}
			}
			out.Text = strings.Join(texts, "\n")
		}
		if v.Type == "result" {
			if v.IsError {
				out.Error = v.Result
				if out.Error == "" {
					out.Error = strings.Join(v.Errors, "; ")
				}
				if out.Error == "" {
					out.Error = "Claude task failed"
				}
			}
			if len(v.Denials) > 0 {
				out.Error = "Native CLI denied a requested permission; inspect the session before continuing"
			}
		}
	} else {
		out.Native = v.Thread
		if v.Type == "item.completed" && v.Item.Type == "agent_message" {
			out.Text = v.Item.Text
		}
		if v.Type == "item.started" {
			out.Status = v.Item.Type
		}
		if v.Type == "error" || v.Type == "turn.failed" {
			out.Error = v.Error.Message
			if out.Error == "" {
				_ = json.Unmarshal(v.Message, &out.Error)
			}
			if out.Error == "" {
				out.Error = "Codex task failed"
			}
		}
	}
	return out
}

type tailWriter struct {
	mu sync.Mutex
	b  []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	t.b = append(t.b, p...)
	if len(t.b) > 16384 {
		t.b = t.b[len(t.b)-16384:]
	}
	return n, nil
}
func (t *tailWriter) String() string { t.mu.Lock(); defer t.mu.Unlock(); return string(t.b) }

// Execute isolates a process group so cancellation also stops spawned CLI tools.
func Execute(ctx context.Context, args []string, prompt, repo string, emit func([]byte)) error {
	if len(args) == 0 {
		return errors.New("empty agent command")
	}
	if _, err := exec.LookPath(args[0]); err != nil {
		return fmt.Errorf("%s is not installed; run crew doctor", args[0])
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader(prompt)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr tailWriter
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-done:
		}
	}()
	reader := bufio.NewScanner(stdout)
	reader.Buffer(make([]byte, 65536), 8*1024*1024)
	for reader.Scan() {
		emit(append([]byte(nil), reader.Bytes()...))
	}
	readErr := reader.Err()
	if readErr != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	waitErr := cmd.Wait()
	close(done)
	<-watchDone
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if readErr != nil {
		return fmt.Errorf("CLI stream: %w", readErr)
	}
	if waitErr != nil {
		return fmt.Errorf("agent process: %w\n%s", waitErr, stderr.String())
	}
	return nil
}
func Version(ctx context.Context, agent string) (string, string) {
	path, e := exec.LookPath(agent)
	if e != nil {
		return "", "not installed"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if e != nil {
		return path, fmt.Sprintf("version check failed: %v", e)
	}
	return path, strings.TrimSpace(string(b))
}

var _ io.Writer = (*tailWriter)(nil)
