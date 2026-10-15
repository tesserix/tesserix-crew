// SPDX-License-Identifier: Apache-2.0
package core

import (
	"fmt"
	"strings"
)

// ResolveAgent selects only before execution. It never retries a failed task on
// another provider, and explicit provider/model selections remain binding.
func (c Config) ResolveAgent(task string, options AgentOptions) (string, string, error) {
	if options.Agent != "" && options.Agent != "auto" {
		if err := c.CheckProvider(options); err != nil {
			return "", "", err
		}
		return options.Agent, "explicit selection; local prerequisites verified", nil
	}
	type candidate struct{ agent, reason string }
	var candidates []candidate
	for _, rule := range c.Rules {
		for _, term := range rule.Contains {
			if strings.TrimSpace(term) != "" && strings.Contains(strings.ToLower(task), strings.ToLower(term)) {
				candidates = append(candidates, candidate{rule.Agent, "configured task rule"})
				break
			}
		}
	}
	if c.DefaultAgent != "" {
		candidates = append(candidates, candidate{c.DefaultAgent, "configured default"})
	}
	// A model identifier can be provider-specific. Do not move it across providers.
	if options.Model != "" {
		if len(candidates) == 0 {
			return "", "", fmt.Errorf("select a provider before pinning a provider-specific model")
		}
		candidates = candidates[:1]
	} else {
		for _, name := range c.ProviderNames() {
			candidates = append(candidates, candidate{name, "available provider fallback"})
		}
	}
	seen := map[string]bool{}
	var unavailable []string
	for _, item := range candidates {
		if seen[item.agent] {
			continue
		}
		seen[item.agent] = true
		probe := options
		probe.Agent = item.agent
		if err := c.CheckProvider(probe); err != nil {
			unavailable = append(unavailable, item.agent+": "+err.Error())
			continue
		}
		reason := item.reason + "; local prerequisites verified"
		if len(unavailable) > 0 {
			reason += "; skipped " + strings.Join(unavailable, "; ")
		}
		return item.agent, reason, nil
	}
	return "", "", fmt.Errorf("no eligible provider for auto mode: %s; run crew doctor or select an installed provider", strings.Join(unavailable, "; "))
}
