package model

import "time"

const (
	RequestChat         = "chat"
	RequestPhysical     = "physical"
	RequestAppearance   = "appearance_change"
	RequestLinkedChat   = "linked_chat"
	RequestAutonomous   = "autonomous"
	RequestDeferred     = "deferred"
	RequestMemoryRecall = "memory_recall"
)

type PhysicalEvent struct {
	Type          string  `json:"type,omitempty"`
	Gesture       string  `json:"gesture,omitempty"`
	Target        string  `json:"target,omitempty"`
	CharacterID   string  `json:"character_id,omitempty"`
	Phase         string  `json:"phase,omitempty"`
	Contact       bool    `json:"contact"`
	Moving        bool    `json:"moving"`
	Resting       bool    `json:"resting"`
	Released      bool    `json:"released"`
	DurationMS    int64   `json:"duration_ms,omitempty"`
	Speed         float64 `json:"speed,omitempty"`
	Reversals     int     `json:"reversals,omitempty"`
	Intensity     float64 `json:"intensity,omitempty"`
	SessionID     string  `json:"session_id,omitempty"`
	ObservedAt    string  `json:"observed_at,omitempty"`
	Authoritative bool    `json:"authoritative,omitempty"`
}

type AffectState struct {
	FormatVersion int                `json:"format_version"`
	UpdatedAt     string             `json:"updated_at"`
	Revision      int64              `json:"revision"`
	Primary       string             `json:"primary"`
	Intensity     float64            `json:"intensity"`
	Channels      map[string]float64 `json:"channels"`
	LastReaction  string             `json:"last_reaction,omitempty"`
	LastSource    string             `json:"last_source,omitempty"`
	LastCause     string             `json:"last_cause,omitempty"`
}

type AppearanceState struct {
	UpdatedAt        string         `json:"updated_at,omitempty"`
	Source           string         `json:"source,omitempty"`
	ShellName        string         `json:"shell_name,omitempty"`
	ShellPath        string         `json:"shell_path,omitempty"`
	ShellKey         string         `json:"shell_key,omitempty"`
	SnapshotComplete bool           `json:"snapshot_complete"`
	Summary          string         `json:"summary,omitempty"`
	Dressup          map[string]any `json:"dressup,omitempty"`
	Raw              string         `json:"raw,omitempty"`
}

// AppearanceTransition describes an authoritative Runtime-owned change in
// embodiment.  It is semantic current-state context for cognition; filesystem
// paths and renderer implementation details remain outside this event.
type AppearanceTransition struct {
	Kind              string `json:"kind"`
	PreviousShellName string `json:"previous_shell_name,omitempty"`
	PreviousShellKey  string `json:"previous_shell_key,omitempty"`
	CurrentShellName  string `json:"current_shell_name,omitempty"`
	CurrentShellKey   string `json:"current_shell_key,omitempty"`
	ChangedAt         string `json:"changed_at,omitempty"`
}

type CurrentState struct {
	Physical   *PhysicalEvent  `json:"physical,omitempty"`
	Touch      map[string]any  `json:"touch,omitempty"`
	Affect     AffectState     `json:"affect"`
	Appearance AppearanceState `json:"appearance"`
	Situation  string          `json:"situation,omitempty"`
}

type DirectiveRef struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	InvokedAs string `json:"invoked_as,omitempty"`
	Argument  string `json:"argument,omitempty"`
}

type UserInput struct {
	Text        string        `json:"text,omitempty"`
	UserEmotion string        `json:"user_emotion,omitempty"`
	Directive   *DirectiveRef `json:"directive,omitempty"`
}

type DialogueTurn struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source,omitempty"`
	User      string `json:"user,omitempty"`
	Character string `json:"character,omitempty"`
}

type RequestPolicy struct {
	CheckAfterMS int64 `json:"check_after_ms,omitempty"`
	Cancellable  bool  `json:"cancellable"`
	Presentation bool  `json:"presentation"`
	Priority     int   `json:"priority,omitempty"`
	Secondary    bool  `json:"secondary,omitempty"`
}

