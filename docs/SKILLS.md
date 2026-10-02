# Shared skills

Skills contain `SKILL.md` plus optional support files. Global and repository
catalogs support add, show, edit, update, remove, enable and disable operations.
Removed catalog entries are archived; historical session snapshots remain.
Six built-in role skills cover design, planning, review, TDD, testing and delivery.
Lifecycle stages select shared skills plus their role template and pin snapshots.
Ordinary chats use active installed shared skills, with explicit selection via
`/skills select NAME...` or `/skills select none`.

```sh
crew skills list
crew skills add ./my-skill
crew skills edit my-skill
crew skills disable my-skill
crew skills refresh SESSION_ID
crew skills refresh --apply SESSION_ID
crew skills add --git https://github.com/example/skills.git --ref FULL_40_CHARACTER_COMMIT --path skills/my-skill
```

Refresh previews changes before applying; applying resets ordinary-session context
cursors. Lifecycle snapshots stay pinned: start a new run to change them. Removing
a selected skill requires updating the selection before the next turn. Git imports
require a complete commit hash, reject symlinks and record provenance in SOURCE.json.
Registry-specific imports remain future work. Optional `skill.toml` prerequisites
list required commands and provider capabilities.
