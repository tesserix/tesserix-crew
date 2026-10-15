# Role acceptance

Role assignments are pinned in each workflow recipe: provider, configured model,
selected skill names, required capabilities, task prompt and stage timeout.
The workflow overview reports these assignments, provider defaults/capabilities,
and any explicit relaxation of implementer/tester independence.

Validation includes:

- Real Codex implementation and independent Claude testing, shared context,
  seeded failure feedback, repair and retesting in [live acceptance](LIVE-ACCEPTANCE.md).
- Single-provider configurations are rejected with a different-provider explanation
  unless the recipe explicitly sets `allow_self_test`; the overview labels that override.
- Per-role pinned skill selections and prerequisites, provider/capability/dependency
  validation, immutable snapshots and repository/global configuration precedence.
- Delegated read-only results are journaled on the parent and preserve child session
  identity. Delegated edits and recursive delegation are rejected.
- Repository leases serialize overlapping parent turns; active-session context edits
  are rejected. This uses serialization, not parallel isolated worktrees.
- Failure/cancellation, retry/recovery and session browsing retain durable records.

The model display reports a configured model or “provider default”; it does not
invent the model chosen internally by a CLI. Local availability checks do not
establish native account login validity. Native permission rules still apply.
