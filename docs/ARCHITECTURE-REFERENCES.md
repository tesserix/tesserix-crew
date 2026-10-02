# Architecture references

[Munder Difflin](https://github.com/chaitanyagiri/munder-difflin) is a useful
reference for coordinating subscribed coding CLIs. Crew remains a Go terminal
application focused on explicit delivery lifecycles. The current lifecycle
implementation is written for Crew; no upstream source or artwork was copied.

| Reference | Idea for Crew | Status |
| --- | --- | --- |
| [Hive design](https://github.com/chaitanyagiri/munder-difflin/blob/main/HIVE.md) | Persist task assignments, results, attempts and decisions; let the orchestrator own transitions | Implemented for lifecycle runs in SQLite |
| [Architecture](https://github.com/chaitanyagiri/munder-difflin/blob/main/docs/ARCHITECTURE.md) | Keep coordination records separate from native process output | Raw CLI events remain separate from lifecycle records; UI reads typed state |
| [Provider contracts](https://github.com/chaitanyagiri/munder-difflin/blob/main/src/shared/agentProvider.ts) | Declare model, resume, authentication and event capabilities per provider | Planned under issue #2 |
| [Context automation](https://github.com/chaitanyagiri/munder-difflin/blob/main/src/shared/providerAutomation.ts) | Verify each provider's context controls rather than guessing flags | Crew verifies adapters locally and blocks unverified Gemini support |
| [Task-ledger tests](https://github.com/chaitanyagiri/munder-difflin/blob/main/test/task-ledger.test.cjs) | Avoid destructive partial UI writes that drop task evidence | UI reads lifecycle state; revision-checked core operations preserve prior results |
| [Control registry](https://github.com/chaitanyagiri/munder-difflin/blob/main/src/main/control.ts) | Separate user decisions from agent traffic; bound automation and retries | Explicit approval, checkpoint, retry and crash-recovery commands; richer controls planned |

Further candidates: typed request/reply messages with IDs and reply references,
per-agent worktrees before concurrent writers, context-budget indicators, and
provider-reported tool/cost events. These need adapter verification and should
not be inferred by scraping terminal text or inventing token counts.

Munder Difflin's architecture documents include future design intentions. They
are references, not proof that every described capability is shipped or tested.
