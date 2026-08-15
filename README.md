# CharacterGPT Update Channel

SSP network-update repository for CharacterGPT Prototype.

Current stable text/config baseline: **v0.5.30 - Emotional Persistence Tuning**
Current stable binary baseline: **Bridge v0.5.29 / Runtime v0.5.28**

Experimental architecture work for **v0.6.0-alpha - Desktop Embodiment** lives under `v06/` and does not replace the stable `v05/` update channel.

Published through GitHub Pages at:
`https://scread11-maker.github.io/charactergpt-update/v05/`

## Stable update policy

The `v05/` channel distributes only the safe text/configuration layer:
- `ghost/master/descript.txt`
- `ghost/master/dic00_system.txt`
- `ghost/master/dic01_chat.txt`
- `ghost/master/dic02_touch.txt`
- `ghost/master/satori_bootconf.txt`
- `ghost/master/config/*`

The following are intentionally excluded from ordinary SSP network updates:
- `ghost/master/bridge/*`
- `ghost/master/character/*`
- `ghost/master/profile/*`
- `ghost/master/satori.dll`
- `shell/*`

`character/` is the user's editable persistent character definition. `profile/` is runtime/user state and future memory data. Neither is overwritten by this channel.

Bridge/runtime binary changes continue to use NAR releases until a restart-safe binary updater is implemented.
