# Homebrew distribution

Public install after the first release and tap formula are published:

```sh
brew install --cask tesserix/tap/crew  # macOS: installs the prebuilt binary directly
# Linux: brew install tesserix/tap/crew
crew doctor
crew
```

Upgrade with `brew update && brew upgrade --cask crew` on macOS, or
`brew update && brew upgrade crew` on Linux. Homebrew installs Crew, not the
provider CLIs; Claude Code, Codex and Gemini need their own installation/login.

## macOS first launch for 0.1.0

The initial binary has a valid ad-hoc Go signature, but is not Developer ID signed
or notarized. macOS can show a Gatekeeper warning on first launch. Inspect the
source/release checksums before choosing to trust this release. For a trusted fresh
installation, Homebrew supports the explicit per-install option:

```sh
brew install --cask --no-quarantine tesserix/tap/crew
```

For an already installed, verified binary, remove only its quarantine attribute:

```sh
xattr -d com.apple.quarantine "$(brew --prefix)/Caskroom/crew/0.1.0/crew"
crew version
```

This does not change system-wide Gatekeeper settings. Signed/notarized releases
are tracked in [issue #9](https://github.com/tesserix/tesserix-crew/issues/9).

## Release

The tag workflow builds CGO-free macOS/Linux binaries for arm64 and amd64, runs
checks, packages LICENSE/NOTICE, publishes checksums and immutable release assets,
and renders the formula and macOS cask using the exact asset hashes. The macOS
cask avoids source-formula Xcode/compiler checks for this prebuilt executable.

1. Review, commit and push the source to `tesserix/tesserix-crew`.
2. For automatic tap updates, configure `HOMEBREW_TAP_GITHUB_TOKEN` in Crew's GitHub
   Actions secrets. Scope its repository contents access to `tesserix/homebrew-tap`.
   It is separate from the workflow's Crew-scoped `GITHUB_TOKEN`.
3. Push a stable release tag, such as `v0.1.0`.
4. Verify release assets and install-test the formula.

The workflow can publish Crew's release without the tap token. In that case, download
the attached `crew.rb`, copy it to `Formula/crew.rb` in `tesserix/homebrew-tap`, and
publish that tap change manually. Do not replace an existing version's archives.

For local packaging, generate the formula from the four archives:

```sh
python3 scripts/render_formula.py --version 0.1.0 --assets dist --output dist/crew.rb
ruby -c dist/crew.rb
```

Validate the formula with `brew style`, `brew audit`, `brew install`, and `brew test`
once assets are reachable. A generated formula pointing to unpublished assets is
not yet installable via the public tap.
