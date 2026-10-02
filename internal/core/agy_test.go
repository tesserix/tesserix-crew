// SPDX-License-Identifier: Apache-2.0
package core

import (
	"strings"
	"testing"
)

func TestAGYEvents(t *testing.T) {
	p := BuiltinProviders()["agy"]
	if err := p.Validate("agy"); err != nil {
		t.Fatal(err)
	}
	init := NormalizeProvider(p, []byte(`{"event":"init","conversation_id":"conversation"}`))
	if init.Native != "conversation" {
		t.Fatal(init)
	}
	delta := NormalizeProvider(p, []byte(`{"event":"step_update","step_update":{"step_type":"agent_response","text_delta":"hello"}}`))
	if !delta.Delta || delta.Text != "hello" {
		t.Fatal(delta)
	}
	result := NormalizeProvider(p, []byte(`{"event":"result","result":{"status":"SUCCESS","response":"hello","conversation_id":"conversation"}}`))
	if result.Delta || result.Text != "hello" || result.Native != "conversation" {
		t.Fatal(result)
	}
	denied := NormalizeProvider(p, []byte(`{"event":"result","result":{"status":"SUCCESS","denied_actions":[{"action":"write_file"}]}}`))
	if denied.Error == "" {
		t.Fatal("permission denial accepted as success")
	}
	failed := NormalizeProvider(p, []byte(`{"event":"result","result":{"status":"ERROR"}}`))
	if failed.Error == "" {
		t.Fatal("failed result accepted")
	}
}

func TestAGYCommandPermissions(t *testing.T) {
	c := Config{Providers: BuiltinProviders()}
	args, err := c.Command(AgentOptions{Agent: "agy", Model: "chosen-model", Native: "conversation"})
	if err != nil {
		t.Fatal(err)
	}
	command := strings.Join(args, " ")
	for _, want := range []string{"--sandbox", "--mode plan", "--model chosen-model", "--conversation conversation"} {
		if !strings.Contains(command, want) {
			t.Fatalf("missing %s: %s", want, command)
		}
	}
	if strings.Contains(command, "dangerously-skip") {
		t.Fatal("permission bypass")
	}
	if _, err := c.Command(AgentOptions{Agent: "agy", Edits: true}); err == nil {
		t.Fatal("unverified write support enabled")
	}
}
