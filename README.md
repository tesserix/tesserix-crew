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
brew install --cask tesserix/tap/crew  # macOS
# Linux: brew install tesserix/tap/crew
crew doctor
crew
```

See [Homebrew release setup](docs/HOMEBREW.md).
The initial macOS release is not notarized; see the linked first-launch instructions
if Gatekeeper asks you to verify the application.

Implemented features and remaining work are tracked in the [roadmap](docs/ROADMAP.md).

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

Start with `crew setup` to choose a project default from available providers.
It preserves existing routing, skills and lifecycle configuration. Use
`crew setup --agent codex` for noninteractive setup, or add `--global` for a
global default. Project defaults take precedence over global defaults.

`crew doctor` reports provider capabilities, missing executables, configured or
inferred test commands, and suggested fixes. `crew doctor --check-auth` adds
bounded native login probes for Claude, Codex and GitHub without model calls.
Other native logins remain explicitly unverified. `crew doctor --json` is useful
for automation. Diagnostics do not run project tests or change configuration.

```sh
crew run --agent codex --dry-run "review the authentication flow"
crew run --agent claude "explain the failing tests"
crew run --agent codex --allow-edits "fix the failing tests"
crew --agent auto --allow-edits
crew sessions
crew sessions --pick
crew sessions --json
crew resume
crew resume --latest
crew resume --repo /path/to/repo SESSION_ID
crew run --session SESSION_ID --agent codex "continue the implementation"
crew context SESSION_ID
```

Flags go **before** positional tasks or session IDs. Interactive commands:
`/agent claude|codex|gemini|auto`, `/model NAME`, `/status`, `/context`, `/skills`,
`/sessions`, `/workflow start`, `/workflow`, `/help`, `/quit`. Ctrl+C cancels an active task; when idle it exits. PgUp/PgDown
scroll the output.

`crew resume` opens a searchable picker for this project's sessions;
`--latest` resumes the latest directly. `crew sessions --pick` browses all
projects. Inside Crew, `/sessions` searches the current project and
`/sessions all` includes other projects. Search covers task, repository, agent,
session ID, last activity, workflow state and next action. Switching loads the
selected session's history and provider configuration, clears the model override,
and retains the launch-time edit permission. Delegated child sessions remain
accessible through `crew context CHILD_ID` and are omitted from the picker.

The interface separates user and agent messages, shows subdued tool activity,
streams Claude text, and provides a bordered input and working indicator.
The bottom status area shows repository, branch, changed-entry count, agent/model,
edit mode, skill count, elapsed time, session, and current task/delegation state.

### Delivery lifecycles

```sh
crew workflow start "Fix the checkout regression"
crew workflow next RUN_ID
crew workflow status --json RUN_ID
crew workflow approve RUN_ID
```

The default lifecycle includes design, plan, independent review, automated GitHub
issue actions, explicit user approval, TDD implementation, independent testing and
delivery. `next` advances one stage; `run` advances until a gate or failure. See [lifecycle recipes and recovery](docs/LIFECYCLES.md).

Inside Crew, `/workflow start` guides you through choosing a preset and describing
a task. Creation pins a new session without running agents. `/workflow` opens a
review screen with all stage outputs, decisions, changed paths, pinned checks and
recorded evidence. Use `n` for the next stage, `a` then Enter to approve, `r` for
rejection feedback, and Esc to return to chat. A changed workflow revision requires
a fresh review before approval. Native tool permissions remain separate from
these workflow gates.

### Permissions

Initial headless adapters use Claude's default permission mode with built-in shell
and mutation tools disabled, or Codex's read-only sandbox. Claude's plan mode is
avoided because its plan-approval interaction is incompatible with headless turns.
`--allow-edits` selects Claude's acceptEdits mode or Codex's workspace-write
sandbox. Crew does not pass permission-bypass flags or replace native credentials.
Headless approval requests may be denied. With Claude Code 2.1.287, the experimental
`crew --agent claude --native-approvals` bridge forwards native permission requests
to the terminal: allow one invocation, deny it, or cancel the task. It does not
persist wider permission rules. Other providers retain their headless permission
behavior; permission failures include recovery guidance. Workflow approval and
native tool approval are separate controls.
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

Automatic mode tries matching task rules in order, then the configured default,
then other eligible providers. It checks local installation, required key presence
and read/write capability before execution. `/status` shows the recorded selection
reason. A pinned provider or provider-specific model prevents silent fallback.
Native login validity is not inferred from an installed executable. Crew does not
retry a partially executed coding task on another agent.

### Sessions and context

In the terminal, **Ctrl+S** searches saved sessions and **Ctrl+N** starts a fresh
session in the current project. The picker marks the current session; `/sessions all`
also searches other projects. `/new` provides the same fresh-session action.

**Esc** or **Ctrl+C** cancels an active chat task without closing the session.
`/retry` restores the last chat task as editable input; review it and press Enter
to send it again. Changes already made by an interrupted task remain in the project.
While an agent works, progress shows its current activity, elapsed time, and long
periods without new output. Mouse wheel and PgUp/PgDn scroll the response.

SQLite journal: `~/.crew/sessions.db`. Set `CREW_HOME` or `--home` to relocate it.
It stores user turns, agent responses, raw structured events, native session IDs,
delegation results, and pinned skill manifests. Raw events remain available in the
database; `crew context` prints the readable transcript.

Handoffs include the readable history, shared skill instructions, and current Git
metadata. Returning to an agent resumes its native session and supplies intervening
history. A 240 KB handoff budget fails explicitly rather than silently discarding history.
`/context status` shows full-handoff size; `/context pin TEXT` preserves a goal or
decision; `/context search TEXT` retrieves older turns. `/context draft` creates an
inactive extractive summary for review. `/context apply` opens that draft and
requires confirmation; it does not discard the journal. Drafts can omit details:
edit/import a reviewed version with `crew context draft --file PATH SESSION`.
Summaries and pins are for ordinary sessions; lifecycle context stays pinned.
See [context and latency](docs/CONTEXT-AND-LATENCY.md).

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
versions. Git imports pinned to full commit hashes, session selection and refresh,
prerequisites, and skill catalog editing/removal are implemented. See
[shared skills](docs/SKILLS.md). Registry-specific imports remain planned.

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

Provider configuration: [providers](docs/PROVIDERS.md). Shared catalog and imports: [skills](docs/SKILLS.md). Executable delivery checks: [lifecycles](docs/LIFECYCLES.md). Optional release signing: [macOS signing](docs/MACOS-SIGNING.md).

The footer shows the active provider, repository/branch and real turn timing.
`/status` includes context preparation, first provider event, first output, total
latency and context bytes. Native resumes avoid resending unchanged selected
skills; changing the selection resets context cursors so new instructions arrive.
Provider/model latency can still dominate a turn. Start inside the repository or
use `crew --repo /path/to/project` to display its branch.

Standalone greetings skip Crew delegation setup. The built-in Claude adapter disables tools and native MCP servers for these turns; mixed greetings and coding requests retain normal tool access.

`/agent` opens an available-provider picker filtered for the current edit mode.
`/model` opens a model picker: Codex uses its installed app-server catalog; Claude
uses native aliases; plugins can declare `models = ["NAME", ...]` in configuration.
`/model NAME` remains available when catalog discovery is unsupported.
`crew benchmark --agent claude --runs 2` measures a fresh turn followed by resume;
use `--task` and `--timeout` for explicit read-only benchmark scenarios.
