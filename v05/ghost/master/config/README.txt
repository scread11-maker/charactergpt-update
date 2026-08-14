CharacterGPT v0.5.29 - Local Interaction + Emotional Context

Files
- interaction_rules.json: author/default interpretation rules.
- reaction_examples.jsonl: few-shot reaction references. One JSON object per line.
- reaction_style.json: reaction/context preferences.
- runtime_context_rules.json: live SSP/Shell and Bridge context semantics.
- emotional_state_rules.json: short-term local affect decay and reaction-to-state weights.

User overrides
Create same-named interaction/reaction files under:
  ghost/master/profile/config/
Runtime reloads interaction/reaction config before each LLM prompt. A user rule/example with the same id replaces the built-in one. Set "enabled": false to disable an inherited rule/example.

Physical gesture grammar
The Runtime resolves physical facts before asking the LLM.

Normal character/Owl collisions
- single click -> light_touch (輕碰／輕度摸一下)
- double click -> heavy_tap (重敲)
- movement average speed < 170 px/s -> gentle_stroke (輕撫)
- movement otherwise -> stroke (撫摸)
- movement >= 300 px/s AND at least 2 strong direction reversals -> rough_rub (亂揉)

Bust is a special interaction target
- single click -> light_touch: one brief gentle touch
- double click -> grab: one brief grab/squeeze of the character's chest; NOT heavy_tap
- movement -> gentle_stroke / stroke / rough_rub using the same movement detector

Book is a special interaction object
- single click -> look_at: look toward the book, NO physical contact
- double click -> grab: grab/hold the book
- movement -> gentle_stroke / stroke / rough_rub using the same movement detector

A fast one-way swipe is intentionally NOT rough_rub. It remains stroke unless repeated direction reversals are detected.

Physical target semantics
- Head = character head
- Hair = the character's long hair hanging behind her; not the scalp/top-of-head hair
- Bust = character chest
- Book = book held/read by the character
- Owl.Head = owl head
- Owl.Bust = owl body
- Owl.Wing = owl wing

Important separation
OBSERVED PHYSICAL EVENT is a Runtime-resolved fact. The LLM must not change which body part/object was involved and must not invent contact for Book look_at.
interaction_rules.json describes possible interpretation/reaction framing. Physical contact does not by itself prove motive, romance, sexuality, or relationship status.

Runtime memory vs emotional context
- Runtime RECENT EVENTS and PERSISTENT COUNTS answer what happened and how often.
- Bridge CURRENT EMOTIONAL CONTEXT answers what short-term affect remains from prior LLM reactions.
- Emotional context is stored at ghost/master/profile/emotional_state.json and is never part of Character or autobiographical Memory.
- It decays toward neutral over time instead of resetting after each balloon.
- Physical-event reactions contribute at full configured weight; ordinary dialogue contributes more weakly by default.
- The state is derived from the LLM's structured emotion output, so a touch is not hard-coded to mean affection, anger, embarrassment, etc.
- The state should influence tone, expectation, tolerance and interpretation, but should not be recited as labels, scores, counters or timestamps.
- A new event may strengthen, soften, redirect or resolve the prior state.
- emotional_state_rules.json is hot-read by Bridge for each request/update and can tune half-life and impulse weights online.

Memory-expression policy
- Recent events are private memory, not a narration script.
- Memory should usually change tone, emotion, expectation, tolerance, or interpretation without being spoken aloud.
- Previous events should be explicitly mentioned only when they materially determine the current situation (direct repetition, an immediately ignored warning/boundary, direct continuation, or an explicit user callback).
- For repetition, prefer implicit continuity such as 「還來？」 or a changed tone rather than reciting the event sequence or touch counts.
- Current physical actions also do not need to be restated in speech; the character may simply react.

v0.5.19 semantic calibration
- light_tap is accepted as a legacy alias, but new single-click events canonicalize to light_touch.
- Hair means the long hair hanging behind the character, not hair on top of the head.
- Book look_at means only a brief glance; interest/reading intent is not assumed.

v0.5.25 Bust double-click calibration
- Bust double click canonicalizes to grab.
- The preceding single-click candidate is suppressed by the Satori click arbiter when a double click completes.
- Runtime accepts the resulting grab event as an LLM-triggering physical interaction instead of dropping it.

v0.5.29 local emotional context
- Short-term affect channels: positive, shy, wary, annoyed, downcast.
- User-facing primary labels derived from those channels: neutral, pleased, cheerful, shy, wary, annoyed, upset, downcast.
- Default half-life: 240 seconds.
- This layer complements Runtime event/count memory; it does not replace or duplicate those counters.
