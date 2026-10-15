# Tesserix Crew v0.4.6

Auto mode checks local prerequisites and task capabilities before starting. It
tries matching task rules, the configured default and then eligible alternatives,
and records the selection reason for `/status`. Explicit provider and model
choices remain pinned. Started tasks are never replayed on another provider.

Validation includes real external CLI login/resume/shared-context acceptance,
API tool/permission tests against a fake endpoint, and routing regression tests.
See [provider acceptance](PROVIDER-ACCEPTANCE.md) for the supported scope.
