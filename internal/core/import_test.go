// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitImportRejectsEscapingParentBeforeProvenance(t *testing.T) {
	source, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "skill"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "escape")); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = source
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init")
	git("add", "escape")
	git("commit", "-m", "Fixture")
	revision := git("rev-parse", "HEAD")
	_, err := ImportGitSkill(context.Background(), t.TempDir(), source, source, revision, "escape/skill", true)
	if err == nil {
		t.Fatal("escaping import accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "skill", "SOURCE.json")); !os.IsNotExist(err) {
		t.Fatalf("provenance escaped checkout: %v", err)
	}
}
