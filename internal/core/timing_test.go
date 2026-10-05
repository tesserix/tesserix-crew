// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeResumeAvoidsRepeatedSelectedSkillsAndRecordsTiming(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	if err := CopySkill(makeSkill(t, "UNIQUE_SKILL_INSTRUCTION"), filepath.Join(SkillRoot(home, repo, false), "custom")); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "adapter")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > prompt.txt\nprintf '%s\\n' '{\"type\":\"session\",\"session\":\"native-id\"}' '{\"type\":\"text\",\"text\":\"response\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, err := store.Create(repo, "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	p := Provider{Kind: "cli", Command: []string{script}, Format: "crew", Auth: "none", ReadArgs: []string{"--read"}, ResumeArgs: []string{"--resume", "{session}"}, Capabilities: []string{"read", "resume"}}
	r := Runner{Store: store, Home: home, Config: Config{Providers: map[string]Provider{"fixture": p}}}
	if _, err := r.Skills(s); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSkillSelection(s.ID, []string{"custom"}); err != nil {
		t.Fatal(err)
	}
	run := func() TurnTiming {
		var timing TurnTiming
		_, err := r.Run(context.Background(), s, RunOptions{Agent: "fixture", Task: "hello"}, func(u Update) {
			if u.Kind == "timing" {
				if err := json.Unmarshal([]byte(u.Text), &timing); err != nil {
					t.Error(err)
				}
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if timing.ContextBytes == 0 || timing.FirstOutputMS < timing.PrepareMS || timing.TotalMS < timing.FirstOutputMS {
			t.Fatalf("invalid timing: %+v", timing)
		}
		return timing
	}
	first := run()
	body, _ := os.ReadFile(filepath.Join(repo, "prompt.txt"))
	if !strings.Contains(string(body), "UNIQUE_SKILL_INSTRUCTION") {
		t.Fatal("initial skill omitted")
	}
	second := run()
	body, _ = os.ReadFile(filepath.Join(repo, "prompt.txt"))
	if strings.Contains(string(body), "UNIQUE_SKILL_INSTRUCTION") {
		t.Fatal("skill unnecessarily repeated")
	}
	if second.ContextBytes >= first.ContextBytes {
		t.Fatal("resume context did not shrink")
	}
	if err := store.SetSkillSelection(s.ID, []string{"custom"}); err != nil {
		t.Fatal(err)
	}
	run()
	body, _ = os.ReadFile(filepath.Join(repo, "prompt.txt"))
	if !strings.Contains(string(body), "UNIQUE_SKILL_INSTRUCTION") {
		t.Fatal("selection change omitted skill")
	}
	events, err := store.Events(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Kind == "timing" {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("timings persisted: %d", count)
	}
}
