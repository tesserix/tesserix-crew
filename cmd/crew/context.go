// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tesserix/tesserix-crew/internal/core"
)

func contextCommand(args []string) error {
	action := args[0]
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
	flags := flag.NewFlagSet("context "+action, flag.ContinueOnError)
	home := flags.String("home", defaultHome, "Crew home")
	repo := flags.String("repo", wd, "repository")
	file := flags.String("file", "", "reviewed summary input file")
	output := flags.String("output", "", "write the extractive draft to this file")
	query := flags.String("query", "", "history search query")
	limit := flags.Int("limit", 20, "maximum search results")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() < 1 {
		return fmt.Errorf("usage: crew context %s [options] SESSION [TEXT/PIN_ID]", action)
	}
	id := flags.Arg(0)
	if action != "pin" && action != "unpin" && flags.NArg() != 1 {
		return fmt.Errorf("unexpected context arguments")
	}
	store, err := core.OpenStore(*home)
	if err != nil {
		return err
	}
	defer store.Close()
	session, err := store.Get(id)
	if err != nil {
		return err
	}
	config, err := core.LoadConfig(*home, session.Repo)
	if err != nil && action == "status" {
		return err
	}
	runner := core.Runner{Store: store, Home: *home, Config: config}
	_ = repo // Existing session paths are authoritative for history commands.
	switch action {
	case "status":
		v, err := runner.ContextUsage(session)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(v)
	case "draft":
		text := ""
		if *file != "" {
			data, err := os.ReadFile(*file)
			if err != nil {
				return err
			}
			text = config.Redact(string(data))
		}
		v, err := store.DraftContext(id, text)
		if err != nil {
			return err
		}
		if *output != "" {
			return os.WriteFile(*output, []byte(v.Content), 0600)
		}
		fmt.Print(v.Content)
		fmt.Printf("\nReview/edit this draft, then import with context draft --file PATH %s and explicitly run context apply %s.\n", id, id)
	case "apply":
		v, err := store.ContextDraft(id)
		if err != nil {
			return err
		}
		if err := store.ApplyContextSummary(id, v.Digest()); err != nil {
			return err
		}
		fmt.Println("Reviewed summary applied; the full history remains saved.")
	case "clear":
		return store.ClearContextSummary(id)
	case "pins":
		v, err := store.ContextPins(id)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(v)
	case "pin":
		pin, err := store.PinContext(id, config.Redact(strings.Join(flags.Args()[1:], " ")))
		if err != nil {
			return err
		}
		fmt.Println("Pinned", pin)
	case "unpin":
		if flags.NArg() != 2 {
			return fmt.Errorf("unpin needs SESSION PIN_ID")
		}
		pin, err := strconv.ParseInt(flags.Arg(1), 10, 64)
		if err != nil {
			return err
		}
		return store.UnpinContext(id, pin)
	case "search":
		events, err := store.SearchContext(id, *query, *limit)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(events)
	}
	return nil
}
