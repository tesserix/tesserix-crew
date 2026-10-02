// SPDX-License-Identifier: Apache-2.0
package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeSkill(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "custom")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestSkillUpdatesAndRemovalPreserveSnapshots(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	source := makeSkill(t, "old instructions")
	if err := CopySkill(source, filepath.Join(SkillRoot(home, repo, false), "custom")); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, _ := store.Create(repo, "claude", "")
	r := Runner{Store: store, Home: home}
	pinned, err := r.Skills(s)
	if err != nil {
		t.Fatal(err)
	}
	replacement := makeSkill(t, "new instructions")
	if err := UpdateSkill(home, repo, "custom", replacement, false); err != nil {
		t.Fatal(err)
	}
	changes, err := r.RefreshSkills(s, false)
	if err != nil || len(changes) != 1 || changes[0] != "changed custom" {
		t.Fatal(changes, err)
	}
	body, _ := os.ReadFile(filepath.Join(pinned[0].Path, "SKILL.md"))
	if string(body) != "old instructions" {
		t.Fatal("snapshot changed")
	}
	if _, err := r.RefreshSkills(s, true); err != nil {
		t.Fatal(err)
	}
	updated, err := r.Skills(s)
	if err != nil || updated[0].Digest == pinned[0].Digest {
		t.Fatal(updated, err)
	}
	if _, err := RemoveSkill(home, repo, "custom", false); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(filepath.Join(updated[0].Path, "SKILL.md"))
	if string(body) != "new instructions" {
		t.Fatal("snapshot lost on removal")
	}
}
func TestPerRolePinnedSkillsAndPrerequisites(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, _ := store.Create(repo, "claude", "")
	r := Runner{Store: store, Home: home}
	recipe := testRecipe()
	recipe.Stages[0].Skills = []string{"crew-plan"}
	recipe.Stages[2].Skills = []string{"crew-tdd"}
	if err := r.PinWorkflowSkills(s, recipe); err != nil {
		t.Fatal(err)
	}
	skills, err := r.Skills(s)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := FilterSkills(skills, []string{"crew-plan"})
	if err != nil || len(selected) != 1 || selected[0].Name != "crew-plan" {
		t.Fatal(selected, err)
	}
	if _, err := FilterSkills(skills, []string{"missing"}); err == nil {
		t.Fatal("missing selection ignored")
	}
	if err := SetSkillEnabled(home, repo, "crew-plan", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectSkillSources(home, repo, []string{"crew-plan"}); err == nil {
		t.Fatal("disabled selection used")
	}
	if _, err := os.ReadFile(filepath.Join(selected[0].Path, "SKILL.md")); err != nil {
		t.Fatal("disabled pinned skill lost")
	}
	prereq := makeSkill(t, "instructions")
	os.WriteFile(filepath.Join(prereq, "skill.toml"), []byte("commands=['crew-intentionally-missing-tool']\n"), 0600)
	if err := CheckSkillPrerequisites([]Skill{{Name: "custom", Path: prereq}}, BuiltinProviders()["claude"]); err == nil || !strings.Contains(err.Error(), "missing-tool") {
		t.Fatal(err)
	}
}
func TestLifecycleCapabilityDependencyAndDeadlineValidation(t *testing.T) {
	recipe := testRecipe()
	recipe.Stages[0].DependsOn = []string{"test"}
	if recipe.Validate() == nil {
		t.Fatal("forward dependency accepted")
	}
	recipe = testRecipe()
	recipe.Stages[0].Timeout = "-1s"
	if recipe.Validate() == nil {
		t.Fatal("negative timeout accepted")
	}
	recipe = testRecipe()
	recipe.Stages[0].Requires = []string{"browser"}
	if recipe.Validate() == nil {
		t.Fatal("undeclared capability accepted")
	}
	recipe = testRecipe()
	recipe.Stages[3].Agent = "codex"
	recipe.AllowSelfTest = true
	if err := recipe.Validate(); err != nil {
		t.Fatal("explicit independence override rejected", err)
	}
}
