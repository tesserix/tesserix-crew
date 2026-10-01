# Tesserix Crew

One persistent coding session across your subscribed coding agents.

Crew wraps installed coding CLIs, reuses their existing authentication, journals
sessions locally, and transfers context when you change agents. It includes a
terminal interface, task commands, rule-based routing, shared skills, and bounded
agent delegation over MCP.

**Early development:** Claude Code and Codex adapters have local command-contract
checks and simulated process tests. A small Claude subscription greeting has been
verified; full coding, approvals, resume, and delegation runs still need live checks.
Gemini is detected by `doctor`, but its adapter remains unavailable until verified
against an installed CLI. No API keys are required by Crew itself.

## Build

Install the public release with Homebrew:

```sh
brew install tesserix/tap/crew
crew doctor
crew
```

See [Homebrew release setup](docs/HOMEBREW.md).

The next milestone is tracked in the [provider/lifecycle/skills roadmap](docs/ROADMAP.md).

Go 1.24+ on macOS or Linux:

```sh
make build
./bin/crew doctor
./bin/crew
```

Or install from the checkout:

```sh
go install ./cmd/crew
```

## Use

```sh
crew run --agent codex --dry-run "review the authentication flow"
crew run --agent claude "explain the failing tests"
crew run --agent codex --allow-edits "fix the failing tests"
crew --agent auto --allow-edits
crew sessions
crew resume
crew resume --repo /path/to/repo SESSION_ID
crew run --session SESSION_ID --agent codex "continue the implementation"
crew context SESSION_ID
```

Flags go **before** positional tasks or session IDs. Interactive commands:
`/agent claude|codex|gemini|auto`, `/model NAME`, `/status`, `/context`, `/skills`,
`/help`, `/quit`. Ctrl+C cancels an active task; when idle it exits. PgUp/PgDown
scroll the output.

The interface separates user and agent messages, shows subdued tool activity,
streams Claude text, and provides a bordered input and working indicator.
The bottom status area shows repository, branch, changed-entry count, agent/model,
edit mode, skill count, elapsed time, session, and current task/delegation state.

### Permissions

Initial headless adapters use Claude's default permission mode with built-in shell
and mutation tools disabled, or Codex's read-only sandbox. Claude's plan mode is
avoided because its plan-approval interaction is incompatible with headless turns.
`--allow-edits` selects Claude's acceptEdits mode or Codex's workspace-write
sandbox. Crew does not pass permission-bypass flags or replace native credentials.
Headless approval requests may be denied; there is no unified approval UI yet.
Existing native settings, hooks, MCP servers, and repository instructions still
apply. These modes are not an additional Crew security sandbox.

### Routing

Global configuration: `~/.crew/config.toml`. Repository configuration:
`.crew/config.toml`. Explicit selection wins; repository rules precede global
rules; otherwise the configured default is used.

```toml
default_agent = "claude"

[[rules]]
contains = ["review", "test", "implement"]
agent = "codex"
```

Automatic mode currently means deterministic configured routing. Model-based
classification, staged workflows, and usage-limit fallback are planned. Crew does
not silently retry a partially executed coding task on another agent.

### Sessions and context

SQLite journal: `~/.crew/sessions.db`. Set `CREW_HOME` or `--home` to relocate it.
It stores user turns, agent responses, raw structured events, native session IDs,
delegation results, and pinned skill manifests. Raw events remain available in the
database; `crew context` prints the readable transcript.

Handoffs include the readable history, shared skill instructions, and current Git
metadata. Returning to an agent resumes its native session and supplies intervening
history. A 240 KB initial handoff budget fails explicitly rather than silently
discarding history. Automatic summarization and selective retrieval are planned.

A renewable repository lease serializes parent runs within one Crew home. It does
not coordinate separate Crew homes or coding tools started outside Crew.

### Shared skills

```sh
crew skills add ./skills/code-review
crew skills add --global ./skills/writing
crew skills list
```

Each skill directory contains `SKILL.md` plus optional supporting files. Repository
skills override global skills with the same name. Sessions pin content-addressed
snapshots; delegated agents inherit those exact revisions. Instructions are passed
in shared context, with paths to supporting files. Crew does not change vendor
skill directories or assume identical tool capabilities across agents.

Install skills before creating a session. Existing sessions retain their pinned
versions. Git/Registry installation, session overrides, prerequisites, and skill
removal commands are planned.

### Agent delegation

Parent Claude/Codex commands receive a session-scoped Crew stdio MCP configuration.
The `delegate` tool starts a bounded child task; `session_context` reads shared
context. Children return content and cannot recursively delegate. Delegation is
synchronous with a ten-minute timeout and uses read-only native modes. The parent
integrates code or asset suggestions. Child sessions and results are journaled.

For example, Codex can ask Claude to draft copy or review a proposed change. Gemini
delegation becomes available after its CLI adapter is verified. Raster image
generation needs an explicitly supported image tool; a CLI subscription alone
does not establish that capability.

## Development

```sh
make test
make check
```

See [implementation plan](docs/PLAN.md) and
[CLI compatibility](docs/CLI-COMPATIBILITY.md). Registry and DevAI integration are
optional planned additions; no platform services are needed for local sessions.

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) and [CONTRIBUTING.md](CONTRIBUTING.md).
