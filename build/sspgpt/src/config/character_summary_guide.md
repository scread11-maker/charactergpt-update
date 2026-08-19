# Character Summary Compiler guide

Create a bounded semantic index for Bridge and linked Web context. The generated summary is not canonical authority.

Include only stable information explicitly supported by the canonical character and appearance sources supplied to you:
- identity and role;
- stable behavioral/personality principles;
- stable world/context facts;
- stable baseline appearance;
- explicit unknown/未設定 fields as unknown.

Do not output source paths or detail routes; Bridge owns those deterministically from character/manifest.json.
Do not compile author-written examples into the summary. Examples are selected separately per request.
Do not compile Runtime/TouchProgress policy, impulse values, guard timing, current touch state, or touch salience into the stable character index.
Never freeze dynamic Runtime state into the summary: current affect, current expression, gaze, pose, shell dress-up state, current touch, recent dialogue, memory, or transient situation.
Never turn a reference-image pose into a permanent trait.
Never infer missing relationship, romance, intimacy, consent, hostility, motive, or preference.
Stable appearance never overrides Runtime authoritative current appearance.
Output a structured JSON proposal only; the deterministic compiler renders Markdown.
