# Tesserix Crew implementation plan

## Product

`crew` is the terminal entry point. Sessions retain context across subscribed coding
agents. Configuration combines explicit choices and automatic routing. Agents may
delegate bounded tasks. Skills are shared across providers. A persistent bottom
status area reports repository, Git branch, agent and progress.

## Architecture boundaries

- Crew owns the terminal interface, agent processes, local sessions and handoffs.
- Agent Development Kit is a reference for reusable contracts. A Go CLI does not
  import its Python runtime; use protocol integration if that becomes useful.
- Agentic Registry provides optional versioned artifact discovery and distribution.
- DevAI provides optional remote workflow execution and run visibility.
- Native CLIs own subscription authentication and provider-specific permissions.

## First foundation implemented

- Go executable; interactive Bubble Tea UI and noninteractive task commands.
- SQLite history, native resume references and global repository session index.
- Claude/Codex structured process adapters; Gemini availability detection.
- Explicit/global/repository routing rules.
- Local/global skills and per-session pinned snapshots.
- Session-scoped stdio MCP delegation; bounded read-only child tasks.
- Repository leases, cancellation, journaled errors and bottom status area.
- Apache 2.0 licensing and repeatable simulated-process checks.

## Next milestone: live compatibility and approvals

1. Install/verify Gemini CLI using its supported subscription authentication.
2. Validate each agent with small real tasks and recorded sanitized event fixtures.
3. Validate native resume and MCP delegation with existing trust settings.
4. Add native protocol transports for interactive approvals where supported.
5. Add async delegation status/cancel tools and model capability discovery.

## Next milestone: intelligent routing and richer context

1. Task classification and configurable planning/implementation/review workflows.
2. Reviewable context summaries, repository checkpoints and selective history retrieval.
3. Explicit fallback policy; inspect partial work before retrying with another agent.
4. Structured goals, decisions, remaining work, and verification records.
5. Session-selected skill overrides, prerequisites and Git-based installation.

## Next milestone: ecosystem and distribution

1. Registry discovery, signature verification, pinned imports and offline cache.
2. Optional DevAI run reporting and remote delegation.
3. Isolated worktrees for parallel coding, with parent-controlled integration.
4. Supported image/asset tools with explicit capability checks.
5. Homebrew releases, signed/checksummed binaries and broader platform verification.

## Acceptance scenario

Start a task with Claude, continue with Codex, delegate content generation, review
with Gemini, exit and resume with goals, decisions, changes and outstanding work
preserved. This full scenario is pending live compatibility verification.
