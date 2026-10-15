// SPDX-License-Identifier: Apache-2.0
package core

import (
	"strings"
	"testing"
)

func TestSingleProviderRolesRequireExplicitIndependenceOverride(t *testing.T) {
	recipe := testRecipe()
	for i := range recipe.Stages {
		if recipe.Stages[i].Kind == "agent" {
			recipe.Stages[i].Agent = "claude"
		}
	}
	providers := map[string]Provider{"claude": BuiltinProviders()["claude"]}
	if err := recipe.ValidateWithProviders(providers); err == nil || !strings.Contains(err.Error(), "different provider") {
		t.Fatal("single-provider recipe did not explain independence requirement", err)
	}
	recipe.AllowSelfTest = true
	if err := recipe.ValidateWithProviders(providers); err != nil {
		t.Fatal("explicit independence override was ignored", err)
	}
}
