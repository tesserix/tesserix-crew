# Delivery lifecycles

Crew can run a durable sequence of agent stages, user approvals, executable checks and GitHub
actions. A lifecycle run owns a shared Crew session. Its recipe, provider/model
assignments, outputs, attempts and decisions are saved locally in SQLite.
Configuration changes affect new runs; existing recipes remain pinned.

## Start and advance a run

```sh
crew workflow presets
crew workflow start "Fix the checkout regression"
crew workflow list
crew workflow status RUN_ID
crew workflow status --json RUN_ID
crew workflow next RUN_ID
```

Flags go before the run ID or task. `start` records a run without launching a
provider. Each `next` executes **one** agent stage, then stops. Crew prints the
next action and never automatically approves a user gate.

The default recipe is:

| Stage | Provider or action |
| --- | --- |
| Design | Claude, read-only |
| Plan | Claude, read-only |
| Agent review | Codex, read-only |
| Create GitHub issues | Crew creates issues from the reviewed plan |
| User review | Explicit user approval or rejection |
| TDD implementation | Codex, edits require `--allow-edits` |
| Independent testing | Claude, commands require `--allow-edits` |
| Deliver | Claude prepares a report, read-only |
| Close GitHub issues | Crew validates evidence and closes tracked issues |

```sh
crew workflow record --note 'https://github.com/org/repo/issues/42 — approved scope' RUN_ID
crew workflow approve --note 'Acceptance criteria reviewed' RUN_ID
crew workflow next --allow-edits RUN_ID
```

Inspect `status --json` to review **all** outputs and decisions before approval.
GitHub actions use native `gh` authentication. The planner must provide a fenced
`crew-issues` JSON array with `title`, `body`, and `acceptance` fields. Crew saves
issue identity and a durable marker, so retries can recover creation responses.
Closure requires approved preceding stages, passing required checks, and an
unchanged repository tree. Replanning an already published scope requires manual
reconciliation; conflicting tracked scope blocks closure.

`crew workflow run --allow-edits RUN_ID` advances through ready stages until a
gate or failure. Old pinned recipes retain their manual checkpoints.

The `review` preset provides a read-only Codex review followed by user approval:

```sh
crew workflow start --preset review "Review the authentication changes"
```

## Failure, feedback and recovery

Agent stages must return a standalone `CREW_STAGE_RESULT: pass`, `fail`, or
`blocked` line. An absent/invalid verdict blocks advancement. A failed process
also fails the stage even if its output claims success. Agent verdicts alone cannot satisfy configured checks. Crew records real
subprocess exits, stage revisions, repository fingerprints and artifact hashes. Testing prompts require applicable E2E or
browser checks and must report unavailable tooling instead of claiming success.

```sh
crew workflow reject --note 'Cover the migration rollback case' RUN_ID
crew workflow retry --note 'Replan with the requested rollback coverage' RUN_ID
crew workflow next RUN_ID
```

Rejecting a user gate stops the run. Retrying a rejected gate rewinds to the
nearest preceding agent stage, preserving feedback and replaying subsequent
checkpoints and approvals. Retrying failed or blocked work reruns the same stage.
No partially executed stage is silently retried with another provider.

Execution uses a renewable stage lease and optimistic revision checks. If the
process crashes, the run remains `running`. Stop the old process and inspect the
working tree. After its 30-second lease expires:

```sh
crew workflow recover --note 'Process exited; reviewed partial changes' RUN_ID
crew workflow retry --note 'Safe to resume after inspecting changes' RUN_ID
```

Recovery marks the stage blocked; a separate explicit retry is required. Ctrl+C
cancels the provider process and records failure when Crew can finish cleanup.

## Custom recipes

Add recipes to `~/.crew/config.toml` or `.crew/config.toml`. Repository recipes
replace same-name global recipes in full. Built-in names can also be overridden.

```toml
[lifecycles.small-change]
description = "Plan, approve, implement and independently test"

[[lifecycles.small-change.stages]]
name = "plan"
kind = "agent"
role = "planner"
agent = "claude"
prompt = "Produce a bounded implementation plan and acceptance criteria."

[[lifecycles.small-change.stages]]
name = "review"
kind = "approval"
prompt = "Review and approve the plan before implementation."

[[lifecycles.small-change.stages]]
name = "implement"
kind = "agent"
role = "implementer"
agent = "codex"
allow_edits = true
prompt = "Implement the approved plan using TDD; record commands and outcomes."

[[lifecycles.small-change.stages]]
name = "test"
kind = "agent"
role = "tester"
agent = "claude"
allow_edits = true
prompt = "Independently test acceptance criteria; report evidence and limitations."
```

Use `crew workflow start --preset small-change "task"`. Agent stages may specify
`model` using the provider's native identifier. Each stage needs a unique name and
prompt. An approval must precede any stage that permits edits; tester providers
must differ from every provider assigned an implementer role. Unsupported
adapters are rejected instead of falling back silently.

All agents receive the session's pinned shared skills. Per-role skill selection,
skill CRUD, provider plugins, test-result verification and automatic repair loops
remain on the roadmap. Testing uses native workspace-write/acceptEdits permissions
so tools can run; its instruction to avoid changing production code is not an
additional filesystem sandbox. Likewise lifecycle gates are application controls,
not protection against an adversarial process with access to Crew's data directory.

## Terminal status

`crew resume RUN_ID` opens the session conversation. The footer shows the current
lifecycle stage and state; `/workflow` displays its stage list. Run lifecycle
commands in a separate terminal. Ordinary chat turns do not advance the lifecycle.

## Executable checks and repair

Configure checks before starting, or review the planner's fenced `crew-checks`
JSON proposal when no checks were configured or inferred. Commands are argument
arrays and are pinned with the recipe. Go and Node projects can infer a default
unit command. Browser/E2E checks require tooling installed in the project; browser
checks require configured screenshot or trace artifacts.

```sh
crew workflow check --name unit --phase red RUN_ID
crew workflow check --name unit --phase green RUN_ID
crew workflow evidence RUN_ID
crew workflow repair --note 'Reproduction and expected behavior' RUN_ID
```

TDD requires a real failed check before passing green evidence, unless the recipe
records an explicit exception. Missing executables and cancellation do not count
as red. Failed checks retain available declared artifacts too. Artifact hashes
verify retained copies; Crew does not guarantee that the check rewrote each
artifact. Independent testers cannot modify production files. Repair explicitly
rewinds to implementation and is bounded by the configured repair count.

The `bugfix` and `maintenance` presets supplement `default` and `review`.
Recipes pin providers, models, stage skills, dependencies, timeouts and checks.
Custom lifecycles may relax provider separation only with an explicit override.

In the terminal, use `/workflow next`, `/workflow approve`, `/workflow reject
FEEDBACK`, `/workflow repair NOTE`, and `/workflow evidence`. Edit stages require
starting the session with `--allow-edits`.
