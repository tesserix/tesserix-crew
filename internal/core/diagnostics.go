// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Diagnostic struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	Action string `json:"action,omitempty"`
}

type SetupReport struct {
	Repo         string       `json:"repo"`
	Home         string       `json:"home"`
	DefaultAgent string       `json:"default_agent"`
	Checks       []Diagnostic `json:"checks"`
}

type diagnosticOutput struct{ bytes.Buffer }

func (b *diagnosticOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := max(0, 4096-b.Len())
	_, _ = b.Buffer.Write(data[:min(n, remaining)])
	return n, nil
}

func providerExecutable(name string, p Provider) string {
	if p.Kind == "builtin" {
		return name
	}
	if len(p.Command) > 0 {
		return p.Command[0]
	}
	return ""
}

func providerLogin(name string) string {
	switch name {
	case "claude":
		return "Run claude auth login, then crew doctor --check-auth"
	case "codex":
		return "Run codex login, then crew doctor --check-auth"
	default:
		return "Authenticate in the provider's native CLI; Crew reuses its login"
	}
}

// InspectSetup does not run model tasks, test commands or change configuration.
// Optional native login probes have bounded timeouts and never expose output.
func InspectSetup(ctx context.Context, home, repo string, checkAuth bool) SetupReport {
	report := SetupReport{Repo: repo, Home: home, Checks: []Diagnostic{}}
	add := func(name, state, detail, action string) {
		report.Checks = append(report.Checks, Diagnostic{name, state, detail, action})
	}
	config, err := LoadConfig(home, repo)
	if err != nil {
		add("configuration", "error", err.Error(), "Fix the config.toml error and run crew doctor again")
		return report
	}
	report.DefaultAgent = config.DefaultAgent
	add("configuration", "ok", "Default provider: "+config.DefaultAgent, "")
	info, err := os.Stat(repo)
	if err != nil || !info.IsDir() {
		add("repository", "error", "Repository directory is unavailable", "Use --repo with an existing project directory")
		return report
	}
	branch, changes := GitStatus(repo)
	if branch == "no git" || branch == "unknown" {
		add("repository", "warning", "No Git branch detected", "Start inside a Git project or use --repo PATH")
	} else {
		add("repository", "ok", fmt.Sprintf("Branch %s; %d changed entries", branch, changes), "")
	}
	for _, name := range config.ProviderNames() {
		p := config.Providers[name]
		label := "provider:" + name
		if p.Disabled {
			add(label, "disabled", "Disabled or adapter contract unverified", "")
			continue
		}
		capabilities := "Capabilities: " + strings.Join(p.Capabilities, ", ")
		if p.Kind == "api" {
			if p.Auth == "api-key" && os.Getenv(p.KeyEnv) == "" {
				add(label, "error", "API key environment variable is unset; "+capabilities, "Set "+p.KeyEnv+" in your environment")
			} else {
				add(label, "available", "Configured; endpoint not contacted. "+capabilities, "Use a small explicit task to verify model access")
			}
			continue
		}
		executable := providerExecutable(name, p)
		if _, err := exec.LookPath(executable); err != nil {
			add(label, "missing", "Executable not found: "+executable, "Install the native CLI or correct the configured command")
			continue
		}
		if p.Auth == "api-key" && os.Getenv(p.KeyEnv) == "" {
			add(label, "error", "API key environment variable is unset", "Set "+p.KeyEnv+" in your environment")
			continue
		}
		add(label, "available", capabilities, "")
		if p.Auth != "subscription" {
			continue
		}
		args := []string(nil)
		if p.Kind == "builtin" {
			switch name {
			case "claude":
				args = []string{"auth", "status"}
			case "codex":
				args = []string{"login", "status"}
			}
		}
		if !checkAuth || len(args) == 0 {
			add("login:"+name, "unverified", "Native login has not been verified", providerLogin(name))
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		cmd := exec.CommandContext(probeCtx, executable, args...)
		cmd.Dir = repo
		cmd.WaitDelay = time.Second
		var output diagnosticOutput
		cmd.Stdout = &output
		err := cmd.Run()
		cancel()
		if err != nil {
			add("login:"+name, "unverified", "Native login status probe failed or is unsupported", providerLogin(name))
		} else if name == "claude" {
			var status struct {
				LoggedIn *bool `json:"loggedIn"`
			}
			if json.Unmarshal(output.Bytes(), &status) != nil || status.LoggedIn == nil {
				add("login:"+name, "unverified", "Native status response was not recognized", providerLogin(name))
			} else if !*status.LoggedIn {
				add("login:"+name, "error", "Native CLI reports no active login", providerLogin(name))
			} else {
				add("login:"+name, "ok", "Native CLI reports an active login; model access not tested", "")
			}
		} else {
			add("login:"+name, "ok", "Native login status probe succeeded; model access not tested", "")
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		add("github", "missing", "GitHub CLI is unavailable", "Install gh and run gh auth login for GitHub workflow stages")
	} else if checkAuth {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		cmd := exec.CommandContext(probeCtx, "gh", "auth", "status")
		cmd.Dir = repo
		cmd.WaitDelay = time.Second
		err := cmd.Run()
		cancel()
		if err != nil {
			add("github", "unverified", "GitHub authentication probe failed", "Run gh auth login and check access to the target repository")
		} else {
			add("github", "ok", "GitHub authentication probe succeeded; repository permissions not tested", "")
		}
	} else {
		add("github", "unverified", "gh is installed; login not verified", "Run crew doctor --check-auth")
	}
	checks := config.Checks
	if len(checks) == 0 {
		checks = InferChecks(repo)
	}
	if len(checks) == 0 {
		add("tests", "unverified", "No executable checks configured or inferred", "Configure checks in .crew/config.toml before starting a TDD workflow")
	}
	for _, check := range checks {
		command := check.Command[0]
		if strings.ContainsRune(command, filepath.Separator) && !filepath.IsAbs(command) {
			command = filepath.Join(repo, command)
		}
		if _, err := exec.LookPath(command); err != nil {
			add("check:"+check.Name, "missing", "Executable unavailable: "+check.Command[0], "Install the test tool or correct the configured check")
		} else {
			add("check:"+check.Name, "available", strings.Join(check.Command, " ")+" (not executed)", "")
		}
	}
	add("browser/e2e", "unverified", "Browser dependencies and artifacts require project-specific checks", "Configure e2e/browser checks; browser checks need screenshot or trace artifact paths")
	return report
}

func SetDefaultProvider(home, repo, name string, global bool) error {
	config, err := LoadConfig(home, repo)
	if err != nil {
		return err
	}
	if _, err := config.Provider(name); err != nil {
		return err
	}
	return ModifyConfig(home, repo, global, func(document map[string]any) error {
		document["default_agent"] = name
		return nil
	})
}
