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
}

func ValidAgent(v string) bool { return v == "claude" || v == "codex" || v == "gemini" }
func LoadConfig(home, repo string) (Config, error) {
	out := Config{DefaultAgent: "claude", Lifecycles: BuiltinLifecycles()}
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
		out.Rules = append(next.Rules, out.Rules...)
		for name, recipe := range next.Lifecycles {
			out.Lifecycles[name] = recipe
		}
	}
	if !ValidAgent(out.DefaultAgent) {
		return out, fmt.Errorf("invalid default agent: %s", out.DefaultAgent)
	}
	for _, r := range out.Rules {
		if !ValidAgent(r.Agent) || len(r.Contains) == 0 {
			return out, fmt.Errorf("routing rules need a valid agent and nonempty contains list")
		}
		for _, v := range r.Contains {
			if strings.TrimSpace(v) == "" {
				return out, fmt.Errorf("empty routing term")
			}
		}
	}
	for name, recipe := range out.Lifecycles {
		if strings.TrimSpace(name) == "" {
			return out, fmt.Errorf("lifecycle name is empty")
		}
		if err := recipe.Validate(); err != nil {
			return out, fmt.Errorf("lifecycle %s: %w", name, err)
		}
	}
	return out, nil
}
func (c Config) Route(prompt, explicit string) (string, string, error) {
	if explicit != "" && explicit != "auto" {
		if !ValidAgent(explicit) {
			return "", "", fmt.Errorf("unknown agent: %s", explicit)
		}
		return explicit, "explicit selection", nil
	}
	for _, r := range c.Rules {
		for _, term := range r.Contains {
			if strings.Contains(strings.ToLower(prompt), strings.ToLower(term)) {
				return r.Agent, "configured task rule", nil
			}
		}
	}
	return c.DefaultAgent, "configured default", nil
}
