// SPDX-License-Identifier: Apache-2.0
package core

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Provider describes a CLI's verified command/event contract. Credentials are
// inherited from login state or a named environment variable, never argv.
type Provider struct {
	Kind           string   `toml:"kind" json:"kind"`
	Command        []string `toml:"command" json:"command,omitempty"`
	Format         string   `toml:"format" json:"format"`
	Auth           string   `toml:"auth" json:"auth"`
	KeyEnv         string   `toml:"api_key_env" json:"api_key_env,omitempty"`
	ModelArgs      []string `toml:"model_args" json:"model_args,omitempty"`
	ResumeArgs     []string `toml:"resume_args" json:"resume_args,omitempty"`
	ReadArgs       []string `toml:"read_only_args" json:"read_only_args,omitempty"`
	EditArgs       []string `toml:"edit_args" json:"edit_args,omitempty"`
	MCPArgs        []string `toml:"mcp_args" json:"mcp_args,omitempty"`
	Capabilities   []string `toml:"capabilities" json:"capabilities"`
	Endpoint       string   `toml:"endpoint" json:"endpoint,omitempty"`
	DefaultModel   string   `toml:"model" json:"model,omitempty"`
	RequestTimeout string   `toml:"request_timeout" json:"request_timeout,omitempty"`
	MaxTokens      int      `toml:"max_tokens" json:"max_tokens,omitempty"`
	MaxRounds      int      `toml:"max_rounds" json:"max_rounds,omitempty"`
	Disabled       bool     `toml:"disabled" json:"disabled,omitempty"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
var envIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func BuiltinProviders() map[string]Provider {
	return map[string]Provider{
		"claude": {Kind: "builtin", Format: "claude", Auth: "subscription", Capabilities: []string{"read", "write", "tools", "resume", "stream", "delegate"}},
		"codex":  {Kind: "builtin", Format: "codex", Auth: "subscription", Capabilities: []string{"read", "write", "tools", "resume", "delegate"}},
		"gemini": {Kind: "builtin", Format: "gemini", Auth: "subscription", Disabled: true, Capabilities: []string{"read"}},
	}
}
func (p Provider) Has(capability string) bool {
	for _, c := range p.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}
func (p Provider) Validate(id string) error {
	if !identifier.MatchString(id) {
		return fmt.Errorf("invalid provider identifier %q", id)
	}
	if p.Disabled {
		return nil
	}
	if p.Kind != "builtin" && p.Kind != "cli" && p.Kind != "api" {
		return fmt.Errorf("provider %s needs kind builtin, cli or api", id)
	}
	if p.Kind == "builtin" {
		if id != "claude" && id != "codex" {
			return fmt.Errorf("provider %s has no verified built-in adapter", id)
		}
	} else if p.Kind == "cli" && (len(p.Command) == 0 || strings.TrimSpace(p.Command[0]) == "") {
		return fmt.Errorf("provider %s needs a command array", id)
	} else if p.Kind == "api" {
		endpoint, err := url.Parse(p.Endpoint)
		if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return fmt.Errorf("provider %s needs a credential-free endpoint URL", id)
		}
		if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && (endpoint.Hostname() == "localhost" || endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "::1")) {
			return fmt.Errorf("API endpoints need HTTPS except localhost test servers")
		}
		if p.Format != "crew" {
			return fmt.Errorf("API providers use crew event format")
		}
		if p.DefaultModel == "" {
			return fmt.Errorf("API provider %s needs model", id)
		}
		if p.Has("resume") || p.Has("stream") {
			return fmt.Errorf("API runtime uses shared context and completed messages, not native resume or streaming")
		}
		if p.RequestTimeout != "" {
			d, err := time.ParseDuration(p.RequestTimeout)
			if err != nil || d <= 0 {
				return fmt.Errorf("invalid API request_timeout")
			}
		}
		if p.MaxRounds < 0 || p.MaxRounds > 100 || p.MaxTokens < 0 {
			return fmt.Errorf("invalid API token/round limits")
		}
	} else if len(p.Command) == 0 || strings.TrimSpace(p.Command[0]) == "" {
		return fmt.Errorf("provider %s needs a command array", id)
	}
	switch p.Format {
	case "claude", "codex", "crew", "text":
	default:
		return fmt.Errorf("provider %s has unknown event format %q", id, p.Format)
	}
	if p.Kind == "api" && p.Auth == "subscription" {
		return fmt.Errorf("direct API providers require api-key or none authentication")
	}
	switch p.Auth {
	case "subscription", "none":
		if p.KeyEnv != "" {
			return fmt.Errorf("provider %s: api_key_env requires api-key auth", id)
		}
	case "api-key":
		if !envIdentifier.MatchString(p.KeyEnv) {
			return fmt.Errorf("provider %s needs an API-key environment variable name", id)
		}
	default:
		return fmt.Errorf("provider %s has unknown auth mode %q", id, p.Auth)
	}
	for _, c := range p.Capabilities {
		switch c {
		case "read", "write", "tools", "browser", "resume", "stream", "delegate":
		default:
			return fmt.Errorf("provider %s has unknown capability %q", id, c)
		}
	}
	if !p.Has("read") {
		return fmt.Errorf("provider %s must support read tasks", id)
	}
	if p.Kind == "cli" && len(p.ReadArgs) == 0 {
		return fmt.Errorf("provider %s needs explicit read_only_args", id)
	}
	if p.Kind == "cli" && p.Has("write") && len(p.EditArgs) == 0 {
		return fmt.Errorf("provider %s needs edit_args for write tasks", id)
	}
	if p.Kind == "cli" && p.Has("resume") && len(p.ResumeArgs) == 0 {
		return fmt.Errorf("provider %s needs resume_args", id)
	}
	for _, args := range [][]string{p.Command, p.ModelArgs, p.ResumeArgs, p.ReadArgs, p.EditArgs, p.MCPArgs} {
		for _, a := range args {
			remaining := strings.NewReplacer("{repo}", "", "{model}", "", "{session}", "", "{mcp_config}", "").Replace(a)
			if strings.ContainsAny(remaining, "{}") {
				return fmt.Errorf("provider %s has unsupported argument placeholder", id)
			}
		}
	}
	if p.Kind == "cli" && p.Has("delegate") && len(p.MCPArgs) == 0 {
		return fmt.Errorf("provider %s needs mcp_args for delegation", id)
	}
	return nil
}
func mergeProvider(base, next Provider) Provider {
	if next.Kind != "" {
		base.Kind = next.Kind
	}
	if next.Command != nil {
		base.Command = next.Command
	}
	if next.Format != "" {
		base.Format = next.Format
	}
	if next.Auth != "" {
		base.Auth = next.Auth
	}
	if next.KeyEnv != "" {
		base.KeyEnv = next.KeyEnv
	}
	if next.ModelArgs != nil {
		base.ModelArgs = next.ModelArgs
	}
	if next.ResumeArgs != nil {
		base.ResumeArgs = next.ResumeArgs
	}
	if next.ReadArgs != nil {
		base.ReadArgs = next.ReadArgs
	}
	if next.EditArgs != nil {
		base.EditArgs = next.EditArgs
	}
	if next.Capabilities != nil {
		base.Capabilities = next.Capabilities
	}
	if next.MCPArgs != nil {
		base.MCPArgs = next.MCPArgs
	}
	if next.Endpoint != "" {
		base.Endpoint = next.Endpoint
	}
	if next.DefaultModel != "" {
		base.DefaultModel = next.DefaultModel
	}
	if next.RequestTimeout != "" {
		base.RequestTimeout = next.RequestTimeout
	}
	if next.MaxTokens != 0 {
		base.MaxTokens = next.MaxTokens
	}
	if next.MaxRounds != 0 {
		base.MaxRounds = next.MaxRounds
	}
	base.Disabled = next.Disabled
	return base
}
func (c Config) Provider(id string) (Provider, error) {
	providers := c.Providers
	if providers == nil {
		providers = BuiltinProviders()
	}
	p, ok := providers[id]
	if !ok {
		return p, fmt.Errorf("unknown provider: %s", id)
	}
	if p.Disabled {
		return p, fmt.Errorf("provider %s is disabled or its adapter is unverified", id)
	}
	return p, p.Validate(id)
}
func (c Config) Command(o AgentOptions) ([]string, error) {
	p, err := c.Provider(o.Agent)
	if err != nil {
		return nil, err
	}
	if o.Edits && !p.Has("write") {
		return nil, fmt.Errorf("provider %s does not support write tasks", o.Agent)
	}
	if o.Model != "" && p.Kind == "cli" && len(p.ModelArgs) == 0 {
		return nil, fmt.Errorf("provider %s does not declare model selection", o.Agent)
	}
	if p.Kind == "api" {
		return []string{"HTTP POST", p.Endpoint}, nil
	}
	if p.Kind == "builtin" {
		return AgentCommand(o)
	}
	replace := strings.NewReplacer("{repo}", o.Repo, "{model}", o.Model, "{session}", o.Native, "{mcp_config}", o.MCPPath)
	args := append([]string(nil), p.Command...)
	if o.Edits {
		args = append(args, p.EditArgs...)
	} else {
		args = append(args, p.ReadArgs...)
	}
	if o.Model != "" {
		args = append(args, p.ModelArgs...)
	}
	if o.Native != "" {
		if !p.Has("resume") {
			return nil, fmt.Errorf("provider %s does not support native resume", o.Agent)
		}
		args = append(args, p.ResumeArgs...)
	}
	if o.MCPPath != "" {
		args = append(args, p.MCPArgs...)
	}
	for i := range args {
		args[i] = replace.Replace(args[i])
	}
	return args, nil
}
func (c Config) CheckProvider(o AgentOptions) error {
	args, err := c.Command(o)
	if err != nil {
		return err
	}
	p, _ := c.Provider(o.Agent)
	if p.Kind != "api" {
		if _, err := exec.LookPath(args[0]); err != nil {
			return fmt.Errorf("provider %s command %s is not installed; run crew providers list", o.Agent, args[0])
		}
	}
	if p.Auth == "api-key" && os.Getenv(p.KeyEnv) == "" {
		return fmt.Errorf("provider %s needs environment variable %s", o.Agent, p.KeyEnv)
	}
	return nil
}
func (c Config) Redact(value string) string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if secret := os.Getenv(name); secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	for _, p := range c.Providers {
		if p.Auth == "api-key" {
			secret := os.Getenv(p.KeyEnv)
			if secret != "" {
				value = strings.ReplaceAll(value, secret, "[redacted]")
			}
		}
	}
	return value
}
func NormalizeProvider(p Provider, raw []byte) AgentEvent {
	if p.Format == "claude" || p.Format == "codex" {
		return Normalize(p.Format, raw)
	}
	if p.Format == "text" {
		return AgentEvent{Text: string(raw)}
	}
	var v struct {
		Type         string `json:"type"`
		Text         string `json:"text"`
		Session      string `json:"session"`
		InputTokens  int    `json:"input_tokens"`
		OutputTokens int    `json:"output_tokens"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return AgentEvent{Error: "Provider returned malformed Crew event JSON"}
	}
	switch v.Type {
	case "text":
		return AgentEvent{Text: v.Text}
	case "delta":
		return AgentEvent{Text: v.Text, Delta: true}
	case "status":
		return AgentEvent{Status: v.Text}
	case "session":
		return AgentEvent{Native: v.Session}
	case "error":
		return AgentEvent{Error: v.Text}
	case "usage":
		return AgentEvent{Status: fmt.Sprintf("usage · input %d · output %d tokens", v.InputTokens, v.OutputTokens)}
	case "done":
		return AgentEvent{}
	default:
		return AgentEvent{Error: "Provider returned unknown Crew event type"}
	}
}
func (c Config) ProviderNames() []string {
	providers := c.Providers
	if providers == nil {
		providers = BuiltinProviders()
	}
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
