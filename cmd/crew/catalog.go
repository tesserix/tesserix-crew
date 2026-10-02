// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/tesserix/tesserix-crew/internal/core"
)

type catalogOptions struct {
	home, repo                  string
	global, apply               bool
	sourceURL, revision, subdir string
	session                     string
	args                        []string
}

func parseCatalog(verb string, args []string) (catalogOptions, error) {
	var o catalogOptions
	wd, err := os.Getwd()
	if err != nil {
		return o, err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return o, err
	}
	defaultHome := os.Getenv("CREW_HOME")
	if defaultHome == "" {
		defaultHome = filepath.Join(userHome, ".crew")
	}
	flags := flag.NewFlagSet(verb, flag.ContinueOnError)
	flags.StringVar(&o.home, "home", defaultHome, "Crew home")
	flags.StringVar(&o.repo, "repo", wd, "repository")
	flags.BoolVar(&o.global, "global", false, "global scope")
	flags.BoolVar(&o.apply, "apply", false, "apply the previewed session refresh")
	flags.StringVar(&o.session, "session", "", "session selection")
	flags.StringVar(&o.sourceURL, "git", "", "Git skill source URL")
	flags.StringVar(&o.revision, "ref", "", "full pinned Git commit hash")
	flags.StringVar(&o.subdir, "path", "", "skill directory in Git repository")
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	o.args = flags.Args()
	o.home, err = filepath.Abs(o.home)
	if err != nil {
		return o, err
	}
	o.repo, err = filepath.Abs(o.repo)
	return o, err
}
func skills(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: crew skills add|list|show|edit|update|remove|enable|disable|refresh [options] [NAME/PATH]")
	}
	action := args[0]
	o, err := parseCatalog("skills "+action, args[1:])
	if err != nil {
		return err
	}
	if action == "add" && o.sourceURL != "" {
		if len(o.args) != 0 {
			return fmt.Errorf("Git import does not take a local path")
		}
		target, err := core.ImportGitSkill(ctx, o.home, o.repo, o.sourceURL, o.revision, o.subdir, o.global)
		if err != nil {
			return err
		}
		fmt.Println("Imported pinned skill:", target)
		return nil
	}
	if action == "add" {
		if len(o.args) != 1 {
			return fmt.Errorf("usage: crew skills add [--global] PATH")
		}
		source, err := filepath.Abs(o.args[0])
		if err != nil {
			return err
		}
		target := filepath.Join(core.SkillRoot(o.home, o.repo, o.global), filepath.Base(source))
		if err := core.CopySkill(source, target); err != nil {
			return err
		}
		fmt.Println("Installed", target, "· existing sessions remain pinned")
		return nil
	}
	if action == "refresh" {
		if o.session == "" || len(o.args) != 0 {
			return fmt.Errorf("usage: crew skills refresh --session ID [--apply]")
		}
		store, err := core.OpenStore(o.home)
		if err != nil {
			return err
		}
		defer store.Close()
		session, err := store.Get(o.session)
		if err != nil {
			return err
		}
		if session.Repo != o.repo {
			return fmt.Errorf("session belongs to %s; use --repo", session.Repo)
		}
		runner := core.Runner{Store: store, Home: o.home}
		changes, err := runner.RefreshSkills(session, o.apply)
		for _, change := range changes {
			fmt.Println(change)
		}
		if err != nil {
			return err
		}
		if !o.apply {
			fmt.Println("Preview only. Repeat with --apply to update this session's pinned instructions.")
		}
		return nil
	}
	catalog, states, err := core.SkillCatalog(o.home, o.repo)
	if err != nil {
		return err
	}
	if action == "list" {
		if len(o.args) != 0 {
			return fmt.Errorf("skills list takes no name")
		}
		for _, name := range core.SortedSkillNames(catalog) {
			state := "enabled"
			if states[name] {
				state = "disabled"
			}
			fmt.Printf("%-18s %-8s %s\n", name, state, catalog[name])
		}
		return nil
	}
	if action == "update" {
		if len(o.args) != 2 {
			return fmt.Errorf("usage: crew skills update [--global] NAME PATH")
		}
		source, err := filepath.Abs(o.args[1])
		if err != nil {
			return err
		}
		if err := core.UpdateSkill(o.home, o.repo, o.args[0], source, o.global); err != nil {
			return err
		}
		fmt.Println("Updated catalog; existing sessions remain pinned")
		return nil
	}
	if len(o.args) != 1 {
		return fmt.Errorf("usage: crew skills %s [options] NAME", action)
	}
	name := o.args[0]
	source, ok := catalog[name]
	if !ok {
		return fmt.Errorf("unknown skill %s", name)
	}
	switch action {
	case "show":
		data, err := os.ReadFile(filepath.Join(source, "SKILL.md"))
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "enable", "disable":
		return core.SetSkillEnabled(o.home, o.repo, name, o.global, action == "enable")
	case "remove":
		archive, err := core.RemoveSkill(o.home, o.repo, name, o.global)
		if err != nil {
			return err
		}
		fmt.Println("Catalog copy archived:", archive, "· session snapshots retained")
		return nil
	case "edit":
		target := filepath.Join(core.SkillRoot(o.home, o.repo, o.global), name)
		if filepath.Clean(source) != filepath.Clean(target) {
			if err := core.CopySkill(source, target); err != nil {
				return err
			}
		}
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vi"
		}
		parts := strings.Fields(editor)
		if len(parts) == 0 {
			parts = []string{"vi"}
		}
		command := exec.Command(parts[0], append(parts[1:], filepath.Join(target, "SKILL.md"))...)
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return err
		}
		fmt.Println("Edited catalog; existing sessions remain pinned")
		return nil
	default:
		return fmt.Errorf("unknown skills command %s", action)
	}
}
func providers(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: crew providers list|inspect|add|enable|disable [options] [NAME/FILE]")
	}
	action := args[0]
	o, err := parseCatalog("providers "+action, args[1:])
	if err != nil {
		return err
	}
	if action == "add" {
		if len(o.args) != 1 {
			return fmt.Errorf("usage: crew providers add [--global] MANIFEST.toml")
		}
		data, err := os.ReadFile(o.args[0])
		if err != nil {
			return err
		}
		var manifest struct {
			ID       string        `toml:"id"`
			Provider core.Provider `toml:"provider"`
		}
		decoder := toml.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&manifest); err != nil {
			return err
		}
		if err := core.AddProvider(o.home, o.repo, manifest.ID, manifest.Provider, o.global); err != nil {
			return err
		}
		fmt.Println("Added provider", manifest.ID, "· existing lifecycle runs retain pinned adapters")
		return nil
	}
	config, err := core.LoadConfig(o.home, o.repo)
	if err != nil {
		return err
	}
	if action == "list" {
		for _, name := range config.ProviderNames() {
			p := config.Providers[name]
			status := "available"
			if p.Disabled {
				status = "disabled/unverified"
			} else if err := config.CheckProvider(core.AgentOptions{Agent: name, Repo: o.repo}); err != nil {
				status = err.Error()
			}
			fmt.Printf("%-14s %-12s %s\n", name, p.Auth, status)
		}
		return nil
	}
	if len(o.args) != 1 {
		return fmt.Errorf("usage: crew providers %s [options] NAME", action)
	}
	name := o.args[0]
	switch action {
	case "enable", "disable":
		return core.SetProviderDisabled(o.home, o.repo, name, o.global, action == "disable")
	case "inspect":
		p, ok := config.Providers[name]
		if !ok {
			return fmt.Errorf("unknown provider %s", name)
		}
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(config.Redact(string(data)))
		return nil
	default:
		return fmt.Errorf("unknown providers command %s", action)
	}
}
