# sspgpt Update Channel

SSP network-update repository for sspgpt / 伺的なGPT.

Current stable online-update channel: **v071fix10/**  
Current stable baseline: **SSPGPT v0.7.1 GPU1 fix10**  
Installed Ghost directory: **sspgpt_proto_v07**

## v0.7.1 fix10 update policy

The `v071fix10/` channel is intentionally conservative. SSP native network update is limited to bootstrap/presentation-safe text and does **not** replace executable/runtime-owned or user-owned state.

Excluded from ordinary network updates:
- Runtime / Bridge / MemoryService / TouchProgress / Plug executables
- local model assets and inference runtimes
- `ghost/master/config/*` editable rules/guides
- `ghost/master/character/*` canonical character sources and examples
- `ghost/master/profile/*` generated profile, state, settings and secrets
- memory state, credentials/API keys and logs

Core architecture/config-layout upgrades continue to be distributed as full NAR packages.

Superseded update channels are removed from the active tree. Their history remains recoverable from Git history; they are not supported update targets.
