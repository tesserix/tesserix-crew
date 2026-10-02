// SPDX-License-Identifier: Apache-2.0
package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var roleSkill = map[string]string{"designer": "crew-design", "planner": "crew-plan", "reviewer": "crew-review", "implementer": "crew-tdd", "tester": "crew-test", "deliverer": "crew-deliver"}
var builtinSkillBodies = map[string]string{
	"crew-design":  "Define the user's goal, constraints and measurable acceptance criteria. Inspect only relevant repository context. Compare feasible designs and explain the recommended choice. Identify questions that need user input. Output a concrete design; do not implement.",
	"crew-plan":    "Turn the reviewed design into bounded work items with dependencies, acceptance criteria and a test strategy. Identify unit/integration/E2E/browser checks and prerequisites. Explain any TDD exception. Do not broaden scope or implement.",
	"crew-review":  "Review independently for correctness, feasibility, compatibility and missing acceptance criteria. Cite concrete evidence and flag material unresolved risks. Return actionable findings and a clear pass/fail/blocked result. Do not approve user gates or implement.",
	"crew-tdd":     "Implement only approved scope. Add a meaningful test demonstrating the requested behavior is initially absent, run it and preserve the failing evidence before changing production code. Implement, rerun checks, and report commands/results. Explain unavoidable TDD exceptions. Do not publish or close issues.",
	"crew-test":    "Independently exercise acceptance criteria using appropriate unit, integration, E2E or browser tools. Preserve command outcomes, screenshots/traces when applicable, reproduction steps and limitations. Missing required tools or skipped checks are blocked, not passed. Do not modify production code; send failures to the implementer.",
	"crew-deliver": "Produce an evidence-backed delivery report mapping acceptance criteria to implementation and test results. Identify limitations and pending user actions. Do not claim skipped checks passed, publish changes, or close issues without the lifecycle's delivery controls.",
}

func BuiltinRoleSkill(role string) string { return roleSkill[role] }
func materializeSkills(home string) (map[string]string, error) {
	out := map[string]string{}
	for name, body := range builtinSkillBodies {
		path := filepath.Join(home, "builtin-skills", name)
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
		contents := "---\nname: " + name + "\ndescription: Shared lifecycle instructions\n---\n\n" + body + "\n"
		// Versioned instructions are owned by the binary; user edits belong in catalog overrides.
		current, _ := os.ReadFile(filepath.Join(path, "SKILL.md"))
		if string(current) != contents {
			if err := atomicWrite(filepath.Join(path, "SKILL.md"), []byte(contents)); err != nil {
				return nil, err
			}
		}
		out[name] = path
	}
	return out, nil
}
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".crew-write-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, path)
}
func SkillRoot(home, repo string, global bool) string {
	if global {
		return filepath.Join(home, "skills")
	}
	return filepath.Join(repo, ".crew", "skills")
}
func validSkillName(name string) bool { return identifier.MatchString(name) }
func skillState(home, repo string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, path := range []string{filepath.Join(home, "skill-state.json"), filepath.Join(repo, ".crew", "skill-state.json")} {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var next map[string]bool
		if err := json.Unmarshal(data, &next); err != nil {
			return nil, err
		}
		for name, disabled := range next {
			out[name] = disabled
		}
	}
	return out, nil
}
func SetSkillEnabled(home, repo, name string, global, enabled bool) error {
	if !validSkillName(name) {
		return fmt.Errorf("invalid skill name")
	}
	found, _, err := SkillCatalog(home, repo)
	if err != nil {
		return err
	}
	if _, ok := found[name]; !ok {
		return fmt.Errorf("unknown skill %s", name)
	}
	path := filepath.Join(repo, ".crew", "skill-state.json")
	if global {
		path = filepath.Join(home, "skill-state.json")
	}
	states := map[string]bool{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &states); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	states[name] = !enabled
	data, err = json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}
