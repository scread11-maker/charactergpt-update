# sspgpt Update Channel

SSP network-update repository for sspgpt / 伺的なGPT.

Current stable online-update channel: **v062/**  
Current v0.62 online-update baseline: **v0.62-alpha2c-u1**  
Current project/work-folder name: **sspgpt_proto_v062**

Older `v061/` remains available as the v0.6 compatibility channel. Historical/architecture material remains under `v06/`.

## v0.62 update policy

The `v062/` channel is intentionally conservative. SSP network update is for safe Satori/text-layer maintenance and does **not** update executable/runtime-owned or user-owned state.

Excluded from ordinary network updates:
- executable binaries under `ghost/master/bridge/*`
- `ghost/master/satori.dll`
- `ghost/master/config/*` local editable settings/guides
- `ghost/master/character/*`
- `ghost/master/profile/*`
- memory state and credentials/API keys

Core Runtime / Bridge / MemoryService / TouchProgress upgrades continue to be distributed as NAR packages.
