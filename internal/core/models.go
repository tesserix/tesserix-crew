// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ModelChoice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Discovery reads the installed Codex app-server catalog, without a model turn.
func codexModels(ctx context.Context) ([]ModelChoice, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "app-server", "--stdio")
	cmd.WaitDelay = time.Second
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var diagnostics tailWriter
	cmd.Stderr = &diagnostics
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		input.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
	}()
	encoder := json.NewEncoder(input)
	if err := encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "tesserix-crew", "version": "0.4.0"}}}); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 65536), 4*1024*1024)
	for scanner.Scan() {
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil {
			continue
		}
		if response.ID != 1 && response.ID != 2 {
			continue
		}
		if len(response.Error) > 0 && string(response.Error) != "null" {
			return nil, fmt.Errorf("native Codex catalog request failed")
		}
		if response.ID == 1 {
			if err := encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
				return nil, err
			}
			if err := encoder.Encode(map[string]any{"id": 2, "method": "model/list", "params": map[string]any{"limit": 100, "includeHidden": false}}); err != nil {
				return nil, err
			}
		} else {
			var result struct {
				Data []struct {
					Model       string `json:"model"`
					Name        string `json:"displayName"`
					Description string `json:"description"`
					Hidden      bool   `json:"hidden"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Result, &result); err != nil {
				return nil, err
			}
			out := []ModelChoice{}
			for _, m := range result.Data {
				if !m.Hidden && m.Model != "" {
					out = append(out, ModelChoice{m.Model, m.Name, m.Description})
				}
			}
			return out, nil
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, fmt.Errorf("native Codex catalog ended without a response")
}

func (c Config) ModelChoices(ctx context.Context, name string) ([]ModelChoice, error) {
	p, err := c.Provider(name)
	if err != nil {
		return nil, err
	}
	if p.Kind == "builtin" && name == "codex" && len(p.Models) == 0 {
		return codexModels(ctx)
	}
	names := append([]string{}, p.Models...)
	if p.DefaultModel != "" {
		names = append(names, p.DefaultModel)
	}
	if len(names) == 0 && p.Kind == "builtin" && name == "claude" {
		names = []string{"sonnet", "opus", "haiku"}
	}
	out := []ModelChoice{}
	seen := map[string]bool{}
	for _, id := range names {
		if strings.TrimSpace(id) != "" && !seen[id] {
			seen[id] = true
			out = append(out, ModelChoice{id, id, "Configured model or native alias; access depends on the provider login"})
		}
	}
	return out, nil
}
