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
		return fmt.Errorf("usage: crew workflow start|list|status|next|run|approve|reject|record|retry|recover|presets|check|evidence|repair [options] [ID]")
	}
	action := args[0]
	switch action {
	case "start", "list", "status", "next", "approve", "reject", "record", "retry", "recover", "presets", "check", "evidence", "repair", "run":
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
	checkName := flags.String("name", "", "pinned check name")
	phase := flags.String("phase", "green", "red or green test evidence")
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
		if recipe.GitHubRepo == "" {
			recipe.GitHubRepo = config.GitHubRepo
		}
		if recipe.GitHubRepo == "" {
			recipe.GitHubRepo = core.InferGitHubRepo(*repo)
		}
		if len(recipe.Checks) == 0 && recipe.TDD {
			recipe.Checks = config.Checks
			if len(recipe.Checks) == 0 {
				recipe.Checks = core.InferChecks(*repo)
			}
		}
		for i := range recipe.Stages {
			if recipe.Stages[i].Role == "planner" {
				recipe.Stages[i].Prompt += "\nInclude scoped issue proposals as a fenced crew-issues JSON array of objects with title, body and acceptance (array of strings). This typed block is required for GitHub creation. If no executable checks are pinned and TDD is required, also include a fenced crew-checks JSON array of check objects (name, kind, command array, optional timeout, optional artifact paths). These commands will be presented for user approval before execution."
			}
		}
		activeSkills, skillErr := core.DiscoverSkills(*home, *repo)
		if skillErr != nil {
			return skillErr
		}
		for i := range recipe.Stages {
			if recipe.Stages[i].Kind == "agent" && recipe.Stages[i].Skills == nil {
				if name := core.BuiltinRoleSkill(recipe.Stages[i].Role); name != "" {
					recipe.Stages[i].Skills = core.SortedSkillNames(activeSkills)
					if _, exists := activeSkills[name]; !exists {
						recipe.Stages[i].Skills = append(recipe.Stages[i].Skills, name)
					}
				}
			}
		}
		w, err = store.CreateWorkflowWithProviders(*repo, strings.Join(flags.Args(), " "), *preset, recipe, config.Providers)
		if err == nil {
			session, _ := store.Get(w.Session)
			runner := core.Runner{Store: store, Home: *home}
			err = runner.PinWorkflowSkills(session, recipe)
		}
	} else {
		w, err = store.Workflow(flags.Arg(0))
		if err != nil {
			return err
		}
		if w.Repo != *repo {
			return fmt.Errorf("workflow belongs to %s; select it with --repo", w.Repo)
		}
		switch action {
		case "check":
			evidence, e := store.RunCheck(ctx, w.ID, *checkName, *phase)
			if *asJSON {
				_ = json.NewEncoder(os.Stdout).Encode(evidence)
			} else {
				fmt.Printf("%s %s: exit %d · passed=%v\n%s\n", evidence.Check, evidence.Phase, evidence.ExitCode, evidence.Passed, evidence.Output)
			}
			return e
		case "evidence":
			evidence, e := store.Evidence(w.ID)
			if e != nil {
				return e
			}
			return json.NewEncoder(os.Stdout).Encode(evidence)
		case "repair":
			w, err = store.RepairWorkflow(w.ID, *note)
		case "next", "run":
			config.Providers = w.Providers
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
			for steps := 0; steps < len(w.Recipe.Stages); steps++ {
				if w.State != "ready" {
					break
				}
				stage := w.Recipe.Stages[w.Current]
				if stage.Kind == "github-create" || stage.Kind == "github-close" {
					w, err = store.RunGitHubStage(ctx, w.ID, core.GitHubCLI{})
				} else {
					w, err = store.RunWorkflow(ctx, w.ID, *edits, runner.Run, emit)
				}
				if err != nil || action == "next" {
					break
				}
			}
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
		if stage.Model != "" {
			provider += " / " + stage.Model
		}
		fmt.Printf("%s %-22s %s\n", marker, stage.Name, provider)
	}
	if w.Recipe.AllowSelfTest {
		fmt.Println("Independence policy explicitly relaxed for this recipe.")
	}
	for _, issue := range w.Issues {
		fmt.Println("Issue:", issue.URL)
	}
	for _, check := range w.Recipe.Checks {
		fmt.Printf("Check: %s (%s) %s\n", check.Name, check.Kind, strings.Join(check.Command, " "))
	}
	if w.Recipe.NoTDDReason != "" {
		fmt.Println("TDD exception:", w.Recipe.NoTDDReason)
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
