// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreResumeHandoffAndLease(t *testing.T) {
	home := t.TempDir()
	s, e := OpenStore(home)
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.Create(t.TempDir(), "claude", "")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Append(session.ID, "user", "claude", "Keep existing API compatibility"); e != nil {
		t.Fatal(e)
	}
	if e = s.SetNative(session.ID, "claude", "native-id"); e != nil {
		t.Fatal(e)
	}
	if e = s.Switch(session.ID, "codex"); e != nil {
		t.Fatal(e)
	}
	if e = s.Acquire(session.Repo, "one"); e != nil {
		t.Fatal(e)
	}
	if e = s.Acquire(session.Repo, "two"); e == nil {
		t.Fatal("concurrent repository lease accepted")
	}
	if e = s.Release(session.Repo, "one"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenStore(home)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e := s.Latest(session.Repo)
	if e != nil || got.Agent != "codex" {
		t.Fatalf("resume: %+v %v", got, e)
	}
	native, e := s.Native(session.ID, "claude")
	if e != nil || native != "native-id" {
		t.Fatalf("native resume: %s %v", native, e)
	}
	runner := Runner{Store: s}
	brief, e := runner.Context(got, nil)
	if e != nil || !strings.Contains(brief, "Keep existing API compatibility") {
		t.Fatalf("handoff: %s %v", brief, e)
	}
}
func TestConfigPrecedence(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".crew"), 0700)
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("default_agent='claude'\n[[rules]]\ncontains=['review']\nagent='claude'\n"), 0600)
	os.WriteFile(filepath.Join(repo, ".crew", "config.toml"), []byte("default_agent='codex'\n[[rules]]\ncontains=['review']\nagent='codex'\n"), 0600)
	config, e := LoadConfig(home, repo)
	if e != nil {
		t.Fatal(e)
	}
	agent, _, e := config.Route("Review this change", "auto")
	if e != nil || agent != "codex" {
		t.Fatal(agent, e)
	}
	agent, _, e = config.Route("Review this change", "claude")
	if e != nil || agent != "claude" {
		t.Fatal(agent, e)
	}
}
func TestSkillSnapshotPinnedAcrossChanges(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	source := filepath.Join(repo, ".crew", "skills", "review")
	os.MkdirAll(source, 0700)
	os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("Check API compatibility."), 0600)
	store, e := OpenStore(home)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	session, e := store.Create(repo, "claude", "")
	if e != nil {
		t.Fatal(e)
	}
	runner := Runner{Store: store, Home: home}
	first, e := runner.Skills(session)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("Changed instructions"), 0600)
	second, e := runner.Skills(session)
	if e != nil || first[0].Digest != second[0].Digest {
		t.Fatalf("session skill changed: %v", e)
	}
	body, e := os.ReadFile(filepath.Join(second[0].Path, "SKILL.md"))
	if e != nil || string(body) != "Check API compatibility." {
		t.Fatal(string(body), e)
	}
	if e = os.Symlink("/etc/passwd", filepath.Join(source, "secret")); e != nil {
		t.Fatal(e)
	}
	if _, e = SnapshotSkills(map[string]string{"review": source}, filepath.Join(home, "snapshots")); e == nil {
		t.Fatal("symlink accepted")
	}
}
func TestAgentCommandsAndEvents(t *testing.T) {
	claudeArgs, err := AgentCommand(AgentOptions{Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	claudeCommand := strings.Join(claudeArgs, " ")
	if strings.Contains(claudeCommand, "--permission-mode plan") || !strings.Contains(claudeCommand, "--include-partial-messages") || !strings.Contains(claudeCommand, "--disallowedTools Edit,Write,NotebookEdit,Bash") {
		t.Fatal(claudeCommand)
	}
	args, e := AgentCommand(AgentOptions{Agent: "codex", Native: "abc", Edits: false})
	if e != nil {
		t.Fatal(e)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "resume abc") || !strings.Contains(joined, "read-only") {
		t.Fatal(joined)
	}
	if strings.Contains(joined, "dangerously") {
		t.Fatal("permission bypass")
	}
	claude := Normalize("claude", []byte(`{"type":"assistant","session_id":"x","message":{"content":[{"type":"text","text":"result"}]}}`))
	if claude.Native != "x" || claude.Text != "result" {
		t.Fatal(claude)
	}
	codex := Normalize("codex", []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`))
	if codex.Text != "done" {
		t.Fatal(codex)
	}
	failed := Normalize("claude", []byte(`{"type":"result","is_error":true,"errors":["limit reached"]}`))
	if failed.Error != "limit reached" {
		t.Fatal(failed)
	}
}

func TestClaudeProgressEvents(t *testing.T) {
	thinking := Normalize("claude", []byte(`{"type":"system","subtype":"thinking_tokens","estimated_tokens":50}`))
	if thinking.Status != "thinking" {
		t.Fatal(thinking)
	}
	delta := Normalize("claude", []byte(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`))
	if !delta.Delta || delta.Text != "Hello" {
		t.Fatal(delta)
	}
	tool := Normalize("claude", []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"git status --short"}}]}}`))
	if !strings.Contains(tool.Status, "git status --short") {
		t.Fatal(tool)
	}
	done := Normalize("claude", []byte(`{"type":"user","message":{"content":[{"type":"tool_result","content":"result"}]}}`))
	if !strings.Contains(done.Status, "tool completed") {
		t.Fatal(done)
	}
}
func TestExecuteCancellationAndLargeStderr(t *testing.T) {
	script := filepath.Join(t.TempDir(), "agent")
	os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\ni=0\nwhile [ $i -lt 3000 ]; do echo diagnostic-diagnostic-diagnostic >&2; i=$((i+1)); done\necho '{\"type\":\"done\"}'\nsleep 30\n"), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	count := 0
	started := time.Now()
	e := Execute(ctx, []string{script}, strings.Repeat("context", 100000), t.TempDir(), func([]byte) { count++; cancel() })
	if e == nil || count != 1 || time.Since(started) > 5*time.Second {
		t.Fatalf("cancel: %v events %d", e, count)
	}
}
func TestMCPProtocol(t *testing.T) {
	store, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	s, e := store.Create(t.TempDir(), "codex", "")
	if e != nil {
		t.Fatal(e)
	}
	runner := Runner{Store: store, Home: t.TempDir()}
	var out bytes.Buffer
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-06-18\"}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"delegate\",\"arguments\":{\"agent\":\"bogus\",\"task\":\"x\"}}}\n")
	if e = runner.ServeMCP(context.Background(), s, 0, input, &out); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatal(out.String())
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal(line)
		}
	}
	if !strings.Contains(lines[0], "2025-06-18") || !strings.Contains(lines[1], "delegate") || !strings.Contains(lines[2], `"isError":true`) {
		t.Fatal(out.String())
	}
	if e = runner.ServeMCP(context.Background(), s, 1, strings.NewReader(""), &out); e == nil {
		t.Fatal("recursive delegation accepted")
	}
}

func TestRunnerResumeSwitchAndDelegation(t *testing.T) {
	bin, home, repo := t.TempDir(), t.TempDir(), t.TempDir()
	capture := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("CREW_TEST_CAPTURE", capture)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Scripted CLIs exercise the real subprocess boundary without paid requests.
	scripts := map[string]string{
		"claude": "#!/bin/sh\ncat >\"$CREW_TEST_CAPTURE\"\necho '{\"type\":\"assistant\",\"session_id\":\"native-claude\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"Decision: preserve the public API\"}]}}'\necho '{\"type\":\"result\",\"is_error\":false}'\n",
		"codex":  "#!/bin/sh\ncat >\"$CREW_TEST_CAPTURE\"\necho '{\"type\":\"thread.started\",\"thread_id\":\"native-codex\"}'\necho '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"Review: public API preserved\"}}'\n",
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session, err := store.Create(repo, "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: store, Home: home, Config: Config{DefaultAgent: "claude"}}
	_, err = runner.Run(context.Background(), session, RunOptions{Agent: "claude", Task: "Plan the change"}, func(Update) {})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), session, RunOptions{Agent: "codex", Task: "Continue with the agreed plan"}, func(Update) {})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(prompt), "preserve the public API") {
		t.Fatalf("lost handoff: %s %v", prompt, err)
	}
	got, err := runner.Delegate(context.Background(), session, "codex", "Review API compatibility")
	if err != nil || !strings.Contains(got, "Review: public API preserved") {
		t.Fatalf("delegation: %s %v", got, err)
	}
	children, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	childCount := 0
	for _, child := range children {
		if child.Parent == session.ID {
			childCount++
		}
	}
	if childCount != 1 {
		t.Fatalf("expected one child; got %d", childCount)
	}
	events, err := store.Events(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Kind == "delegation" && strings.Contains(event.Content, "Review: public API preserved") {
			found = true
		}
	}
	if !found {
		t.Fatal("delegation result not saved to parent")
	}
	if _, err = runner.Run(context.Background(), session, RunOptions{Agent: "codex", Task: "Edit", Depth: 1, Edits: true}, func(Update) {}); err == nil {
		t.Fatal("child allowed to edit")
	}
}
