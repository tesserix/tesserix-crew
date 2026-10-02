# macOS release signing

The release workflow supports Developer ID signing and Apple notarization before
Homebrew checksums are generated. Configure these GitHub Actions secrets:

| Secret | Value |
| --- | --- |
| `MACOS_CERT_P12_BASE64` | Base64-encoded Developer ID Application certificate and private key exported as an encrypted P12 |
| `MACOS_CERT_PASSWORD` | P12 password |
| `MACOS_SIGNING_IDENTITY` | Full Developer ID Application signing identity |
| `APPLE_NOTARY_KEY_BASE64` | Base64-encoded notarization API private key (P8) |
| `APPLE_NOTARY_KEY_ID` | API key ID |
| `APPLE_NOTARY_ISSUER_ID` | API issuer ID |

Use GitHub's secret configuration; never commit credentials or paste them into
issues/comments. Required certificate/account setup is owned by the repository
maintainer. Credentials are imported into a temporary keychain and removed after
signing. Signing failure prevents release publication when the credential set is
enabled. If credentials are absent, signing jobs are skipped and the workflow
explicitly reports that binaries retain Go ad-hoc signatures.

Each macOS archive is unpacked, its binary is signed with a timestamp and hardened
runtime, and the signed binary is submitted to Apple's notarization service.
The final archive contains that same binary. Standalone CLI binaries do not
support stapled tickets; Gatekeeper retrieves the notarization ticket online.
Homebrew hashes are calculated from the final archives after signing, and already
published archives are never replaced.

Signing support is prepared but has not been exercised with a real Developer ID
credential set. Issue #9 remains open until a signed release is verified through
fresh Homebrew installation and first launch with quarantine intact. Existing
unsigned releases keep their documented first-launch behavior.
