// SPDX-License-Identifier: Apache-2.0
package core

import (
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"strings"
)

type Rule struct {
	Contains []string `toml:"contains"`
	Agent    string   `toml:"agent"`
}
type Config struct {
	DefaultAgent string               `toml:"default_agent"`
	Rules        []Rule               `toml:"rules"`
	Lifecycles   map[string]Lifecycle `toml:"lifecycles"`
	GitHubRepo   string               `toml:"github_repo"`
	Checks       []Check              `toml:"checks"`
	Providers    map[string]Provider  `toml:"providers"`
}

func ValidAgent(v string) bool { return v == "claude" || v == "codex" || v == "gemini" }
func LoadConfig(home, repo string) (Config, error) {
	out := Config{DefaultAgent: "claude", Lifecycles: BuiltinLifecycles(), Providers: BuiltinProviders()}
	for _, path := range []string{filepath.Join(home, "config.toml"), filepath.Join(repo, ".crew", "config.toml")} {
		b, e := os.ReadFile(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return out, e
		}
		var next Config
		if e = toml.Unmarshal(b, &next); e != nil {
			return out, fmt.Errorf("%s: %w", path, e)
		}
		if next.DefaultAgent != "" {
			out.DefaultAgent = next.DefaultAgent
		}
		if next.GitHubRepo != "" {
			out.GitHubRepo = next.GitHubRepo
		}
		if next.Checks != nil {
			out.Checks = next.Checks
		}
		out.Rules = append(next.Rules, out.Rules...)
		for name, provider := range next.Providers {
			out.Providers[name] = mergeProvider(out.Providers[name], provider)
		}
		for name, recipe := range next.Lifecycles {
			out.Lifecycles[name] = recipe
		}
	}
	if _, err := out.Provider(out.DefaultAgent); err != nil {
		return out, fmt.Errorf("invalid default agent: %s", out.DefaultAgent)
	}
	for _, r := range out.Rules {
		if _, known := out.Providers[r.Agent]; !known || len(r.Contains) == 0 {
			return out, fmt.Errorf("routing rules need a valid agent and nonempty contains list")
		}
		for _, v := range r.Contains {
			if strings.TrimSpace(v) == "" {
				return out, fmt.Errorf("empty routing term")
			}
		}
	}
	if out.GitHubRepo != "" && !githubSlug.MatchString(out.GitHubRepo) {
		return out, fmt.Errorf("github_repo must be owner/repo")
	}
	for _, check := range out.Checks {
		if err := check.Validate(); err != nil {
			return out, err
		}
	}
	for name, p := range out.Providers {
		if err := p.Validate(name); err != nil {
			return out, err
		}
	}
	for name, recipe := range out.Lifecycles {
		if strings.TrimSpace(name) == "" {
			return out, fmt.Errorf("lifecycle name is empty")
		}
		if err := recipe.ValidateWithProviders(out.Providers); err != nil {
			return out, fmt.Errorf("lifecycle %s: %w", name, err)
		}
	}
	return out, nil
}
func (c Config) Route(prompt, explicit string) (string, string, error) {
	if explicit != "" && explicit != "auto" {
		if _, err := c.Provider(explicit); err != nil {
			return "", "", err
		}
		return explicit, "explicit selection", nil
	}
	for _, r := range c.Rules {
		for _, term := range r.Contains {
			if strings.Contains(strings.ToLower(prompt), strings.ToLower(term)) {
				if _, err := c.Provider(r.Agent); err == nil {
					return r.Agent, "configured task rule", nil
				}
			}
		}
	}
	return c.DefaultAgent, "configured default", nil
}
