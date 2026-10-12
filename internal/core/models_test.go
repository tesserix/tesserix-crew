// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeModelCatalogHandshake(t *testing.T) {
	bin := t.TempDir()
	script := `#!/usr/bin/env python3
import sys,json
init=json.loads(sys.stdin.readline());assert init['method']=='initialize'
print(json.dumps({'id':1,'result':{}}),flush=True)
ready=json.loads(sys.stdin.readline());assert ready['method']=='initialized'
request=json.loads(sys.stdin.readline());assert request['method']=='model/list'
print(json.dumps({'id':2,'result':{'data':[{'model':'fixture-model','displayName':'Fixture','description':'Catalog discovery','hidden':False}]}}),flush=True)
`
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	choices, err := Config{}.ModelChoices(context.Background(), "codex")
	if err != nil || len(choices) != 1 || choices[0].ID != "fixture-model" {
		t.Fatal(choices, err)
	}
}
