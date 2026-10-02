// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var gitRevision = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

func ImportGitSkill(ctx context.Context, home, repo, source, revision, subdir string, global bool) (string, error) {
	if !gitRevision.MatchString(revision) {
		return "", fmt.Errorf("Git skill imports require a full 40-character commit hash")
	}
	if subdir == "" || subdir == "." || filepath.IsAbs(subdir) || strings.HasPrefix(filepath.Clean(subdir), "..") || strings.HasPrefix(filepath.Clean(subdir), ".git") {
		return "", fmt.Errorf("choose a repository-relative skill directory")
	}
	parsed, err := url.Parse(source)
	if err != nil {
		return "", err
	}
	if parsed.User != nil && parsed.Scheme != "ssh" {
		return "", fmt.Errorf("Git URLs must not contain credentials; reuse git authentication")
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return "", fmt.Errorf("Git URLs must not contain passwords")
		}
	}
	clone, err := os.MkdirTemp("", "crew-skill-import-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(clone)
	cmd := exec.CommandContext(ctx, "git", "clone", "--no-checkout", "--", source, clone)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("Git clone failed: %w", err)
	}
	cmd = exec.CommandContext(ctx, "git", "-C", clone, "checkout", "--detach", revision, "--")
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pinned Git checkout failed: %w", err)
	}
	skillSource := filepath.Join(clone, subdir)
	resolved, err := filepath.EvalSymlinks(skillSource)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(clone, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("skill directory escapes Git checkout")
	}
	if _, err := skillFiles(skillSource); err != nil {
		return "", err
	}
	name := filepath.Base(skillSource)
	if !validSkillName(name) {
		return "", fmt.Errorf("invalid skill directory name")
	}
	provenance, _ := json.MarshalIndent(map[string]string{"repository": source, "revision": strings.ToLower(revision), "path": subdir}, "", "  ")
	if err := os.WriteFile(filepath.Join(skillSource, "SOURCE.json"), provenance, 0600); err != nil {
		return "", err
	}
	target := filepath.Join(SkillRoot(home, repo, global), name)
	if err := CopySkill(skillSource, target); err != nil {
		return "", err
	}
	return target, nil
}