type ContinuationRef struct {
	ContinuationID string `json:"continuation_id,omitempty"`
	ParentRequest  string `json:"parent_request_id,omitempty"`
	Mode           string `json:"mode,omitempty"`
	DueAt          string `json:"due_at,omitempty"`
	OriginalText   string `json:"original_text,omitempty"`
	UserEmotion    string `json:"user_emotion,omitempty"`
}

// SemanticMemory is the only long-term-recall unit in v0.7.1.
// Raw dialogue and raw sensor streams never enter the semantic index directly.
type SemanticMemory struct {
	ID                  string            `json:"memory_id"`
	Kind                string            `json:"kind"` // fact|observation|episode|commitment
	Text                string            `json:"text"`
	CreatedAt           string            `json:"created_at"`
	UpdatedAt           string            `json:"updated_at,omitempty"`
	Importance          float64           `json:"importance,omitempty"`
	Confidence          float64           `json:"confidence,omitempty"`
	EmotionalSalience   float64           `json:"emotional_salience,omitempty"`
	RetentionScore      float64           `json:"retention_score,omitempty"`
	Entities            []string          `json:"entities,omitempty"`
	SourceEpisodeIDs    []string          `json:"source_episode_ids,omitempty"`
	EvidenceCount       int               `json:"evidence_count,omitempty"`
	ValidFrom           string            `json:"valid_from,omitempty"`
	ValidUntil          string            `json:"valid_until,omitempty"`
	SupersededBy        string            `json:"superseded_by,omitempty"`
	EmbeddingModel      string            `json:"embedding_model,omitempty"`
	EmbeddingDimension  int               `json:"embedding_dimension,omitempty"`
	EmbeddingGeneration int               `json:"embedding_generation,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
}

type MemoryCapsule struct {
	Version        int64            `json:"version,omitempty"`
	GeneratedAt    string           `json:"generated_at,omitempty"`
	RecallMode     string           `json:"recall_mode,omitempty"` // semantic|replay
	Facts          []SemanticMemory `json:"facts,omitempty"`
	Observations   []SemanticMemory `json:"observations,omitempty"`
	Events         []SemanticMemory `json:"events,omitempty"`
	Commitments    []SemanticMemory `json:"commitments,omitempty"`
	Replay         []DialogueTurn   `json:"replay,omitempty"`
	TemporalNotes  []string         `json:"temporal_notes,omitempty"`
	Degraded       bool             `json:"degraded,omitempty"`
	DegradedReason string           `json:"degraded_reason,omitempty"`
}

type HotMemorySnapshot struct {
	Version   int64            `json:"version"`
	UpdatedAt string           `json:"updated_at"`
	Items     []SemanticMemory `json:"items,omitempty"`
}

type LinkedRef struct {
	SessionID      string `json:"session_id,omitempty"`
	ExternalTurnID string `json:"external_turn_id,omitempty"`
	Phase          string `json:"phase,omitempty"`
}

type EmbodimentPose struct {
	ID                   string   `json:"id"`
	Meaning              string   `json:"meaning"`
	Uses                 []string `json:"uses,omitempty"`
	SupportedExpressions []string `json:"supported_expressions,omitempty"`
	SupportedGazes       []string `json:"supported_gazes,omitempty"`
}

type EmbodimentCapabilities struct {
	FormatVersion int              `json:"format_version"`
	ShellName     string           `json:"shell_name,omitempty"`
	DefaultPose   string           `json:"default_pose"`
	Poses         []EmbodimentPose `json:"poses,omitempty"`
	Expressions   []string         `json:"expressions,omitempty"`
	Gazes         []string         `json:"gazes,omitempty"`
}

type ShellSurfaceCombination struct {
	Pose       string `json:"pose"`
	Expression string `json:"expression"`
	Gaze       string `json:"gaze"`
	Surface    int    `json:"surface"`
}

type ShellSemantics struct {
	FormatVersion int                       `json:"format_version"`
	ShellName     string                    `json:"shell_name,omitempty"`
	DefaultPose   string                    `json:"default_pose"`
	Poses         []EmbodimentPose          `json:"poses,omitempty"`
	Expressions   []string                  `json:"expressions,omitempty"`
	Gazes         []string                  `json:"gazes,omitempty"`
	Surfaces      []ShellSurfaceCombination `json:"surfaces,omitempty"`
}

func (s ShellSemantics) Capabilities() *EmbodimentCapabilities {
	if s.FormatVersion <= 0 || len(s.Poses) == 0 {
		return nil
	}
	poses := make([]EmbodimentPose, len(s.Poses))
	copy(poses, s.Poses)
	expr := map[string]map[string]bool{}
	gaze := map[string]map[string]bool{}
	for _, x := range s.Surfaces {
		if expr[x.Pose] == nil {
			expr[x.Pose] = map[string]bool{}
		}
		if gaze[x.Pose] == nil {
			gaze[x.Pose] = map[string]bool{}
		}
		expr[x.Pose][x.Expression] = true
		gaze[x.Pose][x.Gaze] = true
	}
	for i := range poses {
		for _, x := range s.Expressions {
			if expr[poses[i].ID][x] {
				poses[i].SupportedExpressions = append(poses[i].SupportedExpressions, x)
			}
		}
		for _, x := range s.Gazes {
			if gaze[poses[i].ID][x] {
				poses[i].SupportedGazes = append(poses[i].SupportedGazes, x)
			}
		}
	}
	return &EmbodimentCapabilities{FormatVersion: s.FormatVersion, ShellName: s.ShellName, DefaultPose: s.DefaultPose, Poses: poses, Expressions: s.Expressions, Gazes: s.Gazes}
}

type RequestEnvelope struct {
	RequestID        string                  `json:"request_id"`
	RequestClass     string                  `json:"request_class"`
	Source           string                  `json:"source"`
	CreatedAt        string                  `json:"created_at"`
	UserInput        UserInput               `json:"user_input"`
	CurrentState     CurrentState            `json:"current_state"`
	RequestPolicy    RequestPolicy           `json:"request_policy"`
	HotMemory        HotMemorySnapshot       `json:"hot_memory,omitempty"`
	RecentDialogue   []DialogueTurn          `json:"recent_dialogue,omitempty"`
	RecentPhysical   []PhysicalEvent         `json:"recent_physical,omitempty"`
	MemoryCapsule    MemoryCapsule           `json:"memory_capsule,omitempty"`
	Continuation     *ContinuationRef        `json:"continuation,omitempty"`
	Linked           *LinkedRef              `json:"linked,omitempty"`
	SemanticDigest   string                  `json:"semantic_digest,omitempty"`
	ReactionIntent   string                  `json:"reaction_intent,omitempty"`
	Embodiment       *EmbodimentCapabilities `json:"embodiment,omitempty"`
	AppearanceChange *AppearanceTransition   `json:"appearance_change,omitempty"`
}

type Presentation struct {
	Expression string `json:"expression,omitempty"`
	Pose       string `json:"pose,omitempty"`
	Gaze       string `json:"gaze,omitempty"`
	Gesture    string `json:"gesture,omitempty"`
}

type ContinuationIntent struct {
	Action  string `json:"action,omitempty"`
	AfterMS int64  `json:"after_ms,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type Reaction struct {
	RequestID       string              `json:"request_id"`
	RequestClass    string              `json:"request_class,omitempty"`
	Action          string              `json:"action,omitempty"`
	Dialogue        string              `json:"dialogue"`
	ReactionEmotion string              `json:"reaction_emotion"`
	Presentation    Presentation        `json:"presentation"`
	Continuation    *ContinuationIntent `json:"continuation,omitempty"`
	Notes           []string            `json:"notes,omitempty"`
	ProviderID      string              `json:"provider_id,omitempty"`
}

