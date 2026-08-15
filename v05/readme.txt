charset,UTF-8
CharacterGPT Prototype v0.5.30 - Emotional Persistence Tuning

This online-update channel tracks the current CharacterGPT v0.5.x text/config layer.

Current text/config feature baseline:
- v0.5.30 Emotional Persistence Tuning

Current binary feature baseline:
- CharacterGPTBridge.exe: v0.5.29 Local Emotional Context
- CharacterGPTRuntime.exe: v0.5.28 Expression Scale Fix

v0.5.30 keeps the existing local emotional-state model but makes ordinary dialogue carry over more perceptibly:
- dialogue_weight: 0.65
- neutral_threshold: 0.05
- half_life_seconds: 300
- physical_weight remains 1.0

Local short-term emotional state is stored under ghost/master/profile/emotional_state.json and is not distributed by NAR or network updates.

Online-update payload includes:
- ghost/master/descript.txt
- Satori dictionaries and configuration
- ghost/master/config/*
- shell/master/surfaces.txt
- shell/master/surfacetable.txt
- readme.txt

Binary executables and user-local character/profile data are not routine network-update payloads.
