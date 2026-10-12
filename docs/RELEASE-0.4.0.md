# Tesserix Crew v0.4.0

This release makes setup, resuming work and reviewing delivery workflows available
through guided terminal controls, and adds reviewed context management.

- `crew setup` selects a project/global provider while preserving other config.
- `crew doctor` explains capabilities, missing tools and optional login probes.
- Searchable session pickers show task, repository, activity and next action.
  `crew resume --latest` retains direct latest-session resume.
- `/workflow start` creates a pinned workflow; `/workflow` reviews all outputs,
  decisions, changed paths, checks and evidence. Approval requires confirmation
  and rejects an outdated workflow revision.
- Context usage, pinned decisions, inactive extractive summary drafts, reviewed
  application and history search preserve the complete journal. Imported edits
  retain the draft boundary; later turns remain in the handoff.
- `/agent` filters available providers for edit capability. `/model` supports the
  installed Codex catalog, Claude aliases and configured plugin models.
- Experimental Claude native permission prompts use `--native-approvals`:
  allow once, deny or cancel. Native rules remain in force. Other providers retain
  headless approvals, with actionable failure recovery.
- `crew benchmark` measures fresh/resumed turns. Greetings omit Git inspection;
  approval review time is reported separately from preparation and model output.

Validation includes static analysis, Go race tests, Python packaging tests,
terminal/CLI smoke checks, native Codex model discovery, live Claude host-protocol
allow/deny checks, live Claude/Codex greeting/resume timings, and a real Chrome
regression/independent-testing/repair scenario with eight recorded checks and
retained failing/passing screenshots. The browser scenario uses scripted adapters;
it does not establish full live-agent E2E compatibility.

macOS assets retain the existing Go ad-hoc signatures. Developer ID signing and
notarization are pending credential configuration in issue #9. Gemini, fully
automatic repair orchestration, asynchronous delegation and isolated worktrees
remain follow-up work. The measured greeting samples do not establish a consistent
end-to-end latency improvement; see [context and latency](CONTEXT-AND-LATENCY.md).

```sh
brew update
brew upgrade --cask crew  # macOS
# Linux: brew upgrade crew
crew setup
crew doctor --check-auth
crew
```