func SkillCatalog(home, repo string) (map[string]string, map[string]bool, error) {
	out, err := materializeSkills(home)
	if err != nil {
		return nil, nil, err
	}
	local, err := discoverSkillSources(home, repo)
	if err != nil {
		return nil, nil, err
	}
	for name, path := range local {
		out[name] = path
	}
	states, err := skillState(home, repo)
	return out, states, err
}
func SelectSkillSources(home, repo string, names []string) (map[string]string, error) {
	catalog, states, err := SkillCatalog(home, repo)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, name := range names {
		if states[name] {
			return nil, fmt.Errorf("selected skill %s is disabled", name)
		}
		path, ok := catalog[name]
		if !ok {
			return nil, fmt.Errorf("unknown selected skill %s", name)
		}
		out[name] = path
	}
	return out, nil
}
func (r *Runner) PinWorkflowSkills(s Session, recipe Lifecycle) error {
	sources, err := DiscoverSkills(r.Home, s.Repo)
	if err != nil {
		return err
	}
	var names []string
	for _, stage := range recipe.Stages {
		names = append(names, stage.Skills...)
	}
	selected, err := SelectSkillSources(r.Home, s.Repo, names)
	if err != nil {
		return err
	}
	for name, path := range selected {
		sources[name] = path
	}
	pinned, err := SnapshotSkills(sources, filepath.Join(r.Home, "snapshots"))
	if err != nil {
		return err
	}
	return r.Store.SetSkills(s.ID, pinned)
}
func FilterSkills(skills []Skill, names []string) ([]Skill, error) {
	if names == nil {
		return skills, nil
	}
	out := []Skill{}
	found := map[string]Skill{}
	for _, skill := range skills {
		found[skill.Name] = skill
	}
	for _, name := range names {
		skill, ok := found[name]
		if !ok {
			return nil, fmt.Errorf("selected skill %s is not pinned to this session", name)
		}
		out = append(out, skill)
	}
	return out, nil
}
func CheckSkillPrerequisites(skills []Skill, provider Provider) error {
	for _, skill := range skills {
		data, err := os.ReadFile(filepath.Join(skill.Path, "skill.toml"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var prerequisites struct {
			Commands     []string `toml:"commands"`
			Capabilities []string `toml:"capabilities"`
		}
		if err := toml.Unmarshal(data, &prerequisites); err != nil {
			return fmt.Errorf("skill %s prerequisites: %w", skill.Name, err)
		}
		for _, command := range prerequisites.Commands {
			if _, err := exec.LookPath(command); err != nil {
				return fmt.Errorf("skill %s needs command %s", skill.Name, command)
			}
		}
		for _, capability := range prerequisites.Capabilities {
			if !provider.Has(capability) {
				return fmt.Errorf("skill %s needs provider capability %s", skill.Name, capability)
			}
		}
	}
	return nil
}
func RemoveSkill(home, repo, name string, global bool) (string, error) {
	if !validSkillName(name) {
		return "", fmt.Errorf("invalid skill name")
	}
	source := filepath.Join(SkillRoot(home, repo, global), name)
	if _, err := skillFiles(source); err != nil {
		return "", fmt.Errorf("skill is not installed in this scope: %w", err)
	}
	trashRoot := filepath.Join(filepath.Dir(SkillRoot(home, repo, global)), "skill-trash")
	if err := os.MkdirAll(trashRoot, 0700); err != nil {
		return "", err
	}
	target, err := os.MkdirTemp(trashRoot, name+"-")
	if err != nil {
		return "", err
	}
	target = filepath.Join(target, name)
	return target, os.Rename(source, target)
}
func UpdateSkill(home, repo, name, source string, global bool) error {
	if !validSkillName(name) {
		return fmt.Errorf("invalid skill name")
	}
	root := SkillRoot(home, repo, global)
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(root, ".update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	next := filepath.Join(temp, name)
	if err := CopySkill(source, next); err != nil {
		return err
	}
	target := filepath.Join(root, name)
	if _, err := os.Lstat(target); err == nil {
		backup, err := RemoveSkill(home, repo, name, global)
		if err != nil {
			return err
		}
		if err := os.Rename(next, target); err != nil {
			_ = os.Rename(backup, target)
			return err
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(next, target)
}
func SortedSkillNames(catalog map[string]string) []string {
	names := make([]string, 0, len(catalog))
	for name := range catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func ConfigPath(home, repo string, global bool) string {
	if global {
		return filepath.Join(home, "config.toml")
	}
	return filepath.Join(repo, ".crew", "config.toml")
}
func ModifyConfig(home, repo string, global bool, mutate func(map[string]any) error) error {
	path := ConfigPath(home, repo, global)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	document := map[string]any{}
	if len(data) > 0 {
		if err := toml.Unmarshal(data, &document); err != nil {
			return err
		}
	}
	if err := mutate(document); err != nil {
		return err
	}
	data, err = toml.Marshal(document)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}
func providerDocument(document map[string]any) map[string]any {
	providers, ok := document["providers"].(map[string]any)
	if !ok {
		providers = map[string]any{}
		document["providers"] = providers
	}
	return providers
}
func SetProviderDisabled(home, repo, name string, global, disabled bool) error {
	config, err := LoadConfig(home, repo)
	if err != nil {
		return err
	}
	if _, ok := config.Providers[name]; !ok {
		return fmt.Errorf("unknown provider %s", name)
	}
	candidate := config.Providers[name]
	candidate.Disabled = disabled
	if err := candidate.Validate(name); err != nil {
		return err
	}
	if disabled && config.DefaultAgent == name {
		return fmt.Errorf("select a different default_agent before disabling %s", name)
	}
	return ModifyConfig(home, repo, global, func(doc map[string]any) error {
		providers := providerDocument(doc)
		settings, ok := providers[name].(map[string]any)
		if !ok {
			settings = map[string]any{}
		}
		settings["disabled"] = disabled
		providers[name] = settings
		return nil
	})
}
func AddProvider(home, repo, name string, p Provider, global bool) error {
	if err := p.Validate(name); err != nil {
		return err
	}
	if _, builtin := BuiltinProviders()[name]; builtin {
		return fmt.Errorf("choose a new provider ID instead of replacing a built-in adapter")
	}
	encoded, err := toml.Marshal(p)
	if err != nil {
		return err
	}
	var settings map[string]any
	if err := toml.Unmarshal(encoded, &settings); err != nil {
		return err
	}
	return ModifyConfig(home, repo, global, func(doc map[string]any) error {
		providers := providerDocument(doc)
		if _, exists := providers[name]; exists {
			return fmt.Errorf("provider already exists in this scope")
		}
		providers[name] = settings
		return nil
	})
}
func skillChanges(old, next []Skill) []string {
	before, after := map[string]string{}, map[string]string{}
	for _, s := range old {
		before[s.Name] = s.Digest
	}
	for _, s := range next {
		after[s.Name] = s.Digest
	}
	names := map[string]string{}
	for name, digest := range before {
		if after[name] != digest {
			names[name] = "changed"
		}
		if _, ok := after[name]; !ok {
			names[name] = "removed"
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			names[name] = "added"
		}
	}
	out := []string{}
	for _, name := range SortedSkillNames(names) {
		out = append(out, names[name]+" "+name)
	}
	return out
}
func (r *Runner) RefreshSkills(s Session, apply bool) ([]string, error) {
	old, _, err := r.Store.SkillManifest(s.ID)
	if err != nil {
		return nil, err
	}
	sources, err := DiscoverSkills(r.Home, s.Repo)
	if err != nil {
		return nil, err
	}
	next, err := SnapshotSkills(sources, filepath.Join(r.Home, "snapshots"))
	if err != nil {
		return nil, err
	}
	changes := skillChanges(old, next)
	if !apply {
		return changes, nil
	}
	if _, err := r.Store.Workflow(s.ID); err == nil {
		return changes, fmt.Errorf("lifecycle skills are pinned; create a new run to change its selections")
	}
	tx, err := r.Store.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	data, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec("INSERT OR REPLACE INTO skills VALUES(?,?)", s.ID, string(data)); err != nil {
		return nil, err
	}
	if _, err = tx.Exec("DELETE FROM context_cursors WHERE session=?", s.ID); err != nil {
		return nil, err
	}
	return changes, tx.Commit()
}

// SkillNames is useful for CLI diagnostics without exposing instruction content.
func SkillNames(skills []Skill) string {
	names := []string{}
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}
