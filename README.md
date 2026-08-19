# sspgpt Update Channel

SSP network-update repository for sspgpt / 伺的なGPT.

Current stable online-update channel: **v07/**  
Current stable baseline: **SSPGPT v0.7.1 GPU1 fix13f**  
Installed Ghost directory: **sspgpt_proto_v07**

## v0.7 stable update policy

The `v07/` channel is the stable SSPGPT v0.7.x network-update endpoint. Its path is intentionally version-stable across fix releases.

SSP native network update is limited to bootstrap/presentation-safe text and does **not** replace executable/runtime-owned or user-owned state.

Excluded from ordinary network updates:
- Runtime / Bridge / MemoryService / TouchProgress / Plug executables
- local model assets and inference runtimes
- `ghost/master/config/*` editable rules/guides
- `ghost/master/character/*` canonical character sources and examples
- `ghost/master/profile/*` generated profile, state, settings and secrets
- memory state, credentials/API keys and logs

Core architecture/config-layout upgrades continue to be distributed as full NAR packages.

Current fix13f keeps sent-input mapping as a simple user-facing **是/否** option; `是` uses the established secondary-character (Owl) presentation path. Raw Replay remains independent and is not sourced from SSP backlog.

Superseded update channels are removed from the active tree. Their history remains recoverable from Git history; they are not supported update targets.
