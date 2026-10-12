// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tesserix/tesserix-crew/internal/core"
	"github.com/tesserix/tesserix-crew/internal/ui"
)

func setup(ctx context.Context, verb string, args []string) error {
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
	flags := flag.NewFlagSet(verb, flag.ContinueOnError)
	repo := flags.String("repo", wd, "project directory")
	home := flags.String("home", defaultHome, "Crew home")
	asJSON := flags.Bool("json", false, "machine-readable diagnostic report")
	checkAuth := flags.Bool("check-auth", false, "probe native login status without model calls")
	var agent string
	var global bool
	if verb == "setup" {
		flags.StringVar(&agent, "agent", "", "default provider; omit for interactive selection")
		flags.BoolVar(&global, "global", false, "save the global default instead of the project default")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s does not take positional arguments", verb)
	}
	if verb == "setup" && *asJSON {
		return fmt.Errorf("use crew doctor --json for diagnostics, or crew setup --agent NAME for noninteractive setup")
	}
	*repo, err = filepath.Abs(*repo)
	if err != nil {
		return err
	}
	*home, err = filepath.Abs(*home)
	if err != nil {
		return err
	}
	report := core.InspectSetup(ctx, *home, *repo, *checkAuth)
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	fmt.Printf("Crew diagnostics · %s\nData: %s\n", report.Repo, report.Home)
	for _, item := range report.Checks {
		fmt.Printf("%-12s %-20s %s\n", item.State, item.Name, item.Detail)
		if item.Action != "" {
			fmt.Println("  →", item.Action)
		}
	}
	if verb != "setup" {
		return nil
	}
	info, err := os.Stat(*repo)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("setup needs an existing project directory")
	}
	config, err := core.LoadConfig(*home, *repo)
	if err != nil {
		return err
	}
	if agent == "" {
		choices := []ui.Choice{}
		for _, name := range config.ProviderNames() {
			if err := config.CheckProvider(core.AgentOptions{Agent: name, Repo: *repo}); err != nil {
				continue
			}
			p := config.Providers[name]
			choices = append(choices, ui.Choice{ID: name, Label: name, Detail: p.Auth + " · " + strings.Join(p.Capabilities, ", ")})
		}
		if len(choices) == 0 {
			return fmt.Errorf("no available providers; follow the diagnostic actions above and rerun crew setup")
		}
		agent, err = ui.Select(ctx, "Choose the default provider · Enter saves your choice", choices)
		if err != nil || agent == "" {
			return err
		}
	}
	if err := core.SetDefaultProvider(*home, *repo, agent, global); err != nil {
		return err
	}
	fmt.Printf("Saved default provider %s to %s\n", agent, core.ConfigPath(*home, *repo, global))
	if global {
		effective, err := core.LoadConfig(*home, *repo)
		if err != nil {
			return err
		}
		if effective.DefaultAgent != agent {
			fmt.Printf("This project's override still selects %s. Run crew setup --agent %s to change it.\n", effective.DefaultAgent, agent)
		}
	}
	fmt.Println("Next: crew doctor --check-auth, then crew to start a session.")
	fmt.Println("In Crew: /sessions resumes work; /workflow start opens the guided workflow menu.")
	return nil
}
