# Homebrew distribution

Public install after the first release and tap formula are published:

```sh
brew install tesserix/tap/crew
crew doctor
crew
```

Upgrade with `brew update && brew upgrade crew`. Homebrew installs Crew, not the
provider CLIs; Claude Code, Codex and Gemini need their own installation/login.

## Release

The tag workflow builds CGO-free macOS/Linux binaries for arm64 and amd64, runs
checks, packages LICENSE/NOTICE, publishes checksums and immutable release assets,
and renders `crew.rb` using the exact asset hashes.

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
