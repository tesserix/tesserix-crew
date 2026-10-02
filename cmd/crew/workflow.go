// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tesserix/tesserix-crew/internal/core"
)

func workflow(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: crew workflow start|list|status|next|approve|reject|record|retry|recover|presets [options] [ID]")
	}
	action := args[0]
	switch action {
	case "start", "list", "status", "next", "approve", "reject", "record", "retry", "recover", "presets":
	default:
		return fmt.Errorf("unknown workflow action %q", action)
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	defaultHome := os.Getenv("CREW_HOME")
	if defaultHome == "" {
		defaultHome = filepath.Join(userHome, ".crew")
	}
	flags := flag.NewFlagSet("workflow "+action, flag.ContinueOnError)
	repo := flags.String("repo", wd, "repository path")
	home := flags.String("home", defaultHome, "Crew data directory")
	preset := flags.String("preset", "default", "lifecycle recipe")
	edits := flags.Bool("allow-edits", false, "enable stage's native edit permissions")
	note := flags.String("note", "", "user feedback or checkpoint evidence")
	asJSON := flags.Bool("json", false, "machine-readable status")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	*repo, err = filepath.Abs(*repo)
	if err != nil {
		return err
	}
	*home, err = filepath.Abs(*home)
	if err != nil {
		return err
	}
	info, err := os.Stat(*repo)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("repository is not a directory")
	}
	// Existing runs use their pinned recipe, even if a later config edit is invalid.
	config := core.Config{DefaultAgent: "claude"}
	if action == "start" || action == "presets" {
		config, err = core.LoadConfig(*home, *repo)
		if err != nil {
			return err
		}
	}
	if action == "presets" {
		names := make([]string, 0, len(config.Lifecycles))
		for name := range config.Lifecycles {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			recipe := config.Lifecycles[name]
			fmt.Printf("%s — %s\n", name, recipe.Description)
			for _, stage := range recipe.Stages {
				fmt.Printf("  %-22s %-10s %s\n", stage.Name, stage.Kind, stage.Agent)
			}
		}
		return nil
	}
	if action != "list" && action != "start" && flags.NArg() != 1 {
		return fmt.Errorf("usage: crew workflow %s [options] ID (put flags before ID)", action)
	}
	if action == "start" && flags.NArg() == 0 {
		return fmt.Errorf("usage: crew workflow start [options] \"task\"")
	}
	if action == "list" && flags.NArg() != 0 {
		return fmt.Errorf("workflow list does not take an ID")
	}
	store, err := core.OpenStore(*home)
	if err != nil {
		return err
	}
	defer store.Close()
	if action == "list" {
		runs, err := store.Workflows(*repo)
		if err != nil {
			return err
		}
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(runs)
		}
		for _, w := range runs {
			fmt.Printf("%s  %-18s %-22s %s\n", w.ID, w.State, workflowStage(w), w.Task)
		}
		return nil
	}
	var w core.Workflow
	if action == "start" {
		recipe, ok := config.Lifecycles[*preset]
		if !ok {
			return fmt.Errorf("unknown lifecycle %q; use crew workflow presets", *preset)
		}
		w, err = store.CreateWorkflow(*repo, strings.Join(flags.Args(), " "), *preset, recipe)
	} else {
		w, err = store.Workflow(flags.Arg(0))
		if err != nil {
			return err
		}
		if w.Repo != *repo {
			return fmt.Errorf("workflow belongs to %s; select it with --repo", w.Repo)
		}
		switch action {
		case "next":
			executable, e := os.Executable()
			if e != nil {
				return e
			}
			runner := core.Runner{Store: store, Home: *home, Executable: executable, Config: config}
			emit := func(update core.Update) {
				if *asJSON {
					return
				}
				printUpdate(os.Stdout, os.Stderr, update)
			}
			w, err = store.RunWorkflow(ctx, w.ID, *edits, runner.Run, emit)
		case "approve", "reject", "record", "retry":
			w, err = store.DecideWorkflow(w.ID, action, *note)
		case "recover":
			w, err = store.RecoverWorkflow(w.ID, *note)
		}
	}
	if w.ID != "" {
		if e := printWorkflow(w, *asJSON); e != nil && err == nil {
			err = e
		}
	}
	return err
}
func workflowStage(w core.Workflow) string {
	if w.Current >= len(w.Recipe.Stages) {
		return "done"
	}
	return w.Recipe.Stages[w.Current].Name
}
func printWorkflow(w core.Workflow, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(w)
	}
	fmt.Printf("\nWorkflow %s · %s · %s\nGoal: %s\n", w.ID, w.Preset, w.State, w.Task)
	for i, stage := range w.Recipe.Stages {
		marker := "○"
		if i < w.Current {
			marker = "✓"
		}
		if i == w.Current {
			marker = "→"
		}
		provider := stage.Agent
		if provider == "" {
			provider = "user"
		}
		fmt.Printf("%s %-22s %s\n", marker, stage.Name, provider)
	}
	if len(w.Results) > 0 {
		result := w.Results[len(w.Results)-1]
		fmt.Printf("\nLatest result: %s · %s\n%s\n", result.Stage, result.State, result.Output)
	}
	if w.Current < len(w.Recipe.Stages) {
		stage := w.Recipe.Stages[w.Current]
		switch w.State {
		case "awaiting-approval":
			fmt.Printf("\n%s\nReview all outputs: crew workflow status --json %s\nApprove: crew workflow approve %s\nReject: crew workflow reject --note 'feedback' %s\n", stage.Prompt, w.ID, w.ID, w.ID)
		case "awaiting-evidence":
			fmt.Printf("\n%s\nRecord: crew workflow record --note 'URLs and evidence' %s\n", stage.Prompt, w.ID)
		case "ready":
			editFlag := ""
			if stage.Edits {
				editFlag = "--allow-edits "
			}
			fmt.Printf("\nNext: crew workflow next %s%s\n", editFlag, w.ID)
		case "failed", "blocked", "rejected":
			fmt.Printf("\nInspect the result, resolve the problem, then: crew workflow retry --note 'what changed' %s\n", w.ID)
		case "running":
			fmt.Println("\nThis stage is running. If its process exited unexpectedly, use workflow recover after the lease expires.")
		}
	}
	return nil
}
