// SPDX-License-Identifier: Apache-2.0
package core

import "strings"

// Match only self-contained social turns. Mixed greetings and tasks retain tools.
func isGreeting(task string) bool {
	task = strings.Trim(strings.ToLower(strings.TrimSpace(task)), "!?., ")
	switch task {
	case "hi", "hello", "hey", "hi there", "hello there", "good morning", "good afternoon", "good evening", "thanks", "thank you":
		return true
	}
	return false
}
