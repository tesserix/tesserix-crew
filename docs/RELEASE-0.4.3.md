# Tesserix Crew v0.4.3

- Ctrl+S opens session search with the current session marked.
- Ctrl+N or `/new` starts a fresh session in the current project while keeping provider, model, and permission preferences.
- `/retry` restores the previous chat task for editing without executing it automatically, including after resuming a saved session.
- Esc or Ctrl+C cancels an active chat task and keeps the session open.
- Progress distinguishes preparation, provider startup, waiting, streaming, and cancellation; elapsed time and extended silence remain visible.

Mac archives are Developer ID signed and Apple notarized. Upgrade with `brew upgrade --cask tesserix/tap/crew`.
