# GitHub lifecycle acceptance

The 2026-10-15 real GitHub acceptance used isolated scripted stage adapters,
real executable arithmetic checks and the native `gh` login/API.

The first run exposed an eventual-visibility bug: a successful creation response
was lost, and an immediate listing did not yet show the issue. Retry created a
duplicate. The duplicate fixture [#19](https://github.com/tesserix/tesserix-crew/issues/19)
was explicitly reconciled/archived, preserving canonical [#20](https://github.com/tesserix/tesserix-crew/issues/20)
and [#21](https://github.com/tesserix/tesserix-crew/issues/21). Those canonical issues
then completed closure recovery with one delivery comment each.

The corrected clean run created exactly two issues,
[#22](https://github.com/tesserix/tesserix-crew/issues/22) and
[#23](https://github.com/tesserix/tesserix-crew/issues/23), and verified:

- Explicit session/workflow references and a resolved issue dependency link.
- Lost creation response plus a deliberately stale listing: the reserved intent
  paused reconciliation and never issued another create.
- Approval gating, actual red/green implementation, a seeded failed independent
  check, open issues on failure, bounded repair and passing retest/delivery.
- Lost closure response plus stale remote state: no duplicate close/comment writes.
- Both issues closed, with one delivery comment each; replay after completion
  made no GitHub calls. The full run retained 29 CLI transitions.

Creation/closure intents are persisted before writes. Unconfirmed outcomes require
remote reconciliation instead of blind repetition. Older ambiguous failures without
intents are treated conservatively. Published scope conflicts and invalid dependency
graphs are rejected before writes. Repository leases serialize Crew edits during
closure, and evidence/tree checks are repeated after remote lookup.

Optional issue proposal `key` and `depends_on` fields refer to distinct earlier
keys; prerequisites must be listed first. Existing proposals without those fields
retain their original durable marker hashes.

Opt-in reproduction (creates/updates/closes labelled fixture issues):

```sh
go build -o /private/tmp/crew-github ./cmd/crew
python3 scripts/smoke_github.py --crew /private/tmp/crew-github --github-repo OWNER/REPO
```

This verifies real GitHub transitions with scripted stage agents. Separate
[live provider acceptance](LIVE-ACCEPTANCE.md) covers real Claude/Codex coding.
Per-stage GitHub progress updates remain part of the active #4 follow-up until
their own delivery and validation complete.
