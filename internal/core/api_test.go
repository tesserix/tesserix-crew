// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func apiProvider(endpoint string) Provider {
	return Provider{Kind: "api", Format: "crew", Auth: "api-key", KeyEnv: "CREW_API_TEST_KEY", Endpoint: endpoint, DefaultModel: "fake-test-model", Capabilities: []string{"read", "write", "tools"}, MaxRounds: 3, RequestTimeout: "2s"}
}
func TestDirectAPIKeyRuntimeAndEditPermissions(t *testing.T) {
	for _, edits := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-only", true: "edits"}[edits], func(t *testing.T) {
			t.Setenv("CREW_API_TEST_KEY", "test-api-secret")
			repo, home := t.TempDir(), t.TempDir()
			rounds := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Authorization") != "Bearer test-api-secret" {
					t.Error("API authentication missing")
				}
				data, _ := io.ReadAll(req.Body)
				if strings.Contains(string(data), "test-api-secret") {
					t.Error("key included in API prompt")
				}
				rounds++
				if rounds == 1 {
					io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call1","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"result.go\",\"content\":\"package result\\n\"}"}}]}}]}`)
				} else {
					if edits && !strings.Contains(string(data), "File written") {
						t.Error("write result not sent")
					}
					if !edits && !strings.Contains(string(data), "read-only permission") {
						t.Error("write denial not sent")
					}
					io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"complete test-api-secret"}}],"usage":{"prompt_tokens":25,"completion_tokens":5}}`)
				}
			}))
			defer server.Close()
			store, err := OpenStore(home)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			session, _ := store.Create(repo, "remote", "")
			config := Config{DefaultAgent: "remote", Providers: map[string]Provider{"remote": apiProvider(server.URL)}}
			runner := Runner{Store: store, Home: home, Config: config}
			result, err := runner.Run(context.Background(), session, RunOptions{Task: "create a file test-api-secret", Edits: edits}, func(Update) {})
			if err != nil || result != "complete [redacted]" {
				t.Fatal(result, err)
			}
			_, fileErr := os.Stat(filepath.Join(repo, "result.go"))
			if edits && fileErr != nil {
				t.Fatal(fileErr)
			}
			if !edits && !os.IsNotExist(fileErr) {
				t.Fatal("read-only API wrote a file")
			}
			events, _ := store.Events(session.ID)
			for _, event := range events {
				if strings.Contains(event.Content, "test-api-secret") {
					t.Fatal("API key journaled")
				}
			}
		})
	}
}
func TestAPIRepositoryToolsRejectEscapesAndStateAccess(t *testing.T) {
	repo, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600)
	os.Symlink(outside, filepath.Join(repo, "link"))
	for _, path := range []string{"../secret", ".git/config", ".crew/config.toml", "link/secret"} {
		if _, err := repoToolPath(repo, path, false); err == nil {
			t.Fatal("read escape allowed", path)
		}
	}
	os.MkdirAll(filepath.Join(repo, "src"), 0700)
	os.Symlink(filepath.Join(repo, "src"), filepath.Join(repo, "alias"))
	if _, err := repoToolPath(repo, "alias/file.go", true); err == nil {
		t.Fatal("symlink write allowed")
	}
}
func TestAPIRequestCancellationAndRoundLimit(t *testing.T) {
	t.Setenv("CREW_API_TEST_KEY", "test-api-secret")
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		close(started)
		select {
		case <-req.Context().Done():
		case <-release:
		}
	}))
	defer func() { close(release); server.Close() }()
	store := workflowStore(t)
	home, repo := t.TempDir(), t.TempDir()
	session, _ := store.Create(repo, "remote", "")
	runner := Runner{Store: store, Home: home, Config: Config{DefaultAgent: "remote", Providers: map[string]Provider{"remote": apiProvider(server.URL)}}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	begin := time.Now()
	if _, err := runner.Run(ctx, session, RunOptions{Task: "inspect"}, func(Update) {}); err == nil || time.Since(begin) > 3*time.Second {
		t.Fatal("API cancellation failed", err)
	}
}
func TestAPIValidationRejectsCredentialURLs(t *testing.T) {
	for _, url := range []string{"https://key@service.example/v1/chat/completions", "https://service.example/v1/chat?key=secret", "http://service.example/v1/chat"} {
		if err := apiProvider(url).Validate("remote"); err == nil {
			t.Fatal("unsafe endpoint allowed", url)
		}
	}
	var result apiMessage
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":null}`), &result); err != nil || result.Content != nil {
		t.Fatal(result, err)
	}
}

func TestAPIToolRoundsAreBounded(t *testing.T) {
	t.Setenv("CREW_API_TEST_KEY", "test-api-secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		io.Copy(io.Discard, req.Body)
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call1","type":"function","function":{"name":"list_files","arguments":"{}"}}]}}]}`)
	}))
	defer server.Close()
	s := workflowStore(t)
	session, _ := s.Create(t.TempDir(), "remote", "")
	p := apiProvider(server.URL)
	p.MaxRounds = 1
	r := Runner{Store: s, Home: t.TempDir(), Config: Config{DefaultAgent: "remote", Providers: map[string]Provider{"remote": p}}}
	if _, err := r.Run(context.Background(), session, RunOptions{Task: "inspect"}, func(Update) {}); err == nil || !strings.Contains(err.Error(), "round limit") {
		t.Fatal(err)
	}
}
