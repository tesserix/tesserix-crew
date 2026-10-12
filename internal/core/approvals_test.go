// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeControlAllowsOnlyExactInvocationAndDefaultsToDeny(t *testing.T) {
	raw := []byte(`{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"printf safe"},"permission_suggestions":[{"type":"addRules"}]}}`)
	for _, allow := range []bool{false, true} {
		var out bytes.Buffer
		handled, err := handleClaudeControl(context.Background(), raw, &out, func(_ context.Context, v ToolApproval) (bool, error) {
			if v.Tool != "Bash" || !strings.Contains(string(v.Input), "printf safe") {
				t.Fatal(v)
			}
			return allow, nil
		})
		if !handled || err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "permission_suggestions") || strings.Contains(out.String(), "addRules") {
			t.Fatal("persistent rules granted")
		}
		var result struct {
			Response struct {
				ID       string `json:"request_id"`
				Response struct {
					Behavior string `json:"behavior"`
				} `json:"response"`
			} `json:"response"`
		}
		json.Unmarshal(out.Bytes(), &result)
		want := "deny"
		if allow {
			want = "allow"
		}
		if result.Response.ID != "r1" || result.Response.Response.Behavior != want {
			t.Fatal(out.String())
		}
	}
	var denied bytes.Buffer
	handleClaudeControl(context.Background(), raw, &denied, nil)
	if !strings.Contains(denied.String(), "deny") {
		t.Fatal("nil callback permitted tool")
	}
}

func TestClaudeDuplexProcessContract(t *testing.T) {
	script := filepath.Join(t.TempDir(), "adapter.py")
	os.WriteFile(script, []byte(`import sys,json
init=json.loads(sys.stdin.readline());assert init['request']['subtype']=='initialize'
user=json.loads(sys.stdin.readline());assert user['type']=='user' and user['message']['content']=='hello'
print(json.dumps({'type':'control_request','request_id':'r1','request':{'subtype':'can_use_tool','tool_name':'Read','input':{'file_path':'test.txt'}}}),flush=True)
response=json.loads(sys.stdin.readline());assert response['response']['response']['behavior']=='allow'
print(json.dumps({'type':'result','result':'approved response'}),flush=True)
`), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	seen := false
	err := executeProcess(ctx, []string{"python3", script}, "hello", t.TempDir(), func(raw []byte) {
		if strings.Contains(string(raw), "approved response") {
			seen = true
		}
	}, func(context.Context, ToolApproval) (bool, error) { return true, nil }, true)
	if err != nil || !seen {
		t.Fatal(err, seen)
	}
}
