// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Fingerprints actual file contents, so committing unchanged files does not
// invalidate test evidence. Declared test artifacts and Crew state are excluded.
func (s *Store) treeDigest(ctx context.Context, repo string, checks []Check) (string, error) {
	ignored := map[string]bool{}
	for _, check := range checks {
		for _, path := range check.Artifacts {
			ignored[filepath.Clean(path)] = true
		}
	}
	homeRel, _ := filepath.Rel(repo, s.home)
	skip := func(path string) bool {
		if path == ".crew" || strings.HasPrefix(path, ".crew"+string(filepath.Separator)) || path == ".git" || strings.HasPrefix(path, ".git"+string(filepath.Separator)) || ignored[path] {
			return true
		}
		return homeRel != "." && homeRel != ".." && !strings.HasPrefix(homeRel, ".."+string(filepath.Separator)) && (path == homeRel || strings.HasPrefix(path, homeRel+string(filepath.Separator)))
	}
	command := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	command.Dir = repo
	data, err := command.Output()
	files := []string{}
	if err == nil {
		for _, name := range strings.Split(string(data), "\x00") {
			if name != "" && !skip(name) {
				files = append(files, name)
			}
		}
	} else {
		err = filepath.WalkDir(repo, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(repo, path)
			if skip(rel) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "node_modules", ".venv", "vendor":
					return filepath.SkipDir
				}
				return nil
			}
			files = append(files, rel)
			return ctx.Err()
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(files)
	hash := sha256.New()
	previous := ""
	buffer := make([]byte, 32*1024)
	for _, name := range files {
		if name == previous {
			continue
		}
		previous = name
		if err := ctx.Err(); err != nil {
			return "", err
		}
		info, err := os.Lstat(filepath.Join(repo, name))
		if os.IsNotExist(err) {
			fmt.Fprintf(hash, "deleted:%s\n", name)
			continue
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%d:%s:%s:%d:", len(name), name, info.Mode().String(), info.Size())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filepath.Join(repo, name))
			if err != nil {
				return "", err
			}
			hash.Write([]byte(target))
			continue
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("unsupported repository file %s", name)
		}
		file, err := os.Open(filepath.Join(repo, name))
		if err != nil {
			return "", err
		}
		for {
			if err := ctx.Err(); err != nil {
				file.Close()
				return "", err
			}
			n, readErr := file.Read(buffer)
			hash.Write(buffer[:n])
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				file.Close()
				return "", readErr
			}
		}
		file.Close()
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
