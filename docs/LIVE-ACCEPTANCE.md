# Live lifecycle acceptance

The 2026-10-14 acceptance run used real Claude and Codex CLIs against an isolated
Python arithmetic fixture. Codex implemented; Claude independently tested.
The goal covered positive, negative, zero and decimal addition.

Verified behavior:

- Interrupting Claude startup left the workflow failed; explicit retry resumed
  design successfully after the startup-ID persistence fix.
- Design, planning and independent review completed with real providers.
- Attempting to advance the user-review gate left it awaiting approval and did
  not mutate the production fixture. The isolated scope and checks were then approved.
- Codex recorded executable unit red/green evidence and implemented addition.
- A controlled negative-input regression was injected after implementation.
  Claude detected it with the pinned acceptance check and blocked delivery.
- Explicit bounded repair rewound to Codex implementation. It added a negative-input
  unit regression, recorded fresh red/green evidence and repaired the defect.
- Claude retested: two unit tests and three acceptance tests passed. Delivery then
  completed, with eleven check records retained across attempts.

Two integration defects were fixed from the live run: startup resume IDs were
saved before conversation creation, and headless providers could deny Crew's
check tool despite workflow approval and edit permission. Check-tool authorization
is scoped to `run_check` on edit-enabled sessions; its pinned-check, role and
workflow gates still apply. Stage prompts list only applicable executable checks.
Codex's setting follows the [official configuration reference](https://developers.openai.com/codex/config-reference/).

The scripted full-default acceptance test separately covers all nine stages,
store reopen/resume, a lost GitHub creation response, one issue creation/closure,
explicit approval, executable red/green evidence and independent provider roles.

Limitations: live acceptance used a custom CLI recipe, not real GitHub creation or
closure. The regression was deliberately injected. This run does not establish
browser acceptance, Gemini support, or correctness for arbitrary projects.

Opt-in reproduction (uses existing Claude/Codex logins and model calls):

```sh
go build -o /private/tmp/crew-live ./cmd/crew
python3 scripts/smoke_live.py --crew /private/tmp/crew-live
```

The harness prints its isolated root and keeps workflow JSON, stderr, timeline,
SQLite journal, fixture files and check artifacts there. It does not run in CI
automatically or create GitHub issues.
