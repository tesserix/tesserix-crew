// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tesserix/tesserix-crew/internal/core"
	"github.com/tesserix/tesserix-crew/internal/ui"
)

var version = "0.3.2"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "crew:", err)
		os.Exit(1)
	}
}
func help() {
	fmt.Println(`Tesserix Crew — one session across your subscribed coding agents

  crew [--agent auto|claude|codex|agy|grok|PROVIDER] [--allow-edits]
  crew run [options] "task"
  crew resume [options] [SESSION]
  crew sessions [options]
  crew context [options] SESSION
  crew doctor
  crew skills add [--global] PATH
  crew skills list|show|edit|update|remove|enable|disable|refresh
  crew providers list|inspect|add|enable|disable
  crew workflow start|status|next|approve|reject|record|retry|recover|list|presets
  crew version

Options: --repo PATH, --home PATH, --agent NAME, --model NAME,
         --session ID, --allow-edits, --dry-run

Interactive: /agent, /model, /skills, /status, /context, /help, /quit
Native CLI authentication is reused. No API keys are required by Crew.
Initial headless integrations default to read-only. --allow-edits enables
native edit permissions; commands requiring approval may still be denied.`)
}
func run(ctx context.Context, args []string) error {
	verb := "chat"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb = args[0]
		args = args[1:]
	}
	if verb == "version" {
		fmt.Println("crew", version)
		return nil
	}
	if verb == "help" || (len(args) > 0 && (args[0] == "--help" || args[0] == "-h")) {
		help()
		return nil
	}
	if verb == "doctor" {
		return providers(append([]string{"list"}, args...))
	}
	if verb == "workflow" {
		return workflow(ctx, args)
	}
	if verb == "providers" {
		return providers(args)
	}
	if verb == "skills" {
		return skills(ctx, args)
	}
	switch verb {
	case "chat", "run", "resume", "sessions", "context", "mcp":
	default:
		return fmt.Errorf("unknown command %q; use crew help", verb)
	}
	wd, e := os.Getwd()
	if e != nil {
		return e
	}
	userHome, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	defaultHome := os.Getenv("CREW_HOME")
	if defaultHome == "" {
		defaultHome = filepath.Join(userHome, ".crew")
	}
	flags := flag.NewFlagSet(verb, flag.ContinueOnError)
	repo := flags.String("repo", wd, "repository path")
	home := flags.String("home", defaultHome, "Crew data directory")
	agent := flags.String("agent", "auto", "agent selection")
	model := flags.String("model", "", "native model identifier")
	sid := flags.String("session", "", "session to continue")
	edits := flags.Bool("allow-edits", false, "enable native edit permissions")
	dry := flags.Bool("dry-run", false, "show planned command without starting an agent")
	depth := flags.Int("depth", 0, "internal delegation depth")
	if e = flags.Parse(args); e != nil {
		return e
	}
	*repo, e = filepath.Abs(*repo)
	if e != nil {
		return e
	}
	*home, e = filepath.Abs(*home)
	if e != nil {
		return e
	}
	info, e := os.Stat(*repo)
	if e != nil {
		return e
	}
	if !info.IsDir() {
		return fmt.Errorf("repository is not a directory")
	}
	config := core.Config{DefaultAgent: "claude"}
	if verb != "mcp" {
		config, e = core.LoadConfig(*home, *repo)
		if e != nil {
			return e
		}
	}
	if verb == "run" && flags.NArg() == 0 {
		return fmt.Errorf("usage: crew run [options] \"task\"")
	}
	if *dry {
		task := strings.Join(flags.Args(), " ")
		chosen, why, e := config.Route(task, *agent)
		if e != nil {
			return e
		}
		command, e := config.Command(core.AgentOptions{Agent: chosen, Model: *model, Repo: *repo, Edits: *edits})
		if e != nil {
			return e
		}
		b, _ := json.MarshalIndent(map[string]any{"agent": chosen, "reason": why, "repo": *repo, "command": command, "note": "Execution also adds shared session context and parent delegation MCP configuration."}, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	store, e := core.OpenStore(*home)
	if e != nil {
		return e
	}
	defer store.Close()
	if verb == "sessions" {
		rows, e := store.List()
		if e != nil {
			return e
		}
		for _, s := range rows {
			fmt.Printf("%s  %-7s  %s  %s", s.ID, s.Agent, s.Updated, s.Repo)
			if s.Parent != "" {
				fmt.Printf("  child of %s", s.Parent)
			}
			fmt.Println()
		}
		return nil
	}
	if verb == "context" {
		if flags.NArg() != 1 {
			return fmt.Errorf("usage: crew context SESSION")
		}
		events, e := store.Events(flags.Arg(0))
		if e != nil {
			return e
		}
		if _, e = store.Get(flags.Arg(0)); e != nil {
			return e
		}
		for _, v := range events {
			if v.Kind != "raw" {
				fmt.Printf("[%s / %s]\n%s\n\n", v.Kind, v.Agent, v.Content)
			}
		}
		return nil
	}
	var session core.Session
	if *sid != "" {
		session, e = store.Get(*sid)
	} else if verb == "resume" {
		if flags.NArg() > 0 {
			session, e = store.Get(flags.Arg(0))
		} else {
			session, e = store.Latest(*repo)
		}
	} else if verb == "mcp" {
		return fmt.Errorf("mcp requires --session")
	} else {
		chosen, _, routeErr := config.Route(strings.Join(flags.Args(), " "), *agent)
		if routeErr != nil {
			return routeErr
		}
		session, e = store.Create(*repo, chosen, "")
	}
	if e != nil {
		return e
	}
	if session.Repo != *repo {
		return fmt.Errorf("session belongs to %s; select it with --repo", session.Repo)
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	runner := &core.Runner{Store: store, Home: *home, Executable: executable, Config: config}
	if w, err := store.Workflow(session.ID); err == nil {
		runner.Config.Providers = w.Providers
	}
	options := core.RunOptions{Agent: *agent, Model: *model, Edits: *edits, Depth: *depth}
	if verb == "mcp" {
		if w, err := store.Workflow(session.ID); err == nil {
			runner.Config.Providers = w.Providers
		} else {
			runner.Config, e = core.LoadConfig(*home, *repo)
			if e != nil {
				return e
			}
		}
		return runner.ServeMCP(ctx, session, *depth, os.Stdin, os.Stdout)
	}
	if verb == "resume" && *agent == "auto" {
		options.Agent = session.Agent
	}
	if verb == "run" {
		options.Task = strings.Join(flags.Args(), " ")
		fmt.Fprintf(os.Stderr, "Session %s\n", session.ID)
		_, e = runner.Run(ctx, session, options, func(v core.Update) { printUpdate(os.Stdout, os.Stderr, v) })
		return e
	}
	return ui.Launch(ctx, runner, session, options)
}

func printUpdate(out, diagnostics io.Writer, v core.Update) {
	switch v.Kind {
	case "delta":
		fmt.Fprint(out, v.Text)
	case "text_end":
		fmt.Fprintln(out)
	case "text":
		fmt.Fprintln(out, v.Text)
	default:
		fmt.Fprintf(diagnostics, "[%s] %s\n", v.Agent, v.Text)
	}
}