type AffectDelta struct {
	Channels   map[string]float64 `json:"channels,omitempty"`
	DeltaMax   float64            `json:"delta_max,omitempty"`
	DeltaTotal float64            `json:"delta_total,omitempty"`
	Dominant   string             `json:"dominant,omitempty"`
}

type EpisodeMaterial struct {
	UserRequest     string `json:"user_request,omitempty"`
	WebResponse     string `json:"web_response,omitempty"`
	RequestSummary  string `json:"request_summary,omitempty"`
	ResponseSummary string `json:"response_summary,omitempty"`
	Topic           string `json:"topic,omitempty"`
	Outcome         string `json:"outcome,omitempty"`
}

type EpisodeCommitV2 struct {
	EpisodeID        string                `json:"episode_id"`
	RequestID        string                `json:"request_id"`
	RequestClass     string                `json:"request_class"`
	CompletedAt      string                `json:"completed_at"`
	Source           string                `json:"source"`
	UserInput        UserInput             `json:"user_input"`
	Event            *PhysicalEvent        `json:"event,omitempty"`
	AppearanceChange *AppearanceTransition `json:"appearance_change,omitempty"`
	Situation        string                `json:"situation,omitempty"`
	Reaction         Reaction              `json:"reaction"`
	// AffectAtRequest is the state the foreground cognition actually saw when
	// the request was created.  AffectBefore/After below are reserved for the
	// causal reaction impulse interval so concurrent requests cannot borrow
	// each other's affect changes when Memory v2 scores emotional salience.
	AffectAtRequest AffectState      `json:"affect_at_request"`
	AffectBefore    AffectState      `json:"affect_before"`
	AffectAfter     AffectState      `json:"affect_after"`
	AffectDelta     AffectDelta      `json:"affect_delta"`
	Status          string           `json:"status"`
	Linked          *LinkedRef       `json:"linked,omitempty"`
	Material        *EpisodeMaterial `json:"episode_material,omitempty"`
}

