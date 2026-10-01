// SPDX-License-Identifier: Apache-2.0
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type Skill struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Path   string `json:"path"`
}

func DiscoverSkills(home, repo string) (map[string]string, error) {
	out := map[string]string{}
	for _, root := range []string{filepath.Join(home, "skills"), filepath.Join(repo, ".crew", "skills")} {
		entries, e := os.ReadDir(root)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		for _, entry := range entries {
			if entry.IsDir() {
				path := filepath.Join(root, entry.Name())
				if _, e := os.Stat(filepath.Join(path, "SKILL.md")); e == nil {
					out[entry.Name()] = path
				}
			}
		}
	}
	return out, nil
}
func skillFiles(source string) ([]string, error) {
	info, e := os.Lstat(source)
	if e != nil {
		return nil, e
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skill must be a directory")
	}
	var paths []string
	e = filepath.WalkDir(source, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill symlinks are unsupported: %s", path)
		}
		if !d.IsDir() {
			if !d.Type().IsRegular() {
				return fmt.Errorf("unsupported skill file: %s", path)
			}
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, e
}
func CopySkill(source, target string) error {
	paths, e := skillFiles(source)
	if e != nil {
		return e
	}
	if _, e = os.Stat(filepath.Join(source, "SKILL.md")); e != nil {
		return fmt.Errorf("skill needs SKILL.md")
	}
	if _, e = os.Lstat(target); e == nil {
		return fmt.Errorf("skill already exists: %s", target)
	} else if !os.IsNotExist(e) {
		return e
	}
	if e = os.MkdirAll(target, 0700); e != nil {
		return e
	}
	for _, path := range paths {
		rel, _ := filepath.Rel(source, path)
		dest := filepath.Join(target, rel)
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		info, e := os.Stat(path)
		if e != nil {
			return e
		}
		if e = os.WriteFile(dest, b, info.Mode().Perm()&0700); e != nil {
			return e
		}
	}
	return nil
}
func SnapshotSkills(sources map[string]string, root string) ([]Skill, error) {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Skill, 0, len(names))
	for _, name := range names {
		source := sources[name]
		paths, e := skillFiles(source)
		if e != nil {
			return nil, e
		}
		hash := sha256.New()
		for _, path := range paths {
			rel, _ := filepath.Rel(source, path)
			b, e := os.ReadFile(path)
			if e != nil {
				return nil, e
			}
			fmt.Fprintf(hash, "%d:%s:%d:", len(rel), rel, len(b))
			hash.Write(b)
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		target := filepath.Join(root, name+"-"+digest)
		if _, e = os.Stat(target); os.IsNotExist(e) {
			if e = CopySkill(source, target); e != nil {
				return nil, e
			}
		} else if e != nil {
			return nil, e
		}
		out = append(out, Skill{name, digest, target})
	}
	return out, nil
}
