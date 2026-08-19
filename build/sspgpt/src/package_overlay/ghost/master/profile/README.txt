Profile = persistent local persona-instance domain.

- generated/: compiled character artifacts only.
- state/: Runtime/TouchProgress persistent current-state files (created at runtime).
- settings/: user/runtime preferences (created at runtime).
- secrets/: credentials only (created at runtime; never prompt/index/export material).

Canonical editable character sources remain under ../character/. Long-term history remains under ../memory/.
