CharacterGPT v0.5 online update channel

Baseline: v0.5.24 system presentation hotfix
Transport URL: raw.githubusercontent.com

Ordinary update payload is intentionally text/config only.
Included: descript, Satori dictionaries, satori_bootconf, satori_conf, config/*.
Excluded: bridge/*, character/*, profile/*, satori.dll, shell/*.

System balloon behavior:
- Normal CharacterGPT system notifications follow the configured dialogue duration.
- Background dress-up synchronization is executed immediately in the same connection script; no delayed timerraise is used to interrupt visible balloons.
- Failure notifications remain open for readability.

Dynamic-state interpretation policy is in config/runtime_context_rules.json and can be adjusted online without replacing Bridge binaries.
