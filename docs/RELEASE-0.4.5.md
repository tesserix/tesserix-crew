# Tesserix Crew v0.4.5

Interrupted provider startup no longer saves a resume ID before the conversation
exists. If an older Claude resume ID is unavailable, Crew clears it and explains
how to explicitly retry with the saved journal; it does not rerun the task automatically.

Edit-enabled stages authorize only Crew's named `run_check` MCP tool, retaining
pinned-check and workflow gates. Stage prompts distinguish applicable checks from
checks assigned to other roles.

Validation includes real Claude/Codex coding, independent failure discovery,
bounded repair and delivery, plus full-default scripted lifecycle/resume coverage.
See [live acceptance](LIVE-ACCEPTANCE.md) for evidence and limitations.
