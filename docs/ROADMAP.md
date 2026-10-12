# Roadmap and implementation status

The umbrella tracker is [issue #8](https://github.com/tesserix/tesserix-crew/issues/8).
An open tracker does not mean its entire scope is unimplemented. The table below
separates implemented foundations from remaining verification and extensions. Issue
closure requires checking its acceptance criteria against the final evidence.

The default lifecycle is **Design → Plan → Agent review → Create GitHub issues →
User review → TDD implementation → Independent testing → Deliver → Close GitHub
issues**. Presets and custom recipes pin providers, models, capabilities, selected
skills, checks and explicit approval gates.

| Tracker | Implemented | Remaining verification or extensions |
| --- | --- | --- |
| [#1 Lifecycles](https://github.com/tesserix/tesserix-crew/issues/1) | Presets, custom stages, durable outputs, revision checks, user gates, recovery | Broader live-provider acceptance runs |
| [#2 Providers](https://github.com/tesserix/tesserix-crew/issues/2) | CLI/API plugins, environment-key authentication, capability validation | Gemini contract verification; API streaming/native resume; broader live checks |
| [#3 Roles](https://github.com/tesserix/tesserix-crew/issues/3) | Role skills, pinned stage selections, independent implementer/tester policy | Model discovery and richer role configuration UI |
| [#4 GitHub](https://github.com/tesserix/tesserix-crew/issues/4) | Typed issue proposals, durable issue identities, recovery, evidence-checked closure | Reconciliation of changed scope after issue publication; live acceptance checks |
| [#5 Skills](https://github.com/tesserix/tesserix-crew/issues/5) | CRUD, enable/disable, selection, refresh, prerequisites, pinned Git imports | Registry-specific discovery/imports |
| [#6 Testing](https://github.com/tesserix/tesserix-crew/issues/6) | Real subprocess evidence, red/green gates, retained artifacts, tester mutation checks, explicit bounded repairs | Project-specific browser/E2E fixtures and fully automatic repair orchestration |
| [#7 Terminal UI](https://github.com/tesserix/tesserix-crew/issues/7) | Session search, guided workflow creation, full output/evidence review, revision-bound approval confirmation, provider/skill controls | Native approvals beyond the opt-in Claude bridge; broader model catalog support |
| [#9 macOS releases](https://github.com/tesserix/tesserix-crew/issues/9) | Homebrew distribution and optional signing pipeline | Credentials, notarized release and clean first-launch verification |

## Usability improvements implemented

- `crew setup`: searchable default-provider selection or explicit `--agent`, with
  project/global scope and preservation of other configuration.
- `crew doctor`: provider capabilities, missing tools, inferred/configured checks,
  actionable fixes and JSON output. Optional `--check-auth` runs bounded native
  status probes without model tasks; unsupported logins stay unverified.
- Searchable sessions show task, repository, provider, last activity, workflow state
  and next action. `crew resume --latest` retains direct latest-session resume.
- `/workflow start` creates a pinned workflow from a preset and task prompt.
  `/workflow` reviews every output and decision, changed paths, checks and evidence.
  Approval requires confirmation and rejects a changed workflow revision.

## v0.4.0 context, permission and verification additions

- Ordinary sessions support a usage meter, pinned decisions, inactive extractive
  summary drafts, explicit application and history search. The full journal is
  retained; imported edits keep the draft boundary and new turns remain visible.
- The opt-in Claude native host bridge supports allow-once, deny and cancellation,
  with durable request/decision records. Other providers retain headless approvals.
- Provider selection filters available adapters for edit capability; model selection
  uses Codex's installed catalog, Claude aliases or configured plugin model lists.
- Fresh/resumed latency benchmarks record real timing. Greeting preparation avoids
  Git inspection. Native approval wait time is separately visible.
- A real browser regression/repair fixture runs locally and gates CI/releases.
  Live Claude/Codex greetings/resumes, Claude permission allow/deny and Codex
  model discovery have been verified. Full live coding/delegation coverage remains.

## Next priorities

1. Extend native approval transports to other providers, with verified contracts.
2. Optional model-generated summary drafts and richer goal/remaining-work controls;
   extractive summaries always require user review.
3. Broader live coding and delegation fixtures, including Gemini verification.
4. Signed/notarized macOS release after credential configuration.
5. Fully automatic repair orchestration, registry discovery, asynchronous delegation
   and isolated worktrees.

These trackers stay open where acceptance or external prerequisites remain. See
[the v0.4.0 release notes](RELEASE-0.4.0.md) and
[context and latency measurements](CONTEXT-AND-LATENCY.md).

See [lifecycles](LIFECYCLES.md), [providers](PROVIDERS.md), [skills](SKILLS.md),
[implementation plan](PLAN.md) and [architecture references](ARCHITECTURE-REFERENCES.md).
