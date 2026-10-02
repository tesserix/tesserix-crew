# Provider plugins

Crew supports subscription CLI adapters and direct API-key providers. Add provider
entries to global or repository configuration; lifecycle runs pin their registry.
Changing a provider affects new runs. Credentials come from native CLI login or a
named environment variable, never command arguments or configuration values.

CLI entries declare `kind = "cli"`, an argument-array `command`, `format`
(`claude`, `codex`, `crew`, or `text`), `auth`, `capabilities`, and explicit
`read_only_args` and `edit_args` when writing is supported. Optional model,
resume and MCP arguments use the adapter's configured placeholders. Crew-format
JSON events include text, delta, status, session, error, done and usage.
Capability declarations must match the executable's actual behavior.

API entries use `kind = "api"`, `endpoint`, an explicit `model`, `auth = "api_key"`,
and `api_key_env`. The endpoint implements compatible Chat Completions tool calls.
HTTPS is required except localhost. Redirects and credential-bearing URLs are
rejected. Available tools read files and selected skill support files, write
permitted repository files, run pinned checks, and delegate bounded subtasks.
There is no arbitrary shell tool. API streaming and native resume are not yet
supported; shared context is supplied on subsequent turns.

Protocol references:
- https://developers.openai.com/api/docs/guides/function-calling/
- https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create

API verification used local fake endpoints, not paid live model calls. Gemini's
built-in subscription adapter remains disabled pending contract verification.
Custom adapters can be registered independently. No silent provider fallback
occurs when an explicit provider is unavailable.
