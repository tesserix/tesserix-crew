# Tesserix Crew v0.4.1

macOS releases are now signed with Tesserix Pty Ltd’s Apple Developer ID and submitted to Apple for notarization. Publication requires Apple to accept both Mac builds. Future releases fail if signing credentials are missing.

Install or upgrade on macOS with `brew install --cask tesserix/tap/crew` or `brew upgrade --cask tesserix/tap/crew`. Gatekeeper retrieves the notarization ticket online for the standalone CLI executable.

This patch also fixes release-note selection and adds regression coverage for rejected notarization submissions. Features and provider latency behavior remain as in v0.4.0.