type MemoryEvaluation struct {
	SemanticImportance float64  `json:"semantic_importance"`
	EmotionalSalience  float64  `json:"emotional_salience"`
	Novelty            float64  `json:"novelty"`
	Commitment         float64  `json:"commitment"`
	Recurrence         float64  `json:"recurrence"`
	PersonalRelevance  float64  `json:"personal_relevance"`
	Triviality         float64  `json:"triviality"`
	ExplicitImportance float64  `json:"explicit_importance"`
	ReasonTags         []string `json:"reason_tags,omitempty"`
}

type SemanticCandidate struct {
	Kind            string   `json:"kind"`
	Text            string   `json:"text"`
	Confidence      float64  `json:"confidence"`
	Entities        []string `json:"entities,omitempty"`
	DurableExplicit bool     `json:"durable_explicit,omitempty"`
	Contradicts     []string `json:"contradicts,omitempty"`
}

type MemoryBrainResult struct {
	Evaluation MemoryEvaluation    `json:"evaluation"`
	Summary    string              `json:"episode_summary"`
	Candidates []SemanticCandidate `json:"semantic_candidates"`
}

type CharacterSummaryProposal struct {
	Identity         []string `json:"identity,omitempty"`
	StableBehavior   []string `json:"stable_behavior,omitempty"`
	WorldContext     []string `json:"world_context,omitempty"`
	StableAppearance []string `json:"stable_appearance,omitempty"`
	Unknowns         []string `json:"unknowns,omitempty"`
}

type CharacterSummaryMeta struct {
	FormatVersion   int    `json:"format_version"`
	Generation      int64  `json:"generation"`
	SourceHash      string `json:"source_hash"`
	SourceUpdatedAt string `json:"source_updated_at,omitempty"`
	GuideHash       string `json:"guide_hash"`
	ConfigHash      string `json:"config_hash"`
	ModelID         string `json:"model_id"`
	CreatedAt       string `json:"created_at"`
	Validation      string `json:"validation"`
	ShellKey        string `json:"shell_key,omitempty"`
	ShellName       string `json:"shell_name,omitempty"`
	AppearanceFile  string `json:"appearance_file,omitempty"`
}

func Now() string { return time.Now().Format(time.RFC3339Nano) }
