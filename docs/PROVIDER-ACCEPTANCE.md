# Provider acceptance

On 2026-10-15, a configuration-only external CLI adapter used the installed Claude
CLI and its existing subscription login. No core adapter changes were needed for
that provider. A first turn read a temporary fixture and received a custom pinned
skill. A Codex turn in the same Crew session received the shared journal and skill.
A third external-provider turn resumed the same native conversation ID and saw
the intervening Codex context. All three turns passed explicit content assertions.

Opt-in reproduction (real model calls, isolated temporary repository/data):

```sh
go build -o /private/tmp/crew-provider ./cmd/crew
python3 scripts/smoke_provider.py --crew /private/tmp/crew-provider
```

Automated provider tests also cover API-key authentication against a local fake
endpoint, actual read/write tool permission enforcement, bounded tool rounds,
cancellation, repository path restrictions and secret redaction in prompts,
journals and responses. CLI tests cover normalized events, native resume,
configuration precedence and unavailable prerequisites before task execution.

Auto-mode regression tests verify availability/capability selection, ordered task
rules, explicit provider/model pinning, and no retry on another provider after
execution starts. Routing decisions are saved and visible through `/status`.

Limitations: executable/key presence does not establish native login validity.
The external-provider run verified read-only tasks, shared context and resume;
it does not establish arbitrary third-party CLI compatibility. Direct API streaming
and native API resume remain explicitly unsupported capabilities. Gemini's built-in
adapter remains disabled pending contract verification.
