// SPDX-License-Identifier: Apache-2.0
package core

import "testing"

func TestGrokResult(t *testing.T) {
	p := BuiltinProviders()["grok"]
	if err := p.Validate("grok"); err != nil {
		t.Fatal(err)
	}
	e := NormalizeProvider(p, []byte(`{"text":"hello","stopReason":"end_turn","thought":"private reasoning"}`))
	if e.Text != "hello" || e.Error != "" {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"text":"partial","stopReason":"max_tokens"}`, `{"error":"failed"}`, `bad`} {
		if NormalizeProvider(p, []byte(raw)).Error == "" {
			t.Fatal("incomplete result accepted")
		}
	}
	c := Config{Providers: BuiltinProviders()}
	if _, err := c.Command(AgentOptions{Agent: "grok", Edits: true}); err == nil {
		t.Fatal("unverified writes enabled")
	}
}
