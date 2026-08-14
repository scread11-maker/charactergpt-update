CharacterGPT Prototype v0.5.20

Core behavior
- SSP/Shell remains the body and native desktop character UI.
- Satori remains a thin event adapter.
- Unicode chat, touch semantics, recent-event context, DPAPI API-key storage, balloon lifetime, and expression restoration continue to use the v0.5.19 native Runtime core.

v0.5.20 character profile
- A local Character Loader Proxy injects character identity and appearance into every OpenAI Responses API request.
- On first start, editable files are created under ghost/master/profile/character/.
- character.md: identity, personality, relationship, speech style, preferences, behavior.
- appearance.md: physical appearance, body details, carried objects, companions.
- manifest.json: maps the two document filenames.
- Files are read again on every LLM request. Save the document and the next request uses the new content.
- profile/character is intentionally excluded from the network-update manifest so user edits persist.

Network-update transition
- v0.5.20 downloads only text scripts/defaults/bootstrap files.
- After the update completes, switch away from and back to the Ghost (or restart SSP).
- The v0.5.20 loader then makes a local versioned copy of the installed v0.5.19 Runtime and patches only its OpenAI endpoint plus displayed version; the original executable is untouched.

API key
The native Runtime still manages the API key and Windows DPAPI protection. Never paste an API key into ordinary chat.

Persistent data
ghost/master/profile/
Loader diagnostics
ghost/master/bridge/v0520_loader.log
