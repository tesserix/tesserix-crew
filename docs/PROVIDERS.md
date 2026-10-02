# Provider plugins

Crew supports subscription CLI adapters and direct API-key providers. Add provider
entries to global or repository configuration; lifecycle runs pin their registry.
Changing a provider affects new runs. Credentials come from native CLI login or a
named environment variable, never command arguments or configuration values.

CLI entries declare `kind = "cli"`, an argument-array `command`, `format`
(`claude`, `codex`, `agy`, `grok`, `crew`, or `text`), `auth`, `capabilities`, and explicit
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

## Antigravity CLI (`agy`)

Crew includes a subscription adapter for the installed `agy` CLI:

```sh
crew --agent agy
```

Switch an existing ordinary session with `/agent agy`. Optional `--model` selects
an identifier listed by `agy models`. The adapter uses stream-json events,
`--sandbox --mode plan`, and `--conversation` for native resume. The final result
is journaled once; permission-denied actions fail the Crew turn even when AGY
reports SUCCESS. Existing native permission settings still apply.

The verified scope is read-only planning, review and content generation. Write
stages and Crew MCP delegation are not enabled pending further contract checks.
This does not automatically change pinned lifecycles or existing routing rules.
Live checks covered response streaming, conversation resume and a denied file
write in a temporary directory. Native mode flags are not an additional Crew
filesystem security boundary.

`agy` is a separate executable/provider from Gemini CLI. The official Gemini CLI
README still provides installation documentation and active CI; no deprecation
notice was found there. Crew's Gemini adapter remains disabled because its own
contract has not been verified.

## Grok Build CLI (`grok`)

```sh
crew --agent grok
```

The adapter was checked against Grok Build 1.0.46. It uses single-turn JSON,
plan permissions and explicit Write/Edit/Bash denials. It exposes read-only
content/planning support and optional model selection. Native resume, streaming,
write stages and Crew delegation are not yet verified or enabled. Shared Crew
context accompanies each turn. Incomplete or malformed final responses fail.
Native login, permission rules and billing apply; CLI availability does not
establish that every model call is covered by a subscription.

AGY and Grok single-turn prompts are passed as process arguments because of their
CLI contracts. They are not shell commands, but local process inspection can see
these prompts. Avoid including credentials in task/context content.
