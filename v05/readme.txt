CharacterGPT Prototype v0.5

This prototype focuses on interaction runtime rather than character lore.

Core behavior
- SSP/Shell remains the body and native desktop character UI.
- Satori is a thin ASCII event adapter only.
- Left-click outside known touch collisions opens the Runtime's Unicode chat window.
- Chinese/Japanese/other Unicode chat text does not pass through the legacy SHIORI text path.
- Responses return to SSP through UTF-8 SSTP.
- Touch gestures are aggregated into tap/poke/stroke/rub semantics.
- Reactions use recent conversation, recent touch events, previous reactions, current local time, and elapsed time.
- Local state and event history are persisted under ghost/master/profile/.
- No fixed character background is defined yet.

API key
Use the "設定 API Key" choice shown when the Runtime connects. The key is encrypted with Windows DPAPI.
Never paste an API key into ordinary chat. v0.5 blocks API-key-like chat input and redacts it from bridge.log.

Diagnostics
ghost/master/bridge/bridge.log
Persistent data
ghost/master/profile/state.json
ghost/master/profile/events.jsonl
ghost/master/profile/credentials.dat

Network update
Normal network updates intentionally do not replace the Bridge executable, satori.dll, shell, or dic00_system.txt bootstrap.
