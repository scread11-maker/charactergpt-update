# CharacterGPT Update Channel

SSP network-update repository for CharacterGPT Prototype.

Current channel: `v05/`

Published through GitHub Pages at:
`https://scread11-maker.github.io/charactergpt-update/v05/`

The ordinary update channel intentionally contains only lightweight, text-based Ghost data such as `descript.txt`, touch/chat adapters, and the local interaction configuration (`interaction_rules.json`, `reaction_examples.jsonl`, `reaction_style.json`).

Bootstrap/runtime components are intentionally excluded from ordinary network updates:
- `ghost/master/bridge/CharacterGPTBridge.exe`
- `ghost/master/dic00_system.txt`
- `ghost/master/satori.dll`
- `shell/`
- `ghost/master/profile/`
- logs, credentials, saved state, and user overrides

Those components continue to use NAR overlay releases when necessary.
