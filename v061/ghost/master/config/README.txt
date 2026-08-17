sspgpt interaction/context configuration

Files
- interaction_rules.json: author/default interpretation rules.
- reaction_examples.jsonl: few-shot reaction references. One JSON object per line.
- reaction_style.json: reaction/context preferences.
- runtime_context_rules.json: live SSP/Shell and Bridge context semantics.
- emotional_state_rules.json: short-term local affect decay and reaction-to-state weights.
- touch_memory_rules.json: independent per-target short-term physical touch salience; does not modify emotional state.

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

Physical truth and interpretation
OBSERVED PHYSICAL EVENT is a Runtime-resolved fact. The LLM must not change which body part/object was involved and must not invent contact for Book look_at.
interaction_rules.json describes possible interpretation/reaction framing. Physical contact does not by itself prove motive, romance, sexuality, or relationship status.
light_tap is accepted as a legacy alias, but new single-click events canonicalize to light_touch.
Hair means the long hair hanging behind the character, not hair on top of the head.
Book look_at means only a brief glance; interest/reading intent is not assumed.
Bust double click canonicalizes to grab, and its preceding single-click candidate is suppressed when a double click completes.

Runtime memory vs emotional context
- Runtime RECENT EVENTS and PERSISTENT COUNTS answer what happened and how often.
- Bridge CURRENT EMOTIONAL CONTEXT answers what short-term affect remains from prior LLM reactions.
- Emotional context is stored at ghost/master/profile/emotional_state.json and is never part of Character or autobiographical Memory.
- Short-term affect channels: positive, shy, wary, annoyed, downcast.
- User-facing primary labels: neutral, pleased, cheerful, shy, wary, annoyed, upset, downcast.
- Default half-life: 300 seconds.
- dialogue_weight: 0.65.
- neutral_threshold: 0.05.
- physical_weight: 1.0.
- It decays toward neutral over time instead of resetting after each balloon.
- The state is derived from the LLM's structured emotion output, so a touch is not hard-coded to mean affection, anger, embarrassment, etc.
- The state should influence tone, expectation, tolerance and interpretation, but should not be recited as labels, scores, counters or timestamps.
- A new event may strengthen, soften, redirect or resolve the prior state.
- emotional_state_rules.json is hot-read by Bridge for each request/update.

Memory-expression policy
- Recent events are private memory, not a narration script.
- Memory should usually change tone, emotion, expectation, tolerance, or interpretation without being spoken aloud.
- Previous events should be explicitly mentioned only when they materially determine the current situation.
- For repetition, prefer implicit continuity such as 「還來？」 or a changed tone rather than reciting the event sequence or touch counts.
- Current physical actions also do not need to be restated in speech; the character may simply react.

Touch salience
- profile/touch_state.json remains independent from profile/emotional_state.json.
- Touch strength uses an independent 300-second half-life by default.
- New gesture impulses accumulate with diminishing gain; strength is fuzzy physical salience, not an exact count or emotion intensity.
- Default impulses: light_touch 220, gentle_stroke 260, stroke 300, heavy_tap 340, resting_touch 120, rough_rub 450, grab 500.
- resting_touch adds only once when resting is established; release adds nothing.
- Bridge receives only compact active target/strength/state lines, ordered older -> newer using a local-only timestamp.
- touch_memory_rules.json is independent and does not modify emotional_state_rules.json.

Stable/dynamic profile separation
- character/summary.md is stable bounded profile only. It must never contain the RecentPhysicalInteractions dynamic block.
- character/t.md is a hidden dynamic transport file owned by TouchProgress. The normal manifest loads both t.md and summary.md, preserving live salience without contaminating stable profile generation.
- details_.json remains character.md + appearance.md for request-time full-detail routing.
- Touch model/impulse hierarchy remains stable generated policy in summary.md; only decayed per-target salience is dynamic.
- bridge/prompt_audit.log is written after successful ProfileRefresh. bridge/prompt_audit_report.txt is generated on normal Ghost shutdown and may also be regenerated manually with CharacterGPTPromptAudit.ps1.

Release presentation
- A release immediately after a visible reaction (2000 ms window) may be locally presentation-deferred.
- Presentation defer never preserves contact: release still ends the physical lifecycle.
- Current-state truth remains authoritative over salience/history.
