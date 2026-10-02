// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Check struct {
	Name      string   `toml:"name" json:"name"`
	Kind      string   `toml:"kind" json:"kind"` // unit, integration, e2e, browser
	Command   []string `toml:"command" json:"command"`
	Timeout   string   `toml:"timeout" json:"timeout,omitempty"`
	Roles     []string `toml:"roles" json:"roles,omitempty"`
	Optional  bool     `toml:"optional" json:"optional,omitempty"`
	Artifacts []string `toml:"artifacts" json:"artifacts,omitempty"`
}
type Artifact struct {
	Path     string `json:"path"`
	Snapshot string `json:"snapshot"`
	Digest   string `json:"digest"`
}
type Evidence struct {
	ID         int64      `json:"id"`
	Workflow   string     `json:"workflow"`
	Stage      string     `json:"stage"`
	Revision   int        `json:"revision"`
	Check      string     `json:"check"`
	Kind       string     `json:"kind"`
	Phase      string     `json:"phase"`
	Command    []string   `json:"command"`
	ExitCode   int        `json:"exit_code"`
	Passed     bool       `json:"passed"`
	Started    string     `json:"started"`
	Finished   string     `json:"finished"`
	Output     string     `json:"output"`
	TreeDigest string     `json:"tree_digest,omitempty"`
	Artifacts  []Artifact `json:"artifacts,omitempty"`
}

