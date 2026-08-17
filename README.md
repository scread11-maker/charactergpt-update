# sspgpt Update Channel

SSP network-update repository for sspgpt / 伺的なGPT.

Current stable online-update channel: **v061/**
Current stable baseline: **v0.6.0-alpha5-r6-test31aa**
Current project/work-folder name: **sspgpt_proto_v06**

The retired `v05/` channel has been removed. Historical/architecture material remains under `v06/`.

## Stable update policy

The `v061/` channel distributes only the text/configuration/Satori/surface-definition layer required by SSP network update.

The following remain excluded from ordinary network updates:
- executable binaries
- `ghost/master/bridge/*`
- `ghost/master/character/*`
- `ghost/master/profile/*`
- credentials/API keys
- `ghost/master/satori.dll`

Legacy binary/event names containing `CharacterGPT` are retained only where changing them would break runtime contracts. User-local character/profile state is never overwritten by this channel.
