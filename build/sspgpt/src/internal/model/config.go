package model

type EmotionRules struct {
	FormatVersion    int                           `json:"format_version"`
	Enabled          bool                          `json:"enabled"`
	HalfLifeSeconds  float64                       `json:"half_life_seconds"`
	NeutralThreshold float64                       `json:"neutral_threshold"`
	DialogueWeight   float64                       `json:"dialogue_weight"`
	PhysicalWeight   float64                       `json:"physical_weight"`
	Impulses         map[string]map[string]float64 `json:"impulses"`
}

type TouchMemoryRules struct {
	FormatVersion    int                `json:"format_version"`
	Enabled          bool               `json:"enabled"`
	HalfLifeSeconds  float64            `json:"half_life_seconds"`
	ForgetThreshold  float64            `json:"forget_threshold"`
	DiminishingScale float64            `json:"diminishing_scale"`
	Impulses         map[string]float64 `json:"impulses"`
}

type MatchConditions struct {
	RepeatWithinSeconds     int `json:"repeat_within_seconds,omitempty"`
	RepeatCountGTE          int `json:"repeat_count_gte,omitempty"`
	RecentChatWithinSeconds int `json:"recent_chat_within_seconds,omitempty"`
}

type InteractionRule struct {
	ID         string          `json:"id"`
	Target     string          `json:"target"`
	Gesture    string          `json:"gesture"`
	Meaning    []string        `json:"meaning"`
	Intensity  float64         `json:"intensity,omitempty"`
	Conditions MatchConditions `json:"conditions,omitempty"`
	Notes      []string        `json:"notes,omitempty"`
}

type InteractionRules struct {
	Rules []InteractionRule `json:"rules"`
}

type CharacterExampleMatch struct {
	RequestClass []string `json:"request_class,omitempty"`
	Target       []string `json:"target,omitempty"`
	Gesture      []string `json:"gesture,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	TextHints    []string `json:"text_hints,omitempty"`
	MatchConditions
}

type CharacterExample struct {
	ID        string                `json:"id"`
	Kind      string                `json:"kind"` // dialogue|interaction
	Match     CharacterExampleMatch `json:"match"`
	Situation string                `json:"situation,omitempty"`
	User      string                `json:"user,omitempty"`
	Response  string                `json:"response"`
	Emotion   string                `json:"emotion,omitempty"`
	Notes     []string              `json:"notes,omitempty"`
}

// LegacyReactionExample exists only for one-way migration from the pre-fix5
// config/reaction_examples.jsonl channel into character/examples/interaction.jsonl.
type LegacyReactionExample struct {
	ID         string          `json:"id"`
	Target     string          `json:"target"`
	Gesture    string          `json:"gesture"`
	Situation  string          `json:"situation"`
	Reaction   string          `json:"reaction"`
	Emotion    string          `json:"emotion"`
	Notes      []string        `json:"notes,omitempty"`
	Conditions MatchConditions `json:"conditions,omitempty"`
}

type CharacterSummaryRules struct {
	FormatVersion           int      `json:"format_version"`
	MaxItemsPerSection      int      `json:"max_items_per_section"`
	MaxItemChars            int      `json:"max_item_chars"`
	RetryMaxItemsPerSection int      `json:"retry_max_items_per_section"`
	RetryMaxItemChars       int      `json:"retry_max_item_chars"`
	ForbiddenDynamicTerms   []string `json:"forbidden_dynamic_terms"`
}

type ReactionStyle struct {
	PreferredLength         string `json:"preferred_length"`
	MaxSentences            int    `json:"max_sentences"`
	MaxExamples             int    `json:"max_examples"`
	RecentContextSeconds    int    `json:"recent_context_seconds"`
	AvoidRepetition         bool   `json:"avoid_repetition"`
	PreferContextContinuity bool   `json:"prefer_context_continuity"`
	AllowSilentReaction     bool   `json:"allow_silent_reaction"`
}
