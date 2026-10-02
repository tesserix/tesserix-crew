# Next milestone

Track progress in [roadmap issue #8](https://github.com/tesserix/tesserix-crew/issues/8).

Default lifecycle:

**Design → Plan → Agent review → Create GitHub issues → User review → TDD implementation → Agent E2E/browser testing → Deliver → Close GitHub issues.**

Users can select predefined lifecycles or change the stages. The user-review gate
requires explicit approval. Implementation and testing use different providers by
default. Each role combines a provider, model, declared capabilities and selected
shared skills. Users can add, edit, update and remove skills.

| Scope | Issue |
| --- | --- |
| Provider plugins with CLI subscriptions and API-key authentication | [#2](https://github.com/tesserix/tesserix-crew/issues/2) |
| Shared skill catalog and role-specific selection | [#5](https://github.com/tesserix/tesserix-crew/issues/5) |
| Skill-based roles and independent providers | [#3](https://github.com/tesserix/tesserix-crew/issues/3) |
| Durable lifecycle presets, custom stages and review gates | [#1](https://github.com/tesserix/tesserix-crew/issues/1) |
| GitHub issue creation, review, evidence and closure | [#4](https://github.com/tesserix/tesserix-crew/issues/4) |
| Independent E2E/browser testing and repair loops | [#6](https://github.com/tesserix/tesserix-crew/issues/6) |
| Lifecycle, provider, skill and review UI | [#7](https://github.com/tesserix/tesserix-crew/issues/7) |

Lifecycle foundations are now implemented: pinned built-in/custom recipes, durable
outputs and decisions, explicit user gates, independent implementation/testing
providers, revision-checked transitions, crash recovery, and terminal status.
See [lifecycle usage and limitations](LIFECYCLES.md). GitHub issue actions remain
manual checkpoints; extensible providers, per-role skills and verified automated
E2E/browser repair loops remain planned.

Architecture ideas considered from Munder Difflin are documented in
[architecture references](ARCHITECTURE-REFERENCES.md).
