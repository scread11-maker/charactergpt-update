# CharacterGPT Update Channel

SSP network-update repository for CharacterGPT Prototype.

Current channel: `v05/`

Published through GitHub Pages at:
`https://scread11-maker.github.io/charactergpt-update/v05/`

## v0.5.20

v0.5.20 adds an external, hot-reloadable local character profile while preserving the installed v0.5.19 Runtime core.

Editable user files are created on first v0.5.20 start under:
- `ghost/master/profile/character/character.md`
- `ghost/master/profile/character/appearance.md`
- `ghost/master/profile/character/manifest.json`

The update channel ships only templates under `ghost/master/character_defaults/`; `profile/character/` is intentionally excluded from `updates2.dau`, so future network updates do not overwrite user character settings.

For the v0.5.20 bootstrap transition, the update ships a text-based VBS/PowerShell loader. It creates a versioned copy of the already installed v0.5.19 Runtime core locally after the old process has stopped, avoiding any attempt to replace the executable while SSP is updating it. The original Runtime executable is left unchanged.

Persistent user data under `ghost/master/profile/`, credentials, `satori.dll`, and the shell remain outside ordinary network updates.
