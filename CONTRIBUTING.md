# Contributing

Crew is early in development. Discuss substantial changes in an issue first.

Use Go 1.24 or newer. Run `gofmt`, `make test`, and `make check`. Include meaningful
tests for session persistence, routing, process lifecycle, and provider contracts.
Keep provider-specific behavior in adapters and platform integrations optional.
Do not commit subscription credentials or unsanitized real-agent recordings.

Contributions are licensed under Apache 2.0. Sign off commits (`git commit -s`)
to certify the Developer Certificate of Origin: https://developercertificate.org/.
