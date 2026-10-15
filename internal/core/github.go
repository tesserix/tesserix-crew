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
	"regexp"
	"strconv"
	"strings"
	"time"
)

type IssueProposal struct {
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Acceptance []string `json:"acceptance"`
	Key        string   `json:"key,omitempty"`
	DependsOn  []string `json:"depends_on,omitempty"`
}
type IssueLink struct {
	Repository      string        `json:"repository"`
	Number          int           `json:"number"`
	URL             string        `json:"url"`
	Marker          string        `json:"marker"`
	Proposal        IssueProposal `json:"proposal"`
	Closed          bool          `json:"closed"`
	CreationPending bool          `json:"creation_pending,omitempty"`
	ClosurePending  bool          `json:"closure_pending,omitempty"`
}
type GitHubIssue struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	Body   string `json:"body"`
	State  string `json:"state"`
}
type GitHub interface {
	Find(context.Context, string, string) (GitHubIssue, bool, error)
	Create(context.Context, string, string, string) (GitHubIssue, error)
	Close(context.Context, string, int, string, string) error
}

// GitHubCLI keeps native gh authentication and passes message bodies through private files.
type GitHubCLI struct{}

var githubSlug = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func InferGitHubRepo(repo string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repo
	data, err := cmd.Output()
	if err != nil {
		return ""
	}
	remote := strings.TrimSpace(string(data))
	for _, prefix := range []string{"git@github.com:", "https://github.com/", "ssh://git@github.com/"} {
		if strings.HasPrefix(remote, prefix) {
			slug := strings.TrimSuffix(strings.TrimPrefix(remote, prefix), ".git")
			if githubSlug.MatchString(slug) {
				return slug
			}
		}
	}
	return ""
}
func ghOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("GitHub CLI request failed: %v; inspect gh auth status and repository access", err)
	}
	return output, nil
}
func (GitHubCLI) Find(ctx context.Context, repo, marker string) (GitHubIssue, bool, error) {
	if !githubSlug.MatchString(repo) {
		return GitHubIssue{}, false, fmt.Errorf("invalid GitHub repository")
	}
	output, err := ghOutput(ctx, "api", "--paginate", "repos/"+repo+"/issues?state=all&per_page=100")
	if err != nil {
		return GitHubIssue{}, false, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var match GitHubIssue
	count := 0
	for {
		var page []GitHubIssue
		err := decoder.Decode(&page)
		if err == io.EOF {
			break
		}
		if err != nil {
			return match, false, err
		}
		for _, issue := range page {
			if strings.Contains(issue.Body, marker) {
				match = issue
				count++
			}
		}
	}
	if count > 1 {
		return match, false, fmt.Errorf("multiple issues contain the workflow marker; resolve duplicates before continuing")
	}
	return match, count == 1, nil
}
func privateBody(body string) (string, func(), error) {
	file, err := os.CreateTemp("", "crew-gh-*.md")
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	cleanup := func() { os.Remove(path) }
	if _, err := file.WriteString(body); err != nil {
		file.Close()
		cleanup()
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}
func (GitHubCLI) Create(ctx context.Context, repo, title, body string) (GitHubIssue, error) {
	path, cleanup, err := privateBody(body)
	if err != nil {
		return GitHubIssue{}, err
	}
	defer cleanup()
	output, err := ghOutput(ctx, "issue", "create", "--repo", repo, "--title", title, "--body-file", path)
	if err != nil {
		return GitHubIssue{}, err
	}
	url := strings.TrimSpace(string(output))
	var number int
	prefix := "https://github.com/" + repo + "/issues/"
	if !strings.HasPrefix(url, prefix) {
		return GitHubIssue{}, fmt.Errorf("GitHub returned an unexpected issue URL")
	}
	if _, err := fmt.Sscanf(strings.TrimPrefix(url, prefix), "%d", &number); err != nil || number <= 0 {
		return GitHubIssue{}, fmt.Errorf("GitHub returned an invalid issue number")
	}
	if url != prefix+strconv.Itoa(number) {
		return GitHubIssue{}, fmt.Errorf("unexpected issue URL")
	}
	return GitHubIssue{Number: number, URL: url, Body: body, State: "open"}, nil
}
func (GitHubCLI) Close(ctx context.Context, repo string, number int, marker, body string) error {
	output, err := ghOutput(ctx, "api", fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", repo, number), "--paginate")
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	exists := false
	for {
		var page []struct {
			Body string `json:"body"`
		}
		err := decoder.Decode(&page)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		for _, comment := range page {
			if strings.Contains(comment.Body, marker) {
				exists = true
			}
		}
	}
	if !exists {
		path, cleanup, err := privateBody(body + "\n\n" + marker)
		if err != nil {
			return err
		}
		defer cleanup()
		if _, err := ghOutput(ctx, "issue", "comment", fmt.Sprint(number), "--repo", repo, "--body-file", path); err != nil {
			return err
		}
	}
	_, err = ghOutput(ctx, "issue", "close", fmt.Sprint(number), "--repo", repo, "--reason", "completed")
	return err
}
func ParseIssueProposals(w Workflow) ([]IssueProposal, error) {
	output := ""
	for _, result := range w.Results {
		for _, stage := range w.Recipe.Stages {
			if result.Stage == stage.Name && stage.Role == "planner" && result.State == "pass" {
				output = result.Output
			}
		}
	}
	_, block, ok := strings.Cut(output, "```crew-issues\n")
	if !ok {
		return nil, fmt.Errorf("the reviewed plan needs a crew-issues JSON block; revise the plan before creating issues")
	}
	block, _, ok = strings.Cut(block, "```")
	if !ok {
		return nil, fmt.Errorf("unterminated crew-issues block")
	}
	var proposals []IssueProposal
	decoder := json.NewDecoder(strings.NewReader(block))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposals); err != nil {
		return nil, fmt.Errorf("invalid issue plan: %w", err)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("issue plan contains trailing data")
	}
	if len(proposals) == 0 || len(proposals) > 20 {
		return nil, fmt.Errorf("issue plan must contain 1–20 scoped issues")
	}
	seenKeys := map[string]bool{}
	for _, p := range proposals {
		if strings.TrimSpace(p.Title) == "" || len(p.Title) > 256 || strings.TrimSpace(p.Body) == "" || len(p.Acceptance) == 0 {
			return nil, fmt.Errorf("each issue needs a title, body and acceptance criteria")
		}
		for _, criterion := range p.Acceptance {
			if strings.TrimSpace(criterion) == "" {
				return nil, fmt.Errorf("empty issue acceptance criterion")
			}
		}
		if p.Key != "" && (!identifier.MatchString(p.Key) || len(p.Key) > 64 || seenKeys[p.Key]) {
			return nil, fmt.Errorf("issue keys must be valid, unique identifiers")
		}
		dependencies := map[string]bool{}
		for _, key := range p.DependsOn {
			if !seenKeys[key] || dependencies[key] {
				return nil, fmt.Errorf("issue dependencies must reference distinct earlier issue keys: %s", key)
			}
			dependencies[key] = true
		}
		if p.Key != "" {
			seenKeys[p.Key] = true
		}
	}
	return proposals, nil
}
func issueMarker(id string, p IssueProposal) string {
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(data)
	return "<!-- crew:" + id + ":" + hex.EncodeToString(sum[:]) + " -->"
}
func (s *Store) RunGitHubStage(ctx context.Context, id string, client GitHub) (Workflow, error) {
	w, err := s.Workflow(id)
	if err != nil {
		return w, err
	}
	if w.State != "ready" {
		return w, fmt.Errorf("workflow is %s", w.State)
	}
	stage := w.Recipe.Stages[w.Current]
	if stage.Kind != "github-create" && stage.Kind != "github-close" {
		return w, fmt.Errorf("current stage is not a GitHub action")
	}
	if !githubSlug.MatchString(w.Recipe.GitHubRepo) {
		return w, fmt.Errorf("lifecycle needs a pinned github_repo (owner/repo)")
	}
	if stage.Timeout != "" {
		d, _ := time.ParseDuration(stage.Timeout)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	ctx, release, err := s.claimWorkflow(ctx, id)
	if err != nil {
		return w, err
	}
	defer release()
	if stage.Kind == "github-close" {
		var releaseRepo func()
		ctx, releaseRepo, err = s.claimLease(ctx, w.Repo)
		if err != nil {
			return w, err
		}
		defer releaseRepo()
		if err := s.verifyDelivery(ctx, w); err != nil {
			return w, err
		}
	}
	var proposals []IssueProposal
	legacyUncertain := false
	if w.GitHubIntentVersion == 0 {
		for _, result := range w.Results {
			if result.Stage == stage.Name && result.State == "fail" {
				legacyUncertain = true
			}
		}
		if !legacyUncertain {
			w.GitHubIntentVersion = 1
		}
	}
	if stage.Kind == "github-create" {
		proposals, err = ParseIssueProposals(w)
		if err != nil {
			return w, err
		}
		expected := map[string]bool{}
		for _, proposal := range proposals {
			expected[issueMarker(w.ID, proposal)] = true
		}
		for _, link := range w.Issues {
			if !expected[link.Marker] || link.Repository != w.Recipe.GitHubRepo {
				return w, fmt.Errorf("published issue scope changed; reconcile tracked issues before further GitHub writes")
			}
		}
	}
	w.State = "running"
	if err := s.SaveWorkflow(&w); err != nil {
		return w, err
	}
	actionErr := error(nil)
	if stage.Kind == "github-create" {
		resolved := map[string]string{}
		for _, proposal := range proposals {
			marker := issueMarker(w.ID, proposal)
			known := -1
			for i := range w.Issues {
				if w.Issues[i].Marker == marker {
					known = i
				}
			}
			issue, found, e := client.Find(ctx, w.Recipe.GitHubRepo, marker)
			if e != nil {
				actionErr = e
				break
			}
			if !found {
				if known >= 0 || legacyUncertain {
					actionErr = fmt.Errorf("creation outcome for %q is unconfirmed; retry remote reconciliation later or inspect GitHub manually; refusing duplicate creation", proposal.Title)
					break
				}
				body := proposal.Body + "\n\nAcceptance criteria:\n"
				for _, criterion := range proposal.Acceptance {
					body += "- [ ] " + criterion + "\n"
				}
				body += fmt.Sprintf("\nCrew workflow: `%s`\nCrew session: `%s`\nResume locally: `crew resume %s`\n", w.ID, w.Session, w.Session)
				if len(proposal.DependsOn) > 0 {
					body += "\nDependencies:\n"
					for _, key := range proposal.DependsOn {
						body += "- " + resolved[key] + "\n"
					}
				}
				body += "\n" + marker
				// Persist intent before the external write. A lost response and a
				// temporarily stale issue listing must never cause another create.
				w.Issues = append(w.Issues, IssueLink{Repository: w.Recipe.GitHubRepo, Marker: marker, Proposal: proposal, CreationPending: true})
				known = len(w.Issues) - 1
				if err := s.SaveWorkflow(&w); err != nil {
					return w, err
				}
				issue, e = client.Create(ctx, w.Recipe.GitHubRepo, proposal.Title, body)
				if e != nil {
					actionErr = e
					break
				}
			}
			if issue.Number <= 0 || issue.URL == "" {
				actionErr = fmt.Errorf("GitHub returned incomplete issue identity")
				break
			}
			if proposal.Key != "" {
				resolved[proposal.Key] = issue.URL
			}
			link := IssueLink{Repository: w.Recipe.GitHubRepo, Number: issue.Number, URL: issue.URL, Marker: marker, Proposal: proposal, Closed: issue.State == "closed"}
			if known >= 0 {
				if w.Issues[known].Number > 0 && w.Issues[known].Number != issue.Number {
					actionErr = fmt.Errorf("tracked issue identity changed; refusing replacement")
					break
				}
				w.Issues[known] = link
			} else {
				w.Issues = append(w.Issues, link)
			}
			if err := s.SaveWorkflow(&w); err != nil {
				return w, err
			}
		}
	} else {
		for i := range w.Issues {
			link := &w.Issues[i]
			// Verify both marker and identity immediately before closing anything.
			issue, found, e := client.Find(ctx, link.Repository, link.Marker)
			if e != nil {
				actionErr = e
				break
			}
			if !found || issue.Number != link.Number || link.Repository != w.Recipe.GitHubRepo {
				actionErr = fmt.Errorf("tracked issue identity changed; refusing closure")
				break
			}
			if err := s.verifyDelivery(ctx, w); err != nil {
				actionErr = err
				break
			}
			if issue.State != "closed" {
				if link.ClosurePending || legacyUncertain {
					actionErr = fmt.Errorf("closure outcome for %s is unconfirmed; inspect the remote issue and delivery comment before retrying; refusing duplicate writes", link.URL)
					break
				}
				body := deliveryEvidence(w)
				evidence, _ := s.Evidence(w.ID)
				for _, item := range evidence {
					if item.Phase == "green" && item.Passed {
						body += fmt.Sprintf("\nCheck %s / %s: exit %d, revision %d", item.Stage, item.Check, item.ExitCode, item.Revision)
						for _, artifact := range item.Artifacts {
							body += "\nArtifact " + artifact.Path + " · SHA256 " + artifact.Digest
						}
					}
				}
				link.ClosurePending = true
				if err := s.SaveWorkflow(&w); err != nil {
					return w, err
				}
				if e := client.Close(ctx, link.Repository, link.Number, "<!-- crew-delivery:"+w.ID+" -->", body); e != nil {
					actionErr = e
					break
				}
			}
			link.Closed = true
			link.ClosurePending = false
			if err := s.SaveWorkflow(&w); err != nil {
				return w, err
			}
		}
	}
	if actionErr != nil {
		w.record("fail", actionErr.Error(), "github")
		w.State = "failed"
	} else {
		w.record("pass", fmt.Sprintf("GitHub action complete; %d scoped issues tracked", len(w.Issues)), "github")
		w.Current++
		w.settle()
	}
	if err := s.SaveWorkflow(&w); err != nil {
		return w, err
	}
	return w, actionErr
}
func (s *Store) verifyDelivery(ctx context.Context, w Workflow) error {
	if len(w.Issues) == 0 {
		return fmt.Errorf("no scoped issues are tracked")
	}
	for _, link := range w.Issues {
		if link.CreationPending || link.Number <= 0 || link.URL == "" {
			return fmt.Errorf("issue creation must be confirmed before delivery or closure")
		}
	}
	latest := map[string]StageResult{}
	for _, result := range w.Results {
		latest[result.Stage] = result
	}
	approval, implementation, testing, delivery := false, false, false, false
	for _, stage := range w.Recipe.Stages[:w.Current] {
		if stage.Kind == "approval" {
			approval = true
		}
		switch stage.Role {
		case "implementer":
			implementation = true
		case "tester":
			testing = true
		case "deliverer":
			delivery = true
		}
		result, ok := latest[stage.Name]
		if !ok || (result.State != "pass" && result.State != "approved" && result.State != "recorded") {
			return fmt.Errorf("stage %s has not passed its completion gate", stage.Name)
		}
		if err := s.VerifyStageEvidence(w, stage, result.Revision); err != nil {
			return err
		}
	}
	if !approval || !implementation || !testing || !delivery || len(w.Recipe.Checks) == 0 {
		return fmt.Errorf("closure requires user approval, implementation, independent testing, delivery and executable checks")
	}
	evidence, evidenceErr := s.Evidence(w.ID)
	if evidenceErr != nil {
		return evidenceErr
	}
	digest, digestErr := s.treeDigest(ctx, w.Repo, w.Recipe.Checks)
	if digestErr != nil {
		return digestErr
	}
	latestTest := int64(0)
	testedDigest := ""
	for _, item := range evidence {
		if item.Phase == "green" && item.Passed && item.ID > latestTest {
			latestTest = item.ID
			testedDigest = item.TreeDigest
		}
	}
	if testedDigest == "" || testedDigest != digest {
		return fmt.Errorf("repository changed since the latest passing check; rerun independent testing before closing issues")
	}
	proposals, err := ParseIssueProposals(w)
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for _, p := range proposals {
		active[issueMarker(w.ID, p)] = true
	}
	for _, link := range w.Issues {
		if !active[link.Marker] {
			return fmt.Errorf("issue scope changed after creation; reconcile superseded issue %s before closure", link.URL)
		}
	}
	return nil
}
func deliveryEvidence(w Workflow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Delivery evidence for workflow %s\n\n", w.ID)
	report := ""
	for _, result := range w.Results {
		if result.State != "pass" {
			continue
		}
		for _, stage := range w.Recipe.Stages {
			if result.Stage == stage.Name && stage.Role == "deliverer" {
				report = result.Output
			}
		}
	}
	if len(report) > 24000 {
		report = report[:24000] + "\nFull report retained in the local workflow record."
	}
	b.WriteString(report)
	b.WriteString("\n\nExecutable check metadata follows. Full logs and immutable artifact copies are retained in Crew's local evidence records.\n")
	return b.String()
}
