SSPGPT v0.7.1 editable configuration

Policy files in this directory are hot-editable unless a file says otherwise.

- interaction_rules.json: physical/target semantic guidance. Runtime physical truth remains authoritative. Conditional rules are evaluated against bounded recent physical/dialogue context before they are injected.
- reaction_style.json: response style, maximum selected author examples, and the actual recent-dialogue window injected into foreground prompts.
- physical_response_rules.json: Runtime presentation/impulse guard policy. This is the single numeric authority for lower-impulse/resting timing.
- touch_memory_rules.json: TouchProgress salience/impulse policy.
- emotional_state_rules.json: Runtime affect decay/impulses.
- runtime_context_rules.json: prose-level context interpretation policy; it intentionally does not duplicate numeric Runtime guard values.
- memory_evaluation_guide.md: local Memory Brain semantic evaluation guidance.
- memory_retention_rules.json: semantic retention weights, thresholds, modifiers (including directive kind/id boosts), observation evidence, and Hot Memory size only.
- memory_retrieval_rules.json: the sole Recall effort-preset policy. light/medium/deep control merged candidate-pool breadth, shared recall deadline, and compact foreground context token budget; medium defaults to 300 candidates / ~1024 tokens. unbounded is a separate raw chronological Replay strategy with no embedding/rerank/Memory-Brain work and a configurable replay safety ceiling.
- directive_rules.json: hot-editable cognition directive registry. Runtime recognizes only registered directive/alias syntax; Bridge resolves registered document or semantic meaning. Documents are restricted to the character/ tree and user input cannot supply arbitrary filesystem paths.
- character_summary_guide.md: local Character Summary compiler guidance.
- character_summary_rules.json: one shared bounded-output contract used by both MemoryService compiler and Bridge renderer/validator.
- local_models.json: local model/runner definitions.
- autonomous_rules.json, linked_chat_rules.json, input_ui.json, presentation_map.json, bridge_settings.json, audit_rules.json: their respective runtime policies. Plug activation is always explicit; linked_chat_rules intentionally has no enabled-by-default switch.

Author-written dialogue/interaction examples are character material, not machine policy. They live under character/examples/ and are declared by character/manifest.json. Legacy config/reaction_examples.jsonl is migrated once and then removed.

- input_ui.json:ssp_backlog_mirror controls whether accepted Runtime input is mirrored to SSP presentation/history. The left-click menu exposes this as a simple yes/no option; Raw Replay and cognition are independent.
