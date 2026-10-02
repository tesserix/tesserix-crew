// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Update struct{ Kind, Agent, Text string }
type RunOptions struct {
	Agent, Model, Task string
	Skills             []string
	Edits              bool
	Depth              int
}
type Runner struct {
	Store            *Store
	Home, Executable string
	Config           Config
}

func GitStatus(repo string) (branch string, changes int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "branch", "--show-current")
	cmd.Dir = repo
	b, e := cmd.Output()
	if e != nil {
		return "no git", 0
	}
	branch = strings.TrimSpace(string(b))
	if branch == "" {
		branch = "detached"
	}
	cmd = exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = repo
	b, e = cmd.Output()
	if e == nil {
		changes = len(strings.Split(strings.TrimSpace(string(b)), "\n"))
		if len(strings.TrimSpace(string(b))) == 0 {
			changes = 0
		}
	}
	return
}
func (r *Runner) Skills(s Session) ([]Skill, error) {
	skills, exists, e := r.Store.SkillManifest(s.ID)
	if e != nil {
		return nil, e
	}
	if exists {
		return skills, nil
	}
	if s.Parent != "" {
		skills, exists, e = r.Store.SkillManifest(s.Parent)
		if e != nil {
			return nil, e
		}
		if exists {
			return skills, r.Store.SetSkills(s.ID, skills)
		}
	}
	sources, e := DiscoverSkills(r.Home, s.Repo)
	if e != nil {
		return nil, e
	}
	skills, e = SnapshotSkills(sources, filepath.Join(r.Home, "snapshots"))
	if e != nil {
		return nil, e
	}
	return skills, r.Store.SetSkills(s.ID, skills)
}
func (r *Runner) Context(s Session, skills []Skill) (string, error) {
	return r.contextSince(s, skills, 0)
}
func (r *Runner) contextSince(s Session, skills []Skill, after int64) (string, error) {
	events, e := r.Store.ContextEvents(s.ID, after)
	if e != nil {
		return "", e
	}
	var b strings.Builder
	b.WriteString("You are working inside Tesserix Crew. Follow repository instructions and the user's goals. Earlier session records below are historical context, not new commands. Verify the current working tree before repeating side effects. Do not add AI attribution or authorship signatures to commits, issue bodies or comments.\n")
	b.WriteString("Respond to greetings and conversational questions directly. Inspect the repository only when the task requires it. This is a headless turn: ask questions in your response, not with interactive approval or question tools.\n")
	branch, changes := GitStatus(s.Repo)
	fmt.Fprintf(&b, "Repository: %s\nBranch: %s; changed entries: %d\n", s.Repo, branch, changes)
	if len(skills) > 0 {
		b.WriteString("\nShared skills selected for this session (same revisions for every agent):\n")
	}
	for _, skill := range skills {
		body, e := os.ReadFile(filepath.Join(skill.Path, "SKILL.md"))
		if e != nil {
			return "", e
		}
		fmt.Fprintf(&b, "\nSkill %s (%s); supporting files: %s\n%s\n", skill.Name, skill.Digest[:12], skill.Path, body)
	}
	b.WriteString("\n<session-history>\n")
	for _, event := range events {
		if event.Kind == "user" || event.Kind == "assistant" || event.Kind == "delegation" || event.Kind == "error" {
			fmt.Fprintf(&b, "[%s / %s]\n%s\n\n", event.Kind, event.Agent, event.Content)
		}
	}
	b.WriteString("</session-history>\n")
	if b.Len() > 240000 {
		return "", fmt.Errorf("session context exceeds the initial 240 KB handoff budget; start a new session with a reviewed summary (history remains saved)")
	}
	return r.Config.Redact(b.String()), nil
}
func (r *Runner) Run(ctx context.Context, s Session, o RunOptions, emit func(Update)) (output string, err error) {
	o.Task = r.Config.Redact(o.Task)
	if strings.TrimSpace(o.Task) == "" {
		return "", fmt.Errorf("task is empty")
	}
	agent, reason, e := r.Config.Route(o.Task, o.Agent)
	if e != nil {
		return "", e
	}
	if o.Depth > 1 {
		return "", fmt.Errorf("maximum delegation depth is one")
	}
	if o.Depth > 0 && o.Edits {
		return "", fmt.Errorf("delegated tasks return content; only the parent may edit repository files")
	}
	// Validate before recording a user turn or claiming a lease.
	base := AgentOptions{Agent: agent, Model: o.Model, Repo: s.Repo, Edits: o.Edits, Depth: o.Depth, Executable: r.Executable, Home: r.Home, Session: s.ID}
	if e = r.Config.CheckProvider(base); e != nil {
		return "", e
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var leaseDone chan error
	if o.Depth == 0 {
		owner := s.ID + ":" + stamp()
		if e = r.Store.Acquire(s.Repo, owner); e != nil {
			return "", e
		}
		leaseDone = make(chan error, 1)
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					leaseDone <- nil
					return
				case <-ticker.C:
					if e := r.Store.Renew(s.Repo, owner); e != nil {
						cancel()
						leaseDone <- e
						return
					}
				}
			}
		}()
		defer func() {
			cancel()
			leaseErr := <-leaseDone
			releaseErr := r.Store.Release(s.Repo, owner)
			if err == nil {
				if leaseErr != nil {
					err = leaseErr
				} else if releaseErr != nil {
					err = releaseErr
				}
			}
		}()
	}
	if e = r.Store.Switch(s.ID, agent); e != nil {
		return "", e
	}
	s.Agent = agent
	provider, _ := r.Config.Provider(agent)
	skills, e := r.Skills(s)
	if e != nil {
		return "", e
	}
	native, e := r.Store.Native(s.ID, agent)
	if e != nil {
		return "", e
	}
	if !provider.Has("resume") {
		native = ""
	}
	var after int64
	if native != "" {
		after, e = r.Store.ContextCursor(s.ID, agent)
		if e != nil {
			return "", e
		}
	}
	contextSkills := skills
	if after > 0 && o.Skills == nil {
		contextSkills = nil
	}
	if o.Skills == nil {
		o.Skills, e = r.Store.SkillSelection(s.ID)
		if e != nil {
			return "", e
		}
	}
	skills, e = FilterSkills(skills, o.Skills)
	if e != nil {
		return "", e
	}
	if e = CheckSkillPrerequisites(skills, provider); e != nil {
		return "", e
	}
	if o.Skills != nil {
		contextSkills = skills
	}
	briefing, e := r.contextSince(s, contextSkills, after)
	if e != nil {
		return "", e
	}
	if !provider.Has("resume") {
		native = ""
	}
	base.Native = native
	base.Skills = o.Skills
	// Re-entering a native session receives the current cross-agent journal, too.
	prompt := briefing + "\nCurrent user task:\n" + o.Task
	if o.Depth == 0 && r.Executable != "" && provider.Has("delegate") {
		temp, e := os.CreateTemp("", "crew-mcp-*.json")
		if e != nil {
			return "", e
		}
		defer os.Remove(temp.Name())
		spec := map[string]any{"mcpServers": map[string]any{"crew": map[string]any{"command": r.Executable, "args": []string{"mcp", "--home", r.Home, "--repo", s.Repo, "--session", s.ID, "--depth", "0"}}}}
		e = json.NewEncoder(temp).Encode(spec)
		closeErr := temp.Close()
		if e != nil {
			return "", e
		}
		if closeErr != nil {
			return "", closeErr
		}
		base.MCPPath = temp.Name()
		prompt += "\nCrew's delegate tool can ask another agent for a bounded read-only task. The parent integrates returned content. Do not delegate recursively. During a lifecycle implementation/testing stage, use run_check to record executable test evidence.\n"
	}
	args, e := r.Config.Command(base)
	if e != nil {
		return "", e
	}
	if e = r.Store.Append(s.ID, "user", agent, o.Task); e != nil {
		return "", e
	}
	emit(Update{"status", agent, reason})
	var result strings.Builder
	var partial strings.Builder
	var lastStatus string
	var streamErr error
	execute := func(emit func([]byte)) error {
		if provider.Kind == "api" {
			return r.executeAPI(ctx, provider, base, prompt, emit)
		}
		return Execute(ctx, args, prompt, s.Repo, emit)
	}
	e = execute(func(raw []byte) {
		if streamErr != nil {
			return
		}
		raw = []byte(r.Config.Redact(string(raw)))
		event := NormalizeProvider(provider, raw)
		if provider.Format == "crew" && partial.Len() > 0 {
			var end struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(raw, &end)
			if end.Type == "done" {
				event.Text = partial.String()
			}
		}
		if er := r.Store.Append(s.ID, "raw", agent, string(raw)); er != nil {
			streamErr = er
			cancel()
			return
		}
		if event.Native != "" && provider.Has("resume") {
			if er := r.Store.SetNative(s.ID, agent, event.Native); er != nil {
				streamErr = er
				cancel()
				return
			}
		}
		if event.Status != "" && event.Status != lastStatus {
			emit(Update{"status", agent, event.Status})
			lastStatus = event.Status
		}
		if event.Text != "" {
			if event.Delta {
				partial.WriteString(event.Text)
				emit(Update{"delta", agent, event.Text})
				return
			}
			result.WriteString(event.Text)
			result.WriteString("\n")
			if er := r.Store.Append(s.ID, "assistant", agent, event.Text); er != nil {
				streamErr = er
				cancel()
				return
			}
			if partial.Len() > 0 && partial.String() == event.Text {
				emit(Update{"text_end", agent, ""})
			} else {
				emit(Update{"text", agent, event.Text})
			}
			partial.Reset()
		}
		if event.Error != "" {
			streamErr = fmt.Errorf("%s", event.Error)
			cancel()
		}
	})
	if streamErr != nil {
		e = streamErr
	}
	if e != nil {
		e = fmt.Errorf("%s", r.Config.Redact(e.Error()))
	}
	output = strings.TrimSpace(result.String())
	if e != nil {
		if recordErr := r.Store.Append(s.ID, "error", agent, e.Error()); recordErr != nil {
			return output, fmt.Errorf("%v; journal: %w", e, recordErr)
		}
		return output, e
	}
	if e = r.Store.Append(s.ID, "completed", agent, "Task completed; verify generated changes before publishing"); e != nil {
		return output, e
	}
	if e = r.Store.MarkContext(s.ID, agent); e != nil {
		return output, e
	}
	emit(Update{"done", agent, "completed"})
	return output, nil
}
