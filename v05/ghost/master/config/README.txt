CharacterGPT v0.5.4 - Local Interaction Dictionary

Files
- interaction_rules.json: author/default interpretation rules.
- reaction_examples.jsonl: few-shot reaction references. One JSON object per line.
- reaction_style.json: reaction/context preferences.

User overrides
Create same-named files under:
  ghost/master/profile/config/
Runtime reloads config before each LLM prompt. A user rule/example with the same id replaces the built-in one. Set "enabled": false to disable an inherited rule/example.

v0.5.4 physical gesture grammar
The Runtime resolves physical facts before asking the LLM.

Normal character/Owl collisions
- single click -> light_touch (輕碰／輕度摸一下)
- double click -> heavy_tap (重敲)
- movement average speed < 170 px/s -> gentle_stroke (輕撫)
- movement otherwise -> stroke (撫摸)
- movement >= 300 px/s AND at least 2 strong direction reversals -> rough_rub (亂揉)

A fast one-way swipe is intentionally NOT rough_rub. It remains stroke unless repeated direction reversals are detected.

Book is a special interaction object
- single click -> look_at: look toward the book, NO physical contact
- double click -> grab: grab/hold the book
- movement -> gentle_stroke / stroke / rough_rub using the same movement detector

Physical target semantics
- Head = character head
- Hair = character hair
- Bust = character chest
- Book = book held/read by the character
- Owl.Head = owl head
- Owl.Bust = owl body
- Owl.Wing = owl wing

Important separation
OBSERVED PHYSICAL EVENT is a Runtime-resolved fact. The LLM must not change which body part/object was involved and must not invent contact for Book look_at.
interaction_rules.json describes possible interpretation/reaction framing. Physical contact does not by itself prove motive, romance, sexuality, or relationship status.

Backward compatibility
Old v0.5.3 events named tap/poke/stroke/rub are normalized in recent context to the v0.5.4 gesture grammar. Existing state.json counters are migrated into the new detailed counters without deleting the legacy fields.

v0.5.6 memory-expression policy
- Recent events are private memory, not a narration script.
- Memory should usually change tone, emotion, expectation, tolerance, or interpretation without being spoken aloud.
- Previous events should be explicitly mentioned only when they materially determine the current situation (direct repetition, an immediately ignored warning/boundary, direct continuation, or an explicit user callback).
- For repetition, prefer implicit continuity such as 「還來？」 or a changed tone rather than reciting the event sequence or touch counts.
- Current physical actions also do not need to be restated in speech; the character may simply react.


v0.5.19 semantic calibration
- light_tap is accepted as a legacy alias, but new single-click events canonicalize to light_touch.
- Hair means the long hair hanging behind the character, not hair on top of the head.
- Book look_at means only a brief glance; interest/reading intent is not assumed.


v0.5.20 character profile
- Runtime character identity is no longer required to live inside Bridge code.
- On the first LLM request, editable files are created under ghost/master/profile/character/.
- character.md: identity, personality, relationship, speech style, preferences, behavior.
- appearance.md: appearance, body details, carried objects, companion details.
- manifest.json: file mapping only.
- These files are reloaded on every LLM request and are intentionally outside the network-update manifest after creation.
