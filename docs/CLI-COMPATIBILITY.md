# CLI compatibility

Inspected locally on 2026-10-01. Reading help and version output does not establish
live runtime compatibility; real task/session/approval checks remain necessary.

A live Claude subscription greeting completed successfully after replacing plan
mode with default permissions plus disabled shell/mutation tools. Streaming events,
thinking-status events, and tool results are now handled without exposing thinking
content. Full coding/resume/delegation verification remains outstanding.

| CLI | Local version | Structured output | Resume | Initial integration |
| --- | --- | --- | --- | --- |
| Claude Code | 2.1.274 | `--print --verbose --include-partial-messages --output-format stream-json` | `--resume ID` | Default mode with shell/mutation tools disabled, or acceptEdits; `--mcp-config` |
| Codex | 0.159.3 | `exec --json` | `exec resume ID --json` | Global `sandbox_mode` override; session MCP configuration |
| Gemini CLI | Not installed | Pending local verification | Pending | Adapter disabled; doctor reports availability |

Crew inherits the native environment and authentication. It never changes HOME
or copies provider credentials. Agent selection chooses a CLI; `--model` forwards
the native model identifier without assuming all subscriptions expose all models.

Claude result errors/permission denials and Codex error/turn-failed events become
task failures even if the process exits zero. Native session IDs and raw events
are persisted. Subprocess stdin and stderr are drained without competing pipe
writes. Cancellation terminates the agent process group and escalates after three
seconds if needed.

## Remaining verification

- Small live task, native resume, cross-agent handoff, and cancellation per CLI.
- MCP discovery and real delegation under native trust/permission behavior.
- Interactive permission transport; headless mode currently may deny requests.
- Usage-limit classification and fallback eligibility without replaying side effects.
- Gemini subscription login, streaming events, session resume and approval modes.
- CLI version changes and fixtures from actual structured output.