func (c Check) Validate() error {
	if !identifier.MatchString(c.Name) || len(c.Command) == 0 || strings.TrimSpace(c.Command[0]) == "" {
		return fmt.Errorf("checks need a valid name and command array")
	}
	switch c.Kind {
	case "unit", "integration", "e2e", "browser":
	default:
		return fmt.Errorf("check %s has unknown kind %q", c.Name, c.Kind)
	}
	for _, role := range c.Roles {
		if role != "implementer" && role != "tester" {
			return fmt.Errorf("check %s has unsupported role %s", c.Name, role)
		}
	}
	if c.Timeout != "" {
		d, err := time.ParseDuration(c.Timeout)
		if err != nil || d <= 0 {
			return fmt.Errorf("check %s needs a positive timeout", c.Name)
		}
	}
	if c.Kind == "browser" && len(c.Artifacts) == 0 {
		return fmt.Errorf("browser check %s needs screenshot or trace artifact paths", c.Name)
	}
	for _, path := range c.Artifacts {
		if filepath.IsAbs(path) || path == "" || strings.HasPrefix(filepath.Clean(path), "..") {
			return fmt.Errorf("check %s artifacts must be repository-relative", c.Name)
		}
	}
	return nil
}
func InferChecks(repo string) []Check {
	if _, err := os.Stat(filepath.Join(repo, "go.mod")); err == nil {
		return []Check{{Name: "unit", Kind: "unit", Command: []string{"go", "test", "./..."}, Timeout: "5m"}}
	}
	data, err := os.ReadFile(filepath.Join(repo, "package.json"))
	if err == nil {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(data, &pkg) == nil && pkg.Scripts["test"] != "" {
			return []Check{{Name: "unit", Kind: "unit", Command: []string{"npm", "test"}, Timeout: "5m"}}
		}
	}
	return nil
}
func (s *Store) Evidence(id string) ([]Evidence, error) {
	rows, err := s.db.Query("SELECT id,data FROM check_runs WHERE workflow=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Evidence{}
	for rows.Next() {
		var seq int64
		var data []byte
		if err := rows.Scan(&seq, &data); err != nil {
			return nil, err
		}
		var e Evidence
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		e.ID = seq
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) RunCheck(ctx context.Context, id, name, phase string) (Evidence, error) {
	w, err := s.Workflow(id)
	if err != nil {
		return Evidence{}, err
	}
	if w.State != "running" || w.Current >= len(w.Recipe.Stages) {
		return Evidence{}, fmt.Errorf("checks run only during an active implementation/testing stage")
	}
	stage := w.Recipe.Stages[w.Current]
	if !stage.Edits || (stage.Role != "implementer" && stage.Role != "tester") {
		return Evidence{}, fmt.Errorf("this stage is not authorized to run checks")
	}
	if phase != "red" && phase != "green" {
		return Evidence{}, fmt.Errorf("check phase must be red or green")
	}
	if stage.Role == "tester" && phase == "red" {
		return Evidence{}, fmt.Errorf("independent testing records green verification; reproduction failures remain failed evidence")
	}
	var check Check
	found := false
	for _, c := range w.Recipe.Checks {
		if c.Name == name && c.Applies(stage.Role) {
			check = c
			found = true
			break
		}
	}
	if !found {
		return Evidence{}, fmt.Errorf("unknown pinned check %s", name)
	}
	ctx, release, err := s.claimLease(ctx, "check:"+id)
	if err != nil {
		return Evidence{}, err
	}
	defer release()
	timeout := 2 * time.Minute
	if check.Timeout != "" {
		timeout, _ = time.ParseDuration(check.Timeout)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	e := Evidence{Workflow: id, Stage: stage.Name, Revision: w.Revision, Check: name, Kind: check.Kind, Phase: phase, Command: check.Command, ExitCode: -1, Started: stamp()}
	cmd := exec.CommandContext(ctx, check.Command[0], check.Command[1:]...)
	cmd.Dir = w.Repo
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 3 * time.Second
	var output tailWriter
	cmd.Stdout = &output
	cmd.Stderr = &output
	runErr := cmd.Run()
	if cmd.ProcessState != nil {
		e.ExitCode = cmd.ProcessState.ExitCode()
	}
	config := Config{Providers: w.Providers}
	e.Output = config.Redact(output.String())
	e.Finished = stamp()
	if ctx.Err() == nil {
		e.Passed = (phase == "green" && runErr == nil) || (phase == "red" && e.ExitCode > 0)
	}
	if e.ExitCode >= 0 && ctx.Err() == nil {
		for _, path := range check.Artifacts {
			source, err := filepath.EvalSymlinks(filepath.Join(w.Repo, path))
			repo, repoErr := filepath.EvalSymlinks(w.Repo)
			rel, relErr := filepath.Rel(repo, source)
			if err != nil || repoErr != nil || relErr != nil || strings.HasPrefix(rel, "..") {
				e.Passed = false
				e.Output += "\nMissing or invalid artifact: " + path
				break
			}
			info, err := os.Stat(source)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 20*1024*1024 {
				e.Passed = false
				e.Output += "\nArtifact must be a regular file of at most 20 MB: " + path
				break
			}
			data, err := os.ReadFile(source)
			if err != nil {
				e.Passed = false
				e.Output += "\nCannot read artifact: " + path
				break
			}
			hash := sha256.Sum256(data)
			digest := hex.EncodeToString(hash[:])
			snapshot := filepath.Join(s.home, "evidence", w.ID, digest)
			if err := atomicWrite(snapshot, data); err != nil {
				return e, err
			}
			e.Artifacts = append(e.Artifacts, Artifact{Path: path, Snapshot: snapshot, Digest: digest})
		}
	}
	if e.Passed {
		digest, err := s.treeDigest(ctx, w.Repo, w.Recipe.Checks)
		if err != nil {
			e.Passed = false
			e.Output += "\nCannot fingerprint tested files: " + err.Error()
		} else {
			e.TreeDigest = digest
		}
	}
	if runErr != nil && e.Output == "" {
		e.Output = config.Redact(runErr.Error())
	}
	if ctx.Err() != nil {
		e.Output += "\nCheck cancelled or timed out"
	}
	data, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	result, err := s.db.Exec("INSERT INTO check_runs(workflow,data) VALUES(?,?)", id, data)
	if err != nil {
		return e, err
	}
	e.ID, err = result.LastInsertId()
	if err != nil {
		return e, err
	}
	if !e.Passed {
		return e, fmt.Errorf("check %s did not satisfy %s evidence (exit %d)", name, phase, e.ExitCode)
	}
	return e, nil
}
func (s *Store) VerifyStageEvidence(w Workflow, stage Stage, revision int) error {
	if stage.Role != "implementer" && stage.Role != "tester" {
		return nil
	}
	if len(w.Recipe.Checks) == 0 {
		return fmt.Errorf("no executable checks are pinned; configure checks or an explicit no_tdd_reason")
	}
	evidence, err := s.Evidence(w.ID)
	if err != nil {
		return err
	}
	tddSatisfied := false
	for _, check := range w.Recipe.Checks {
		if check.Optional || !check.Applies(stage.Role) {
			continue
		}
		var red, green Evidence
		for _, e := range evidence {
			if e.Revision == revision && e.Stage == stage.Name && e.Check == check.Name {
				if e.Phase == "red" {
					red = e
				} else if e.Phase == "green" {
					green = e
				}
			}
		}
		if green.ID == 0 || !green.Passed {
			return fmt.Errorf("stage %s needs current passing executable evidence for %s", stage.Name, check.Name)
		}
		for _, artifact := range green.Artifacts {
			data, err := os.ReadFile(artifact.Snapshot)
			sum := sha256.Sum256(data)
			if err != nil || hex.EncodeToString(sum[:]) != artifact.Digest {
				return fmt.Errorf("stored artifact integrity failed for %s", artifact.Path)
			}
		}
		if red.ID > 0 && red.Passed && red.ID < green.ID {
			tddSatisfied = true
		}
	}
	if stage.Role == "implementer" && w.Recipe.TDD && w.Recipe.NoTDDReason == "" && !tddSatisfied {
		return fmt.Errorf("stage %s needs failing evidence before passing evidence for at least one required check", stage.Name)
	}
	return nil
}
func (s *Store) RepairWorkflow(id, note string) (Workflow, error) {
	w, err := s.Workflow(id)
	if err != nil {
		return w, err
	}
	if (w.State != "failed" && w.State != "blocked") || w.Recipe.Stages[w.Current].Role != "tester" {
		return w, fmt.Errorf("repair requires a failed or blocked independent testing stage")
	}
	repairs := 0
	for _, result := range w.Results {
		if result.State == "repair-requested" {
			repairs++
		}
	}
	if repairs >= w.Recipe.MaxRepairs {
		return w, fmt.Errorf("repair limit reached (%d); review the remaining problem", w.Recipe.MaxRepairs)
	}
	target := -1
	for i := w.Current - 1; i >= 0; i-- {
		if w.Recipe.Stages[i].Role == "implementer" {
			target = i
			break
		}
	}
	if target < 0 {
		return w, fmt.Errorf("no preceding implementer stage")
	}
	if strings.TrimSpace(note) == "" {
		return w, fmt.Errorf("repair needs reproduction or evidence feedback")
	}
	w.record("repair-requested", note, "user")
	w.Current = target
	w.settle()
	err = s.SaveWorkflow(&w)
	return w, err
}

func ParseCheckProposal(output string) ([]Check, error) {
	_, block, ok := strings.Cut(output, "```crew-checks\n")
	if !ok {
		return nil, fmt.Errorf("TDD plan needs a crew-checks JSON block when no checks are configured")
	}
	block, _, ok = strings.Cut(block, "```")
	if !ok {
		return nil, fmt.Errorf("unterminated crew-checks block")
	}
	var checks []Check
	decoder := json.NewDecoder(strings.NewReader(block))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&checks); err != nil {
		return nil, err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("check plan contains trailing data")
	}
	if len(checks) == 0 {
		return nil, fmt.Errorf("plan needs at least one executable check")
	}
	for _, check := range checks {
		if err := check.Validate(); err != nil {
			return nil, err
		}
	}
	return checks, nil
}

func (c Check) Applies(role string) bool {
	if len(c.Roles) == 0 {
		return true
	}
	for _, selected := range c.Roles {
		if selected == role {
			return true
		}
	}
	return false
}
