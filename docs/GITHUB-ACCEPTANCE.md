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

The 2026-10-16 progress acceptance created and closed fixtures
[#25](https://github.com/tesserix/tesserix-crew/issues/25) and
[#26](https://github.com/tesserix/tesserix-crew/issues/26), workflow
`9012fc9d7361995f`. It recorded 12 unique stage comments (approval,
implementation, failed independent check, repair/retest and delivery), with one
final delivery comment per issue. A lost progress-comment reply followed by a
stale comments listing paused advancement. `workflow sync` reconciled metadata
without repeating a provider stage or comment. Completed replay performed no
GitHub calls. Real GitHub API calls and executable checks passed; stage agents
were scripted fixture adapters.

New built-in recipes enable `github_updates = true`; custom recipes can opt in.
Existing pinned recipes retain their original setting. Progress intent is saved
before each comment write; an unknown outcome requires remote reconciliation.
`crew workflow sync ID` (or `/workflow sync`) only synchronizes metadata. Full
user review feedback remains local; published approval comments contain the
recorded decision only. Agent reports and check summaries are published to the
explicitly pinned repository, with long reports bounded to GitHub comment size.
