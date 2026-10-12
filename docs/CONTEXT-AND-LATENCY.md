# Context and latency

Crew retains the full local journal. Compaction changes what is handed to a
provider; it does not delete history. Ordinary sessions can pin decisions and use
a reviewed summary. Lifecycle context remains pinned to its recorded recipe.

## Reviewable context

```sh
crew context status SESSION
crew context draft --output summary.txt SESSION
# Review and edit summary.txt against the saved transcript.
crew context draft --file summary.txt SESSION
crew context apply SESSION
crew context pin SESSION "Keep the public API compatible"
crew context pins SESSION
crew context search --query rollback --limit 20 SESSION
crew context unpin SESSION PIN_ID
crew context clear SESSION
```

Flags go before session IDs. The initial draft is extractive: it includes the goal
and recent excerpts, which can omit decisions or verification. Applying it is an
explicit user decision. Importing an edited draft preserves its original history
boundary, so newer turns are still supplied. A replacement draft leaves the active
summary in place until the replacement is applied. Clearing a summary restores
full-history handoffs and can therefore exceed the budget again.

In the terminal, `/context status`, `/context draft`, `/context apply`,
`/context pin TEXT`, `/context pins`, `/context unpin ID`, `/context search TEXT`,
and `/context clear` provide the same controls. Applying opens review, then a
separate confirmation. A changed draft requires a fresh review. `/context` prints
the effective handoff; `crew context SESSION` prints the full saved transcript.

The full-handoff meter measures the selected skills, summary, pins and remaining
history against 240,000 bytes. The footer's **sent context** meter describes the
last actual prompt, which also contains task and delegation instructions. Native
resumes send intervening history and omit unchanged skills; they can therefore
send less than the full-handoff size.

## Measuring latency

```sh
crew benchmark --agent claude --runs 2
crew benchmark --agent codex --runs 2 --timeout 90s
crew benchmark --agent codex --runs 2 --task "Explain the repository structure without changing files"
```

The command executes explicit read-only model turns using native authentication;
provider billing and subscription rules still apply. It uses built-in and project
provider configuration with an isolated journal, retained at the printed path.
The first turn is fresh; later turns resume natively when supported. JSON includes
preparation, first event, first output, total time, prompt bytes and failures.
Failed turns do not contribute to medians and make the command exit unsuccessfully.

Standalone greetings skip delegation setup and Git inspection. The Claude adapter
also disables tools and native MCP servers for these turns. Mixed greetings and
coding tasks retain normal context and tools. `/status` exposes the last turn's
timing; native approval review time is separately recorded and included in total
time. Model response time and network load remain outside Crew's control.

## Live release validation, 2026-10-12

Identical `hello` tasks were run with native defaults against v0.3.3 and the
v0.4.0 candidate. Each provider/version had one fresh and one resumed turn. This
small sequential sample is diagnostic, not a statistically reliable speed claim.

| Provider / turn | v0.3.3 preparation | v0.4.0 preparation | v0.3.3 first output | v0.4.0 first output |
| --- | ---: | ---: | ---: | ---: |
| Claude fresh | 13 ms | <1 ms | 2.536 s | 3.292 s |
| Claude resume | 7 ms | <1 ms | 3.353 s | 3.839 s |
| Codex fresh | 7 ms | <1 ms | 6.076 s | 5.686 s |
| Codex resume | 7 ms | <1 ms | 10.171 s | 5.277 s |

Greeting prompts fell from 868 to 776 bytes. All eight turns completed. Crew's
preparation work decreased, but provider time varied; overall latency is not
consistently lower across these samples. Larger repeated benchmarks are needed
before claiming an end-to-end performance improvement.
