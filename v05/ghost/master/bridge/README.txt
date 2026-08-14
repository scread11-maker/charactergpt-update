CharacterGPT Runtime v0.5.20

v0.5.20 keeps the installed v0.5.19 native Runtime core and adds an external character-loading layer without replacing a running executable during SSP network update.

Editable character profile
- User files live under ghost/master/profile/character/.
- character.md defines identity, personality, relationship, speech style, preferences, and behavior.
- appearance.md defines visible/body/item/companion details.
- manifest.json selects the two document filenames.
- The files are read again for every LLM request. Saving an edit affects the next request without restarting SSP or the Runtime.
- profile/character is persistent user data and is deliberately absent from updates2.dau.
- Missing user files are seeded from ghost/master/character_defaults/.

v0.5.20 bootstrap
- CharacterGPTBridge_v0520.vbs launches the loader invisibly through Windows PowerShell.
- CharacterGPTBridge_v0520.ps1 creates CharacterGPTBridge_core_v0520.exe locally by copying the installed v0.5.19 CharacterGPTBridge.exe and applying two fixed-length byte patches: the local proxy endpoint and the displayed version number.
- The original CharacterGPTBridge.exe is not modified.
- The loader listens only on 127.0.0.1:8767, injects the local character profile into the OpenAI Responses API `instructions`, and forwards the request to https://api.openai.com/v1/responses.
- Authorization headers are kept only in memory for forwarding and are not written to the loader log. The existing Runtime core continues to own the API key and Windows DPAPI storage.

Existing v0.5.19 behavior retained
- WebSocket control/event server on 127.0.0.1:8766
- Unicode native chat window and UTF-8 SSTP output
- Touch gesture aggregation and local interaction dictionaries
- Recent-event context and persistent state
- Balloon duration and 15-second post-close expression restoration
- v0.5.19 light_touch / Hair / Book semantic calibration

Requirements
- Windows PowerShell is used only as the v0.5.20 loader/proxy host. The existing native Runtime remains responsible for the CharacterGPT UI and event logic.
