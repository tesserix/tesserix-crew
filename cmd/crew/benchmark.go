// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/tesserix/tesserix-crew/internal/core"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type benchmarkTurn struct {
	Agent  string          `json:"agent"`
	Mode   string          `json:"mode"`
	Timing core.TurnTiming `json:"timing"`
	Error  string          `json:"error,omitempty"`
}

func benchmark(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	agent := flags.String("agent", "", "explicit provider to benchmark")
	model := flags.String("model", "", "provider model")
	runs := flags.Int("runs", 2, "turns (first fresh, subsequent native resumes where supported)")
	task := flags.String("task", "hello", "explicit read-only benchmark task")
	repo := flags.String("repo", ".", "repository")
	timeout := flags.Duration("timeout", 90*time.Second, "timeout per turn")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *agent == "" || *agent == "auto" || *runs < 1 || *runs > 10 || *timeout <= 0 || flags.NArg() != 0 {
		return fmt.Errorf("benchmark needs --agent NAME, 1–10 runs and a positive timeout")
	}
	var err error
	*repo, err = filepath.Abs(*repo)
	if err != nil {
		return err
	}
	home, err := os.MkdirTemp("", "crew-benchmark-")
	if err != nil {
		return err
	}
	store, err := core.OpenStore(home)
	if err != nil {
		return err
	}
	defer store.Close()
	config, err := core.LoadConfig(home, *repo)
	if err != nil {
		return err
	}
	session, err := store.Create(*repo, *agent, "")
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	runner := core.Runner{Store: store, Home: home, Executable: executable, Config: config}
	out := []benchmarkTurn{}
	totals := []int64{}
	outputs := []int64{}
	for i := 0; i < *runs; i++ {
		mode := "fresh"
		if i > 0 {
			native, _ := store.Native(session.ID, *agent)
			if native != "" {
				mode = "native-resume"
			} else {
				mode = "shared-context"
			}
		}
		turn := benchmarkTurn{Agent: *agent, Mode: mode}
		turnCtx, cancel := context.WithTimeout(ctx, *timeout)
		_, err := runner.Run(turnCtx, session, core.RunOptions{Agent: *agent, Model: *model, Task: *task}, func(v core.Update) {
			if v.Kind == "timing" {
				_ = json.Unmarshal([]byte(v.Text), &turn.Timing)
			}
		})
		cancel()
		if err != nil {
			turn.Error = err.Error()
		} else {
			totals = append(totals, turn.Timing.TotalMS)
			outputs = append(outputs, turn.Timing.FirstOutputMS)
		}
		out = append(out, turn)
		if ctx.Err() != nil {
			break
		}
	}
	median := func(v []int64) int64 {
		if len(v) == 0 {
			return 0
		}
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		if len(v)%2 == 0 {
			return (v[len(v)/2-1] + v[len(v)/2]) / 2
		}
		return v[len(v)/2]
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"journal": home, "turns": out, "successful_turns": len(totals), "median_total_ms": median(totals), "median_first_output_ms": median(outputs), "note": "Fresh and resumed turns are distinct scenarios; compare identical providers/models/tasks across versions. Journals are retained for inspection."}); err != nil {
		return err
	}
	if len(totals) != *runs {
		return fmt.Errorf("one or more benchmark turns failed; inspect the JSON errors")
	}
	return nil
}
