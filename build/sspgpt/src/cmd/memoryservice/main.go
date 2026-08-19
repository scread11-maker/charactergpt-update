package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"sspgpt/v07/internal/httpjson"
	"sspgpt/v07/internal/localinfer"
	"sspgpt/v07/internal/model"
	"sspgpt/v07/internal/paths"
	"sspgpt/v07/internal/singleinstance"
)

const version = "0.7.1-fix12"

type consolidationEpoch struct {
	FormatVersion int    `json:"format_version"`
	ID            string `json:"id"`
	StartedAt     string `json:"started_at"`
	Reason        string `json:"reason"`
}

type retentionConfig struct {
	FormatVersion int                `json:"format_version"`
	Weights       map[string]float64 `json:"weights"`
	Thresholds    map[string]float64 `json:"thresholds"`
	Modifiers     struct {
		Source          map[string]float64 `json:"source"`
		RequestClass    map[string]float64 `json:"request_class"`
		Gesture         map[string]float64 `json:"gesture"`
		ReactionEmotion map[string]float64 `json:"reaction_emotion"`
		DirectiveKind   map[string]float64 `json:"directive_kind"`
		DirectiveID     map[string]float64 `json:"directive_id"`
	} `json:"modifiers"`
	ObservationMinEvidence int `json:"observation_min_evidence"`
	HotMemoryMaxItems      int `json:"hot_memory_max_items"`
}
type retrievalPreset struct {
	CandidatePool          int   `json:"candidate_pool,omitempty"`
	ContextBudgetTokens    int   `json:"context_budget_tokens,omitempty"`
	ReplayMaxContextTokens int   `json:"replay_max_context_tokens,omitempty"`
	RecallTimeoutMS        int64 `json:"recall_timeout_ms"`
}
type retrievalConfig struct {
	FormatVersion      int                        `json:"format_version"`
	DefaultDepth       string                     `json:"default_depth"`
	EmbeddingDimension int                        `json:"embedding_dimension"`
	QueryInstruction   string                     `json:"query_instruction"`
	RRFK               int                        `json:"rrf_k"`
	Presets            map[string]retrievalPreset `json:"presets"`
}
type vectorRecord struct {
	MemoryID   string    `json:"memory_id"`
	Generation int       `json:"generation"`
	Model      string    `json:"model"`
	Dimension  int       `json:"dimension"`
	Vector     []float64 `json:"vector"`
}
type processedRecord struct {
	EpisodeID   string `json:"episode_id"`
	ProcessedAt string `json:"processed_at"`
	Status      string `json:"status"`
}

func applyProcessedStatus(processed map[string]bool, x processedRecord) {
	if x.EpisodeID == "" {
		return
	}
	switch x.Status {
	case "ok", "skipped_pre_epoch":
		processed[x.EpisodeID] = true
	case "revalidate_fix8", "invalid_schema", "retry":
		delete(processed, x.EpisodeID)
	}
}

type rawRecent struct {
	EpisodeID string `json:"episode_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Kind      string `json:"kind,omitempty"` // user_accepted|episode_completed
	Timestamp string `json:"timestamp"`
	User      string `json:"user,omitempty"`
	Character string `json:"character,omitempty"`
	Event     string `json:"event,omitempty"`
}
type brainAudit struct {
	EpisodeID      string                 `json:"episode_id"`
	Scores         model.MemoryEvaluation `json:"scores"`
	RetentionScore float64                `json:"retention_score"`
	Retained       int                    `json:"retained"`
	ModelID        string                 `json:"model_id"`
	Degraded       string                 `json:"degraded,omitempty"`
}

// memoryBrainWire uses pointers for required numeric fields so a syntactically
// valid but structurally wrong local-model response cannot silently collapse to
// Go zero-values and be treated as a legitimate "remember nothing" decision.
type memoryEvaluationWire struct {
	SemanticImportance *float64 `json:"semantic_importance"`
	EmotionalSalience  *float64 `json:"emotional_salience"`
	Novelty            *float64 `json:"novelty"`
	Commitment         *float64 `json:"commitment"`
	Recurrence         *float64 `json:"recurrence"`
	PersonalRelevance  *float64 `json:"personal_relevance"`
	Triviality         *float64 `json:"triviality"`
	ExplicitImportance *float64 `json:"explicit_importance"`
	ReasonTags         []string `json:"reason_tags,omitempty"`
}
type semanticCandidateWire struct {
	Kind            *string  `json:"kind"`
	Text            *string  `json:"text"`
	Confidence      *float64 `json:"confidence"`
	Entities        []string `json:"entities,omitempty"`
	DurableExplicit bool     `json:"durable_explicit,omitempty"`
	Contradicts     []string `json:"contradicts,omitempty"`
}
type memoryBrainWire struct {
	Evaluation *memoryEvaluationWire   `json:"evaluation"`
	Summary    string                  `json:"episode_summary"`
	Candidates []semanticCandidateWire `json:"semantic_candidates"`
}

type memoryBrainReactionInput struct {
	Dialogue        string `json:"dialogue,omitempty"`
	ReactionEmotion string `json:"reaction_emotion,omitempty"`
}
type memoryBrainMaterialInput struct {
	UserRequest     string `json:"user_request,omitempty"`
	RequestSummary  string `json:"request_summary,omitempty"`
	ResponseSummary string `json:"response_summary,omitempty"`
	Topic           string `json:"topic,omitempty"`
	Outcome         string `json:"outcome,omitempty"`
}
type memoryBrainAppearanceInput struct {
	Kind              string `json:"kind,omitempty"`
	PreviousShellName string `json:"previous_shell_name,omitempty"`
	CurrentShellName  string `json:"current_shell_name,omitempty"`
	ChangedAt         string `json:"changed_at,omitempty"`
}

type memoryBrainInput struct {
	EpisodeID        string                      `json:"episode_id"`
	RequestClass     string                      `json:"request_class"`
	Source           string                      `json:"source"`
	CompletedAt      string                      `json:"completed_at"`
	UserInput        model.UserInput             `json:"user_input"`
	Event            *model.PhysicalEvent        `json:"event,omitempty"`
	AppearanceChange *memoryBrainAppearanceInput `json:"appearance_change,omitempty"`
	Situation        string                      `json:"situation,omitempty"`
	Reaction         memoryBrainReactionInput    `json:"reaction"`
	AffectDelta      model.AffectDelta           `json:"affect_delta"`
	Material         *memoryBrainMaterialInput   `json:"linked_material,omitempty"`
}

type service struct {
	root            string
	log             *log.Logger
	audit           *log.Logger
	infer           *localinfer.Manager
	mu              sync.RWMutex
	episodes        map[string]model.EpisodeCommitV2
	episodeOrder    []string
	semantic        map[string]model.SemanticMemory
	vectors         map[string]vectorRecord
	processed       map[string]bool
	hot             model.HotMemorySnapshot
	hotSeq          int64
	seq             atomic.Uint64
	jobs            chan model.EpisodeCommitV2
	stop            chan struct{}
	workerCtx       context.Context
	workerCancel    context.CancelFunc
	workerDone      chan struct{}
	shutdownOnce    sync.Once
	shuttingDown    atomic.Bool
	opsMu           sync.Mutex
	opsClosing      bool
	opsWG           sync.WaitGroup
	opsCtx          context.Context
	opsCancel       context.CancelFunc
	rerankerWarmMu  sync.Mutex
	rerankerWarming bool
	rerankerReady   atomic.Bool
	epoch           consolidationEpoch
	epochStart      time.Time
}

type semanticPlan struct {
	mem         model.SemanticMemory
	contradicts []string
}

type preparedSemantic struct {
	mem         model.SemanticMemory
	vector      vectorRecord
	contradicts []string
}

func main() {
	root := paths.GhostRoot()
	if !singleinstance.Acquire("MemoryService", root) {
		return
	}
	_ = os.MkdirAll(filepath.Join(root, "memory", "index"), 0755)
	_ = os.MkdirAll(filepath.Join(root, "memory", "models"), 0755)
	_ = os.MkdirAll(filepath.Join(root, "memory", "inference"), 0755)
	_ = os.MkdirAll(filepath.Join(root, "logs"), 0755)
	lf, _ := os.OpenFile(filepath.Join(root, "logs", "memory_service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	af, _ := os.OpenFile(filepath.Join(root, "logs", "memory_audit.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	opsCtx, opsCancel := context.WithCancel(context.Background())
	s := &service{root: root, log: log.New(lf, "", log.LstdFlags|log.Lmicroseconds), audit: log.New(af, "", log.LstdFlags|log.Lmicroseconds), infer: localinfer.New(root), episodes: map[string]model.EpisodeCommitV2{}, semantic: map[string]model.SemanticMemory{}, vectors: map[string]vectorRecord{}, processed: map[string]bool{}, jobs: make(chan model.EpisodeCommitV2, 256), stop: make(chan struct{}), workerCtx: workerCtx, workerCancel: workerCancel, workerDone: make(chan struct{}), opsCtx: opsCtx, opsCancel: opsCancel}
	s.initEpoch()
	s.load()
	s.migrateFix8RevalidateProcessed()
	s.rebuildHotLocked()
	if s.semanticCount() > 0 {
		s.ensureRerankerWarmAsync("startup_semantic_store")
	}
	go s.worker()
	go s.requeueUnprocessed()
	go s.pushHot()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/v2/episode", s.episode)
	mux.HandleFunc("/v2/dialogue", s.dialogueEndpoint)
	mux.HandleFunc("/v2/recall", s.recallEndpoint)
	mux.HandleFunc("/v2/hot", s.hotEndpoint)
	mux.HandleFunc("/v2/status", s.statusEndpoint)
	mux.HandleFunc("/v2/profile/compile", s.profileCompile)
	mux.HandleFunc("/v2/models/prepare", s.prepareModels)
	mux.HandleFunc("/v2/models/status", s.modelsStatus)
	mux.HandleFunc("/v2/observe", s.observe)
	mux.HandleFunc("/shutdown", s.shutdown)
	addr := "127.0.0.1:8768"
	s.log.Printf("MemoryService %s listening %s root=%s", version, addr, root)
	if err := http.ListenAndServe(addr, mux); err != nil {
		s.log.Fatal(err)
	}
}

func (s *service) health(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, 200, map[string]any{"ok": true, "service": "MemoryService", "version": version, "shutting_down": s.shuttingDown.Load(), "semantic_memories": s.semanticCount(), "local_models": map[string]any{"memory_llm": s.infer.Status("memory_llm"), "embedder": s.infer.Status("embedder"), "reranker": s.infer.Status("reranker")}})
}

func (s *service) shutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	started := false
	s.shutdownOnce.Do(func() {
		started = true
		s.shuttingDown.Store(true)
		s.opsMu.Lock()
		s.opsClosing = true
		s.opsMu.Unlock()
		s.log.Printf("SHUTDOWN_BEGIN policy=cancel_inference_preserve_persistence")
		close(s.stop)
		if s.workerCancel != nil {
			s.workerCancel()
		}
		if s.opsCancel != nil {
			s.opsCancel()
		}
		go s.finishShutdown()
	})
	httpjson.Write(w, http.StatusAccepted, map[string]any{"ok": true, "started": started, "service": "MemoryService", "version": version, "recovery": "unprocessed episodes requeue on next start"})
}

func (s *service) finishShutdown() {
	// The worker's model phase is context-cancellable. If it has already crossed
	// into the short semantic/vector persistence phase, workerDone closes only
	// after those writes and the processed marker complete.
	if s.workerDone != nil {
		<-s.workerDone
	}
	s.opsWG.Wait()
	s.infer.Stop()
	s.log.Printf("SHUTDOWN_COMPLETE local_runners=stopped")
	time.Sleep(120 * time.Millisecond)
	os.Exit(0)
}

func (s *service) beginInferOp() bool {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if s.opsClosing || s.shuttingDown.Load() {
		return false
	}
	s.opsWG.Add(1)
	return true
}

func (s *service) endInferOp() { s.opsWG.Done() }

func (s *service) inferOpContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	if s.opsCtx == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(s.opsCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// ensureRerankerWarmAsync keeps model startup outside the foreground recall
// budget. Candidate retrieval may be large (medium=300), so reranking must not
// repeatedly lose to a 1.2s cold-start deadline. The runner is prepared only
// once semantic memory exists or a recall actually needs it; idle installs
// with no retrievable memory keep the old low-resource behavior.
func (s *service) ensureRerankerWarmAsync(reason string) {
	if s.shuttingDown.Load() || s.rerankerReady.Load() {
		return
	}
	s.rerankerWarmMu.Lock()
	if s.rerankerWarming || s.rerankerReady.Load() {
		s.rerankerWarmMu.Unlock()
		return
	}
	s.rerankerWarming = true
	s.rerankerWarmMu.Unlock()
	if !s.beginInferOp() {
		s.rerankerWarmMu.Lock()
		s.rerankerWarming = false
		s.rerankerWarmMu.Unlock()
		return
	}
	go func() {
		defer s.endInferOp()
		defer func() {
			s.rerankerWarmMu.Lock()
			s.rerankerWarming = false
			s.rerankerWarmMu.Unlock()
		}()
		base := s.opsCtx
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithTimeout(base, 2*time.Minute)
		err := s.infer.EnsureServer(ctx, "reranker")
		cancel()
		if err != nil {
			s.rerankerReady.Store(false)
			s.log.Printf("RERANKER_WARM reason=%s ready=false error=%v", reason, err)
			return
		}
		s.rerankerReady.Store(true)
		s.log.Printf("RERANKER_WARM reason=%s ready=true", reason)
	}()
}
func (s *service) semanticCount() int { s.mu.RLock(); defer s.mu.RUnlock(); return len(s.semantic) }

func (s *service) retention() retentionConfig {
	c := retentionConfig{Weights: map[string]float64{"semantic_importance": 1, "emotional_salience": 1.4, "novelty": .7, "commitment": 1.3, "personal_relevance": .9, "recurrence_relevance": .7, "explicit_importance": 1.5, "triviality": -1.2}, Thresholds: map[string]float64{"semantic_store": .42, "strong_retention": .72, "hot_memory_candidate": .60}, ObservationMinEvidence: 2, HotMemoryMaxItems: 5}
	b, e := os.ReadFile(filepath.Join(s.root, "config", "memory_retention_rules.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Weights == nil {
		c.Weights = map[string]float64{}
	}
	if c.Thresholds == nil {
		c.Thresholds = map[string]float64{}
	}
	if c.ObservationMinEvidence < 2 {
		c.ObservationMinEvidence = 2
	}
	if c.HotMemoryMaxItems <= 0 {
		c.HotMemoryMaxItems = 5
	}
	return c
}
func defaultRetrievalConfig() retrievalConfig {
	return retrievalConfig{
		FormatVersion:      2,
		DefaultDepth:       "medium",
		EmbeddingDimension: 512,
		QueryInstruction:   "Instruct: Retrieve memories relevant to the current user request and situation. Query: ",
		RRFK:               60,
		Presets: map[string]retrievalPreset{
			"light":     {CandidatePool: 100, ContextBudgetTokens: 512, RecallTimeoutMS: 700},
			"medium":    {CandidatePool: 300, ContextBudgetTokens: 1024, RecallTimeoutMS: 1200},
			"deep":      {CandidatePool: 600, ContextBudgetTokens: 2048, RecallTimeoutMS: 2000},
			"unbounded": {ReplayMaxContextTokens: 32768, RecallTimeoutMS: 700},
		},
	}
}
func normalizeRecallDepth(depth string) string {
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "light", "medium", "deep", "unbounded":
		return strings.ToLower(strings.TrimSpace(depth))
	default:
		return "medium"
	}
}
func (s *service) retrieval() retrievalConfig {
	c := defaultRetrievalConfig()
	b, e := os.ReadFile(filepath.Join(s.root, "config", "memory_retrieval_rules.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.EmbeddingDimension <= 0 {
		c.EmbeddingDimension = 512
	}
	if strings.TrimSpace(c.QueryInstruction) == "" {
		c.QueryInstruction = defaultRetrievalConfig().QueryInstruction
	}
	if c.RRFK <= 0 {
		c.RRFK = 60
	}
	if c.Presets == nil {
		c.Presets = map[string]retrievalPreset{}
	}
	defaults := defaultRetrievalConfig().Presets
	for name, def := range defaults {
		p := c.Presets[name]
		if p.CandidatePool <= 0 {
			p.CandidatePool = def.CandidatePool
		}
		if p.ContextBudgetTokens <= 0 {
			p.ContextBudgetTokens = def.ContextBudgetTokens
		}
		if p.ReplayMaxContextTokens <= 0 && def.ReplayMaxContextTokens > 0 {
			p.ReplayMaxContextTokens = def.ReplayMaxContextTokens
		}
		if p.RecallTimeoutMS <= 0 {
			p.RecallTimeoutMS = def.RecallTimeoutMS
		}
		c.Presets[name] = p
	}
	c.DefaultDepth = normalizeRecallDepth(c.DefaultDepth)
	return c
}
func (c retrievalConfig) preset(depth string) (string, retrievalPreset) {
	depth = normalizeRecallDepth(depth)
	if strings.TrimSpace(depth) == "" {
		depth = normalizeRecallDepth(c.DefaultDepth)
	}
	p, ok := c.Presets[depth]
	if !ok {
		depth = "medium"
		p = defaultRetrievalConfig().Presets[depth]
	}
	return depth, p
}
func clamp(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
func (s *service) retentionScore(ev model.MemoryEvaluation, ep model.EpisodeCommitV2, c retentionConfig) float64 {
	// The Memory Brain judges semantic dimensions, but MemoryService owns the
	// configurable policy gate.  A sparse but decisive signal (for example a
	// large emotional transition or an explicit durable commitment) must not be
	// diluted merely because unrelated dimensions are zero.  We therefore mix
	// the strongest weighted positive signal with the weighted positive mean,
	// then apply triviality as a penalty and finally deterministic modifiers.
	vals := map[string]float64{
		"semantic_importance":  ev.SemanticImportance,
		"emotional_salience":   ev.EmotionalSalience,
		"novelty":              ev.Novelty,
		"commitment":           ev.Commitment,
		"personal_relevance":   ev.PersonalRelevance,
		"recurrence_relevance": ev.Recurrence,
		"explicit_importance":  ev.ExplicitImportance,
	}
	weightedSum, positiveWeight, strongest := 0.0, 0.0, 0.0
	for k, v := range vals {
		w := c.Weights[k]
		if w <= 0 {
			continue
		}
		v = clamp(v)
		weightedSum += w * v
		positiveWeight += w
		// Weights above 1 intentionally make this dimension reach a decisive
		// signal sooner; clamp keeps the policy score bounded.
		if sig := clamp(w * v); sig > strongest {
			strongest = sig
		}
	}
	mean := 0.0
	if positiveWeight > 0 {
		mean = weightedSum / positiveWeight
	}
	score := 0.65*strongest + 0.35*mean
	if w := c.Weights["triviality"]; w < 0 {
		score -= clamp(ev.Triviality) * math.Abs(w) * 0.25
	}
	score += c.Modifiers.Source[ep.Source] + c.Modifiers.RequestClass[ep.RequestClass] + c.Modifiers.ReactionEmotion[ep.Reaction.ReactionEmotion]
	if ep.UserInput.Directive != nil {
		score += c.Modifiers.DirectiveKind[ep.UserInput.Directive.Kind] + c.Modifiers.DirectiveID[ep.UserInput.Directive.ID]
	}
	if ep.Event != nil {
		score += c.Modifiers.Gesture[ep.Event.Gesture]
	}
	return clamp(score)
}
func affectDelta(before, after model.AffectState) model.AffectDelta {
	keys := map[string]bool{}
	for k := range before.Channels {
		keys[k] = true
	}
	for k := range after.Channels {
		keys[k] = true
	}
	d := model.AffectDelta{Channels: map[string]float64{}}
	for k := range keys {
		x := after.Channels[k] - before.Channels[k]
		d.Channels[k] = x
		ax := math.Abs(x)
		d.DeltaTotal += ax
		if ax > d.DeltaMax {
			d.DeltaMax = ax
			d.Dominant = k
		}
	}
	return d
}

func (s *service) episode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	if s.shuttingDown.Load() {
		httpjson.Write(w, http.StatusServiceUnavailable, map[string]any{"error": "memory service is shutting down", "retry_on_next_start": true})
		return
	}
	var ep model.EpisodeCommitV2
	if err := httpjson.Decode(r, &ep); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if ep.EpisodeID == "" {
		ep.EpisodeID = ep.RequestID
	}
	if ep.CompletedAt == "" {
		ep.CompletedAt = model.Now()
	}
	if ep.AffectDelta.Channels == nil {
		ep.AffectDelta = affectDelta(ep.AffectBefore, ep.AffectAfter)
	}
	if ep.Status != "completed" {
		httpjson.Write(w, 202, map[string]any{"ok": true, "journaled": false, "reason": "non_completed"})
		return
	}
	if ep.Event != nil && strings.EqualFold(ep.Event.Type, "move") {
		httpjson.Write(w, 202, map[string]any{"ok": true, "journaled": false, "reason": "raw_move_rejected"})
		return
	}
	s.mu.Lock()
	if _, ok := s.episodes[ep.EpisodeID]; ok {
		s.mu.Unlock()
		httpjson.Write(w, 200, map[string]any{"ok": true, "duplicate": true})
		return
	}
	s.episodes[ep.EpisodeID] = ep
	s.episodeOrder = append(s.episodeOrder, ep.EpisodeID)
	s.mu.Unlock()
	if err := appendJSON(filepath.Join(s.root, "memory", "episodes_v2.jsonl"), ep); err != nil {
		s.log.Printf("EPISODE_JOURNAL_ERROR id=%s err=%v", ep.EpisodeID, err)
		httpjson.Write(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.appendRawRecent(ep)
	s.audit.Printf("EPISODE_COMMIT_V2 id=%s request=%s class=%s source=%s delta_max=%.4f", ep.EpisodeID, ep.RequestID, ep.RequestClass, ep.Source, ep.AffectDelta.DeltaMax)
	queued := true
	select {
	case s.jobs <- ep:
	case <-s.stop:
		queued = false
	default:
		go func() {
			select {
			case s.jobs <- ep:
			case <-s.stop:
			}
		}()
	}
	httpjson.Write(w, 202, map[string]any{"ok": true, "journaled": true, "queued": queued, "retry_on_next_start": !queued, "episode_id": ep.EpisodeID})
}
func (s *service) appendRawRecent(ep model.EpisodeCommitV2) {
	rr := rawRecent{EpisodeID: ep.EpisodeID, RequestID: ep.RequestID, Kind: "episode_completed", Timestamp: ep.CompletedAt, User: ep.UserInput.Text, Character: ep.Reaction.Dialogue}
	if ep.Material != nil {
		if rr.User == "" {
			rr.User = ep.Material.UserRequest
		}
		if rr.Character == "" {
			rr.Character = ep.Material.WebResponse
		}
	}
	if ep.Event != nil {
		rr.Event = fmt.Sprintf("gesture=%s target=%s phase=%s contact=%t released=%t", ep.Event.Gesture, ep.Event.Target, ep.Event.Phase, ep.Event.Contact, ep.Event.Released)
	}
	_ = appendJSON(filepath.Join(s.root, "memory", "raw_recent_v2.jsonl"), rr)
}
func (s *service) dialogueEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var x rawRecent
	if err := httpjson.Decode(r, &x); err != nil {
		httpjson.Write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	x.User = strings.TrimSpace(x.User)
	x.Character = strings.TrimSpace(x.Character)
	if x.User == "" && x.Character == "" {
		httpjson.Write(w, http.StatusBadRequest, map[string]string{"error": "dialogue text required"})
		return
	}
	if strings.TrimSpace(x.Timestamp) == "" {
		x.Timestamp = model.Now()
	}
	if strings.TrimSpace(x.Kind) == "" {
		x.Kind = "user_accepted"
	}
	if err := appendJSON(filepath.Join(s.root, "memory", "raw_recent_v2.jsonl"), x); err != nil {
		httpjson.Write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.log.Printf("RAW_DIALOGUE kind=%s request=%s user_chars=%d character_chars=%d", x.Kind, x.RequestID, len([]rune(x.User)), len([]rune(x.Character)))
	httpjson.Write(w, http.StatusAccepted, map[string]any{"ok": true})
}

func appendJSON(path string, v any) error {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	defer f.Close()
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	return e
}

func (s *service) worker() {
	if s.workerDone != nil {
		defer close(s.workerDone)
	}
	ctx := s.workerCtx
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if s.shuttingDown.Load() {
			return
		}
		select {
		case <-s.stop:
			return
		case ep := <-s.jobs:
			if s.shuttingDown.Load() {
				return
			}
			if s.isProcessed(ep.EpisodeID) {
				continue
			}
			if err := s.consolidateContext(ctx, ep); err != nil {
				if s.shuttingDown.Load() || errors.Is(err, context.Canceled) {
					s.log.Printf("MEMORY_BRAIN_PAUSED episode=%s reason=shutdown", ep.EpisodeID)
					return
				}
				s.log.Printf("MEMORY_BRAIN_DEFER episode=%s error=%v", ep.EpisodeID, err)
				s.audit.Printf("MEMORY_BRAIN_DEGRADED episode=%s error=%q", ep.EpisodeID, err.Error())
				go func(e model.EpisodeCommitV2) {
					select {
					case <-s.stop:
						return
					case <-time.After(60 * time.Second):
						select {
						case s.jobs <- e:
						case <-s.stop:
						}
					}
				}(ep)
			}
		}
	}
}
func (s *service) isProcessed(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.processed[id]
}

func (s *service) initEpoch() {
	path := filepath.Join(s.root, "memory", "consolidation_epoch_v2.json")
	var e consolidationEpoch
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &e) == nil && e.ID != "" {
		if t, err := time.Parse(time.RFC3339Nano, e.StartedAt); err == nil {
			s.epoch, s.epochStart = e, t
			return
		}
	}
	now := time.Now()
	e = consolidationEpoch{FormatVersion: 1, ID: fmt.Sprintf("fix3-%d", now.Unix()), StartedAt: now.Format(time.RFC3339Nano), Reason: "quarantine pre-fix3 Memory v2 backlog after structured-output/contact regressions"}
	b, _ := json.MarshalIndent(e, "", "  ")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, b, 0644)
	s.epoch, s.epochStart = e, now
}

func (s *service) preEpoch(ep model.EpisodeCommitV2) bool {
	if s.epochStart.IsZero() {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, ep.CompletedAt)
	return err == nil && t.Before(s.epochStart)
}

func (s *service) activeSemantic(m model.SemanticMemory) bool {
	return m.Metadata != nil && m.Metadata["consolidation_epoch"] == s.epoch.ID
}

func (s *service) requeueUnprocessed() {
	time.Sleep(2 * time.Second)
	s.mu.Lock()
	var eps []model.EpisodeCommitV2
	for _, id := range s.episodeOrder {
		if !s.processed[id] {
			ep := s.episodes[id]
			if s.preEpoch(ep) {
				s.processed[id] = true
				_ = appendJSON(filepath.Join(s.root, "memory", "processed_v2.jsonl"), processedRecord{EpisodeID: id, ProcessedAt: model.Now(), Status: "skipped_pre_epoch"})
				s.audit.Printf("MEMORY_EPOCH_SKIP episode=%s epoch=%s completed_at=%s", id, s.epoch.ID, ep.CompletedAt)
				continue
			}
			eps = append(eps, ep)
		}
	}
	s.mu.Unlock()
	for _, ep := range eps {
		select {
		case s.jobs <- ep:
		case <-s.stop:
			return
		}
	}
}

func (s *service) memoryGuide() string {
	b, _ := os.ReadFile(filepath.Join(s.root, "config", "memory_evaluation_guide.md"))
	return string(b)
}

func scoreField(name string, p *float64) (float64, error) {
	if p == nil {
		return 0, fmt.Errorf("memory brain missing required score %s", name)
	}
	if math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0 || *p > 1 {
		return 0, fmt.Errorf("memory brain score %s out of range: %v", name, *p)
	}
	return *p, nil
}

func validateMemoryBrainWire(w memoryBrainWire) (model.MemoryBrainResult, error) {
	if w.Evaluation == nil {
		return model.MemoryBrainResult{}, errors.New("memory brain missing required evaluation object")
	}
	e := w.Evaluation
	sem, err := scoreField("semantic_importance", e.SemanticImportance)
	if err != nil {
		return model.MemoryBrainResult{}, err
	}
	emo, err := scoreField("emotional_salience", e.EmotionalSalience)
	if err != nil {
		return model.MemoryBrainResult{}, err
	}
	nov, err := scoreField("novelty", e.Novelty)
	if err != nil {
		return model.MemoryBrainResult{}, err
	}
	com, err := scoreField("commitment", e.Commitment)
	if err != nil {
		return model.MemoryBrainResult{}, err
	}
	rec, err := scoreField("recurrence", e.Recurrence)
	if err != nil {
		return model.MemoryBrainResult{}, err
	}
	per, err := scoreField("personal_relevance", e.PersonalRelevance)
	if err != nil {
		return model.MemoryBrainResult{}, err
	}
	tri, err := scoreField("triviality", e.Triviality)
	if err != nil {
		return model.MemoryBrainResult{}, err
	}
	exp, err := scoreField("explicit_importance", e.ExplicitImportance)
	if err != nil {
		return model.MemoryBrainResult{}, err
	}
	out := model.MemoryBrainResult{Evaluation: model.MemoryEvaluation{SemanticImportance: sem, EmotionalSalience: emo, Novelty: nov, Commitment: com, Recurrence: rec, PersonalRelevance: per, Triviality: tri, ExplicitImportance: exp, ReasonTags: dedupeStrings(e.ReasonTags)}, Summary: strings.TrimSpace(w.Summary)}
	if sem == 0 && emo == 0 && nov == 0 && com == 0 && rec == 0 && per == 0 && tri == 0 && exp == 0 {
		return model.MemoryBrainResult{}, errors.New("memory brain returned degenerate all-zero evaluation")
	}
	for i, c := range w.Candidates {
		if c.Kind == nil || strings.TrimSpace(*c.Kind) == "" {
			return model.MemoryBrainResult{}, fmt.Errorf("memory brain candidate %d missing kind", i)
		}
		if c.Text == nil || strings.TrimSpace(*c.Text) == "" {
			return model.MemoryBrainResult{}, fmt.Errorf("memory brain candidate %d missing text", i)
		}
		conf, err := scoreField(fmt.Sprintf("semantic_candidates[%d].confidence", i), c.Confidence)
		if err != nil {
			return model.MemoryBrainResult{}, err
		}
		out.Candidates = append(out.Candidates, model.SemanticCandidate{Kind: strings.TrimSpace(*c.Kind), Text: strings.TrimSpace(*c.Text), Confidence: conf, Entities: dedupeStrings(c.Entities), DurableExplicit: c.DurableExplicit, Contradicts: dedupeStrings(c.Contradicts)})
	}
	return out, nil
}

func compactMemoryBrainInput(ep model.EpisodeCommitV2) memoryBrainInput {
	in := memoryBrainInput{
		EpisodeID: ep.EpisodeID, RequestClass: ep.RequestClass, Source: ep.Source,
		CompletedAt: ep.CompletedAt, UserInput: ep.UserInput, Event: ep.Event,
		Situation: truncate(strings.TrimSpace(ep.Situation), 500), AffectDelta: ep.AffectDelta,
		Reaction: memoryBrainReactionInput{Dialogue: truncate(strings.TrimSpace(ep.Reaction.Dialogue), 500), ReactionEmotion: ep.Reaction.ReactionEmotion},
	}
	if ep.AppearanceChange != nil {
		in.AppearanceChange = &memoryBrainAppearanceInput{Kind: ep.AppearanceChange.Kind, PreviousShellName: ep.AppearanceChange.PreviousShellName, CurrentShellName: ep.AppearanceChange.CurrentShellName, ChangedAt: ep.AppearanceChange.ChangedAt}
	}
	if ep.Material != nil {
		m := &memoryBrainMaterialInput{RequestSummary: truncate(strings.TrimSpace(ep.Material.RequestSummary), 500), ResponseSummary: truncate(strings.TrimSpace(ep.Material.ResponseSummary), 600), Topic: truncate(strings.TrimSpace(ep.Material.Topic), 100), Outcome: truncate(strings.TrimSpace(ep.Material.Outcome), 300)}
		if m.RequestSummary == "" {
			m.UserRequest = truncate(strings.TrimSpace(ep.Material.UserRequest), 500)
		}
		if m.ResponseSummary == "" && m.Outcome == "" {
			m.ResponseSummary = truncate(strings.TrimSpace(ep.Material.WebResponse), 600)
		}
		in.Material = m
	}
	return in
}

func (s *service) runMemoryBrain(parent context.Context, ep model.EpisodeCommitV2) (model.MemoryBrainResult, error) {
	cfg := s.infer.Config()
	if cfg.Mock {
		return mockBrain(ep), nil
	}
	b, _ := json.Marshal(compactMemoryBrainInput(ep))
	system := s.memoryGuide() + "\nReturn JSON only. Required shape: evaluation{semantic_importance,emotional_salience,novelty,commitment,recurrence,personal_relevance,triviality,explicit_importance,reason_tags}, episode_summary, semantic_candidates[{kind,text,confidence,entities,durable_explicit,contradicts}]. Every evaluation score is required and must be a number from 0 to 1. Do not rename fields."
	var last error
	for attempt := 1; attempt <= 2; attempt++ {
		ctx, cancel := context.WithTimeout(parent, 50*time.Second)
		var wire memoryBrainWire
		err := s.infer.ChatJSONLimit(ctx, system, string(b), 384, &wire)
		cancel()
		if err == nil {
			var br model.MemoryBrainResult
			br, err = validateMemoryBrainWire(wire)
			if err == nil {
				return br, nil
			}
		}
		last = err
		if parent.Err() != nil {
			return model.MemoryBrainResult{}, parent.Err()
		}
		s.audit.Printf("MEMORY_BRAIN_SCHEMA_RETRY episode=%s attempt=%d error=%q", ep.EpisodeID, attempt, errString(err))
		system += "\nPrevious output was invalid. Output exactly the required keys; omit prose and markdown."
	}
	if last == nil {
		last = errors.New("memory brain returned invalid structured output")
	}
	return model.MemoryBrainResult{}, last
}

func errString(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}
func (s *service) consolidate(ep model.EpisodeCommitV2) error {
	return s.consolidateContext(context.Background(), ep)
}

func (s *service) consolidateContext(parent context.Context, ep model.EpisodeCommitV2) error {
	cfg := s.infer.Config()
	br, err := s.runMemoryBrain(parent, ep)
	if err != nil {
		return err
	}
	if err := parent.Err(); err != nil {
		return err
	}
	rc := s.retention()
	score := s.retentionScore(br.Evaluation, ep, rc)
	threshold := rc.Thresholds["semantic_store"]
	if threshold <= 0 {
		threshold = .42
	}
	plans := []semanticPlan{}
	if score >= threshold {
		if len(br.Candidates) == 0 && strings.TrimSpace(br.Summary) == "" {
			return errors.New("memory brain retained episode but supplied no semantic candidate or summary")
		}
		for _, cand := range br.Candidates {
			cand.Text = strings.TrimSpace(cand.Text)
			if cand.Text == "" {
				continue
			}
			kind := canonicalKind(cand.Kind)
			if kind == "observation" && !cand.DurableExplicit {
				evidence := s.evidenceCount(cand.Text, cand.Entities)
				if evidence < rc.ObservationMinEvidence {
					kind = "episode"
				}
			}
			mem := model.SemanticMemory{ID: s.newID("mem"), Kind: kind, Text: cand.Text, CreatedAt: model.Now(), Importance: clamp(br.Evaluation.SemanticImportance), Confidence: clamp(cand.Confidence), EmotionalSalience: clamp(br.Evaluation.EmotionalSalience), RetentionScore: score, Entities: dedupeStrings(cand.Entities), SourceEpisodeIDs: []string{ep.EpisodeID}, EvidenceCount: 1, ValidFrom: ep.CompletedAt, Metadata: map[string]string{"source": ep.Source, "request_class": ep.RequestClass, "reaction_emotion": ep.Reaction.ReactionEmotion, "consolidation_epoch": s.epoch.ID}}
			if kind == "observation" {
				mem.EvidenceCount = s.evidenceCount(cand.Text, cand.Entities) + 1
			}
			if score >= rc.Thresholds["strong_retention"] {
				mem.Metadata["retention_tier"] = "strong"
			} else {
				mem.Metadata["retention_tier"] = "semantic"
			}
			plans = append(plans, semanticPlan{mem: mem, contradicts: append([]string(nil), cand.Contradicts...)})
		}
		if len(plans) == 0 && strings.TrimSpace(br.Summary) != "" {
			mem := model.SemanticMemory{ID: s.newID("mem"), Kind: "episode", Text: strings.TrimSpace(br.Summary), CreatedAt: model.Now(), Importance: clamp(br.Evaluation.SemanticImportance), Confidence: .8, EmotionalSalience: clamp(br.Evaluation.EmotionalSalience), RetentionScore: score, SourceEpisodeIDs: []string{ep.EpisodeID}, ValidFrom: ep.CompletedAt, Metadata: map[string]string{"source": ep.Source, "request_class": ep.RequestClass, "consolidation_epoch": s.epoch.ID}}
			plans = append(plans, semanticPlan{mem: mem})
		}
	}

	// Shutdown boundary: all model work (Memory Brain + embeddings) is
	// cancellable and happens before any semantic/vector persistence. Once this
	// staging succeeds, the worker enters a short non-cancellable persistence
	// phase. Runtime may close SSP immediately; MemoryService will finish these
	// local writes, then stop its llama runners and exit.
	prepared := make([]preparedSemantic, 0, len(plans))
	for _, plan := range plans {
		p, err := s.prepareSemantic(parent, plan)
		if err != nil {
			return err
		}
		prepared = append(prepared, p)
	}
	if err := parent.Err(); err != nil {
		return err
	}
	retained := 0
	for _, p := range prepared {
		if err := s.persistPreparedSemantic(p); err != nil {
			return err
		}
		retained++
	}
	if err := appendJSON(filepath.Join(s.root, "memory", "processed_v2.jsonl"), processedRecord{EpisodeID: ep.EpisodeID, ProcessedAt: model.Now(), Status: "ok"}); err != nil {
		return err
	}
	s.mu.Lock()
	s.processed[ep.EpisodeID] = true
	s.mu.Unlock()
	audit := brainAudit{EpisodeID: ep.EpisodeID, Scores: br.Evaluation, RetentionScore: score, Retained: retained, ModelID: cfg.MemoryLLM.ID}
	b, _ := json.Marshal(audit)
	s.audit.Printf("MEMORY_BRAIN %s", b)
	s.rebuildHot()
	if retained > 0 {
		s.ensureRerankerWarmAsync("semantic_persisted")
	}
	return nil
}

func canonicalKind(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "fact":
		return "fact"
	case "observation":
		return "observation"
	case "commitment":
		return "commitment"
	default:
		return "episode"
	}
}
func mockBrain(ep model.EpisodeCommitV2) model.MemoryBrainResult {
	txt := strings.TrimSpace(ep.UserInput.Text)
	resp := strings.TrimSpace(ep.Reaction.Dialogue)
	if txt == "" && ep.AppearanceChange != nil {
		txt = fmt.Sprintf("Appearance changed from %s to %s", ep.AppearanceChange.PreviousShellName, ep.AppearanceChange.CurrentShellName)
	}
	if ep.Material != nil {
		if txt == "" {
			txt = ep.Material.UserRequest
		}
		if resp == "" {
			resp = ep.Material.WebResponse
		}
	}
	trivial := .65
	if len([]rune(txt)) > 18 {
		trivial = .15
	}
	imp := .35
	if len([]rune(txt)) > 20 {
		imp = .72
	}
	emo := clamp(ep.AffectDelta.DeltaMax * 1.8)
	explicit := 0.0
	if strings.Contains(txt, "記住") || strings.Contains(strings.ToLower(txt), "remember") {
		explicit = .95
	}
	candText := txt
	if resp != "" {
		candText = txt + "；慕娜回應：" + truncate(resp, 80)
	}
	cands := []model.SemanticCandidate{}
	if strings.TrimSpace(candText) != "" {
		cands = append(cands, model.SemanticCandidate{Kind: "episode", Text: truncate(candText, 220), Confidence: .8})
	}
	return model.MemoryBrainResult{Evaluation: model.MemoryEvaluation{SemanticImportance: imp, EmotionalSalience: emo, Novelty: .65, PersonalRelevance: .55, Triviality: trivial, ExplicitImportance: explicit}, Summary: truncate(candText, 220), Candidates: cands}
}
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
func (s *service) newID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), s.seq.Add(1))
}
func dedupeStrings(x []string) []string {
	m := map[string]bool{}
	o := []string{}
	for _, v := range x {
		v = strings.TrimSpace(v)
		if v != "" && !m[v] {
			m[v] = true
			o = append(o, v)
		}
	}
	return o
}

func (s *service) storeSemantic(mem model.SemanticMemory, contradicts []string) error {
	p, err := s.prepareSemantic(context.Background(), semanticPlan{mem: mem, contradicts: contradicts})
	if err != nil {
		return err
	}
	return s.persistPreparedSemantic(p)
}

func (s *service) prepareSemantic(parent context.Context, plan semanticPlan) (preparedSemantic, error) {
	cfg := s.infer.Config()
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	vec, err := s.infer.Embed(ctx, plan.mem.Text)
	cancel()
	if err != nil {
		return preparedSemantic{}, err
	}
	mem := plan.mem
	mem.EmbeddingGeneration = cfg.EmbeddingGeneration
	mem.EmbeddingModel = cfg.Embedder.ID
	mem.EmbeddingDimension = len(vec)
	vr := vectorRecord{MemoryID: mem.ID, Generation: cfg.EmbeddingGeneration, Model: cfg.Embedder.ID, Dimension: len(vec), Vector: vec}
	return preparedSemantic{mem: mem, vector: vr, contradicts: append([]string(nil), plan.contradicts...)}, nil
}

func (s *service) persistPreparedSemantic(p preparedSemantic) error {
	mem, vr := p.mem, p.vector
	s.mu.Lock()
	for _, oldID := range p.contradicts {
		if old, ok := s.semantic[oldID]; ok && old.SupersededBy == "" {
			old.SupersededBy = mem.ID
			old.ValidUntil = mem.ValidFrom
			old.UpdatedAt = model.Now()
			s.semantic[oldID] = old
			_ = appendJSON(filepath.Join(s.root, "memory", "semantic_v2.jsonl"), old)
		}
	}
	s.semantic[mem.ID] = mem
	s.vectors[mem.ID] = vr
	s.mu.Unlock()
	if err := appendJSON(filepath.Join(s.root, "memory", "semantic_v2.jsonl"), mem); err != nil {
		return err
	}
	return appendJSON(filepath.Join(s.root, "memory", "index", fmt.Sprintf("vectors_g%d.jsonl", vr.Generation)), vr)
}
func (s *service) evidenceCount(text string, entities []string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, m := range s.semantic {
		if m.SupersededBy != "" || !s.activeSemantic(m) {
			continue
		}
		if similarity(text, m.Text) >= .32 || sharesEntity(entities, m.Entities) {
			n += maxInt(1, m.EvidenceCount)
		}
	}
	return n
}
func sharesEntity(a, b []string) bool {
	m := map[string]bool{}
	for _, x := range a {
		m[strings.ToLower(strings.TrimSpace(x))] = true
	}
	for _, x := range b {
		if m[strings.ToLower(strings.TrimSpace(x))] {
			return true
		}
	}
	return false
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *service) recallEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var q struct {
		Query    string                `json:"query"`
		Depth    string                `json:"depth,omitempty"`
		Envelope model.RequestEnvelope `json:"envelope"`
	}
	if err := httpjson.Decode(r, &q); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(q.Query) == "" {
		q.Query = q.Envelope.UserInput.Text
	}
	if !s.beginInferOp() {
		httpjson.Write(w, http.StatusServiceUnavailable, map[string]string{"error": "memory service is shutting down"})
		return
	}
	defer s.endInferOp()
	opctx, cancel := s.inferOpContext(r.Context())
	defer cancel()
	cap := s.recall(opctx, q.Query, q.Depth)
	httpjson.Write(w, 200, cap)
}

type scored struct {
	id    string
	score float64
}

func dot(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	x := 0.0
	for i := 0; i < n; i++ {
		x += a[i] * b[i]
	}
	return x
}

func rrfAdd(dst map[string]float64, ranked []scored, rrfK, maxSource int) {
	if rrfK <= 0 {
		rrfK = 60
	}
	if maxSource > 0 && len(ranked) > maxSource {
		ranked = ranked[:maxSource]
	}
	for i, x := range ranked {
		dst[x.id] += 1.0 / float64(rrfK+i+1)
	}
}

func estimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	n := 0
	latin := 0
	flushLatin := func() {
		if latin > 0 {
			n += (latin + 3) / 4
			latin = 0
		}
	}
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			flushLatin()
			n++
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			latin++
		case unicode.IsSpace(r):
			flushLatin()
		default:
			flushLatin()
			n++
		}
	}
	flushLatin()
	if n == 0 {
		return 1
	}
	return n
}

func truncateToTokenEstimate(text string, budget int) string {
	if budget <= 0 {
		return ""
	}
	if estimateTextTokens(text) <= budget {
		return text
	}
	r := []rune(text)
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if estimateTextTokens(string(r[:mid])+"…") <= budget {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo <= 0 {
		return ""
	}
	return strings.TrimSpace(string(r[:lo])) + "…"
}

func compactRecallMemory(m model.SemanticMemory) model.SemanticMemory {
	// Retrieval/index bookkeeping stays inside MemoryService. Foreground
	// cognition needs semantic content, kind, time and entities—not embedding
	// generations, retention policy scores or source journal identifiers.
	return model.SemanticMemory{
		ID: m.ID, Kind: m.Kind, Text: m.Text, CreatedAt: m.CreatedAt,
		Entities: append([]string(nil), m.Entities...), ValidFrom: m.ValidFrom,
	}
}

func addMemoryToCapsule(caps *model.MemoryCapsule, m model.SemanticMemory) {
	switch m.Kind {
	case "fact":
		caps.Facts = append(caps.Facts, m)
	case "observation":
		caps.Observations = append(caps.Observations, m)
	case "commitment":
		caps.Commitments = append(caps.Commitments, m)
	default:
		caps.Events = append(caps.Events, m)
	}
}

func packRecallContext(caps *model.MemoryCapsule, ids []string, sem map[string]model.SemanticMemory, budget int) (items, used int) {
	if budget <= 0 {
		return 0, 0
	}
	for _, id := range ids {
		if items >= 32 {
			break
		} // hard safety cap; token budget remains authoritative.
		m, ok := sem[id]
		if !ok {
			continue
		}
		cost := estimateTextTokens(m.Text) + 8 // small envelope/metadata allowance
		remaining := budget - used
		if remaining <= 8 {
			break
		}
		if cost > remaining {
			if items > 0 {
				continue
			}
			m.Text = truncateToTokenEstimate(m.Text, remaining-8)
			if strings.TrimSpace(m.Text) == "" {
				continue
			}
			cost = estimateTextTokens(m.Text) + 8
		}
		addMemoryToCapsule(caps, compactRecallMemory(m))
		used += cost
		items++
	}
	return items, used
}

func replayTurnCost(t model.DialogueTurn) int {
	return estimateTextTokens(t.User) + estimateTextTokens(t.Character) + 8
}

func truncateReplayTurn(t model.DialogueTurn, budget int) model.DialogueTurn {
	if budget <= 8 {
		return model.DialogueTurn{}
	}
	remaining := budget - 8
	uc, cc := estimateTextTokens(t.User), estimateTextTokens(t.Character)
	if uc+cc <= remaining {
		return t
	}
	if t.Character == "" {
		t.User = truncateToTokenEstimate(t.User, remaining)
		return t
	}
	if t.User == "" {
		t.Character = truncateToTokenEstimate(t.Character, remaining)
		return t
	}
	// Preserve both sides of a completed turn when possible.
	ub := remaining / 2
	cb := remaining - ub
	t.User = truncateToTokenEstimate(t.User, ub)
	t.Character = truncateToTokenEstimate(t.Character, cb)
	return t
}

// replayTail projects the append-only raw dialogue journal into chronological
// dialogue without vector search, embeddings, reranking, or Memory Brain work.
// It reads only a bounded tail window proportional to the requested context
// budget so archive growth does not make a recall scan progressively slower.
func (s *service) replayTail(ctx context.Context, budget int) ([]model.DialogueTurn, int, error) {
	if budget <= 0 {
		budget = 32768
	}
	path := filepath.Join(s.root, "memory", "raw_recent_v2.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if st.Size() == 0 {
		return nil, 0, nil
	}
	maxBytes := int64(budget*32 + 256*1024)
	if maxBytes < 256*1024 {
		maxBytes = 256 * 1024
	}
	if maxBytes > 8*1024*1024 {
		maxBytes = 8 * 1024 * 1024
	}
	start := st.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	buf := make([]byte, st.Size()-start)
	n, err := f.ReadAt(buf, start)
	if err != nil && n == 0 {
		return nil, 0, err
	}
	buf = buf[:n]
	text := string(buf)
	if start > 0 {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		} else {
			return nil, 0, nil
		}
	}
	lines := strings.Split(text, "\n")
	records := make([]rawRecent, 0, len(lines))
	completedUser := map[string]bool{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rr rawRecent
		if json.Unmarshal([]byte(line), &rr) != nil {
			continue
		}
		records = append(records, rr)
		id := strings.TrimSpace(rr.RequestID)
		if id == "" {
			id = strings.TrimSpace(rr.EpisodeID)
		}
		if id != "" && strings.TrimSpace(rr.User) != "" && (rr.Kind == "episode_completed" || strings.TrimSpace(rr.Character) != "") {
			completedUser[id] = true
		}
	}
	rev := make([]model.DialogueTurn, 0, 128)
	used := 0
	for i := len(records) - 1; i >= 0; i-- {
		select {
		case <-ctx.Done():
			return nil, used, ctx.Err()
		default:
		}
		rr := records[i]
		id := strings.TrimSpace(rr.RequestID)
		if id == "" {
			id = strings.TrimSpace(rr.EpisodeID)
		}
		user := strings.TrimSpace(rr.User)
		character := strings.TrimSpace(rr.Character)
		if rr.Kind == "user_accepted" && id != "" && completedUser[id] {
			continue
		}
		if user == "" && character == "" {
			continue // Replay is dialogue, not raw physical/UI event history.
		}
		t := model.DialogueTurn{Timestamp: rr.Timestamp, Source: "replay", User: user, Character: character}
		cost := replayTurnCost(t)
		remaining := budget - used
		if cost > remaining {
			if len(rev) > 0 {
				break
			}
			t = truncateReplayTurn(t, remaining)
			cost = replayTurnCost(t)
			if strings.TrimSpace(t.User) == "" && strings.TrimSpace(t.Character) == "" {
				break
			}
		}
		rev = append(rev, t)
		used += cost
		if used >= budget {
			break
		}
	}
	out := make([]model.DialogueTurn, len(rev))
	for i := range rev {
		out[len(rev)-1-i] = rev[i]
	}
	return out, used, nil
}

func (s *service) recall(ctx context.Context, query, requestedDepth string) model.MemoryCapsule {
	rc := s.retrieval()
	depth, preset := rc.preset(requestedDepth)
	caps := model.MemoryCapsule{Version: time.Now().UnixNano(), GeneratedAt: model.Now()}
	if strings.TrimSpace(query) == "" {
		return caps
	}
	deadlineCtx, cancelDeadline := context.WithTimeout(ctx, time.Duration(preset.RecallTimeoutMS)*time.Millisecond)
	defer cancelDeadline()
	if depth == "unbounded" {
		budget := preset.ReplayMaxContextTokens
		if budget <= 0 {
			budget = 32768
		}
		replay, used, err := s.replayTail(deadlineCtx, budget)
		caps.RecallMode = "replay"
		caps.Replay = replay
		if err != nil {
			caps.Degraded = true
			caps.DegradedReason = "replay unavailable"
		}
		s.log.Printf("RECALL depth=unbounded mode=replay replay_budget=%d replay_used_est=%d turns=%d degraded=%t", budget, used, len(replay), caps.Degraded)
		return caps
	}
	caps.RecallMode = "semantic"

	s.mu.RLock()
	sem := make(map[string]model.SemanticMemory, len(s.semantic))
	for k, v := range s.semantic {
		if v.SupersededBy == "" && s.activeSemantic(v) {
			sem[k] = v
		}
	}
	vecs := make(map[string]vectorRecord, len(s.vectors))
	for k, v := range s.vectors {
		vecs[k] = v
	}
	s.mu.RUnlock()
	if len(sem) == 0 {
		return caps
	}

	pool := preset.CandidatePool
	if pool <= 0 {
		pool = 300
	}
	fused := map[string]float64{}

	// Vector search is intentionally a high-recall mathematical source. It does
	// not decide final subjective relevance; the reranker sees the merged pool.
	qv, err := s.infer.Embed(deadlineCtx, rc.QueryInstruction+query)
	if err == nil {
		xs := make([]scored, 0, len(vecs))
		cfg := s.infer.Config()
		for id, v := range vecs {
			if _, ok := sem[id]; !ok || v.Generation != cfg.EmbeddingGeneration || v.Dimension != len(qv) {
				continue
			}
			xs = append(xs, scored{id, dot(qv, v.Vector)})
		}
		sort.Slice(xs, func(i, j int) bool { return xs[i].score > xs[j].score })
		rrfAdd(fused, xs, rc.RRFK, pool)
	} else {
		caps.Degraded = true
		caps.DegradedReason = "embedding unavailable"
	}

	lx := make([]scored, 0, len(sem))
	for id, m := range sem {
		if sc := similarity(query, m.Text); sc > 0 {
			lx = append(lx, scored{id, sc})
		}
	}
	sort.Slice(lx, func(i, j int) bool { return lx[i].score > lx[j].score })
	rrfAdd(fused, lx, rc.RRFK, pool)

	ex := make([]scored, 0)
	lq := strings.ToLower(query)
	for id, m := range sem {
		match := false
		for _, e := range m.Entities {
			if e != "" && strings.Contains(lq, strings.ToLower(e)) {
				match = true
				break
			}
		}
		if match {
			ex = append(ex, scored{id, m.RetentionScore})
		}
	}
	sort.Slice(ex, func(i, j int) bool { return ex[i].score > ex[j].score })
	rrfAdd(fused, ex, rc.RRFK, pool)

	ids := make([]string, 0, len(fused))
	for id := range fused {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if fused[ids[i]] == fused[ids[j]] {
			return sem[ids[i]].CreatedAt > sem[ids[j]].CreatedAt
		}
		return fused[ids[i]] > fused[ids[j]]
	})
	if len(ids) > pool {
		ids = ids[:pool]
	}
	candidateCount := len(ids)

	if len(ids) > 0 && deadlineCtx.Err() == nil {
		// Model startup is infrastructure, not search work. Never let a cold
		// reranker consume/kill itself on the foreground recall deadline.
		if !s.rerankerReady.Load() {
			s.ensureRerankerWarmAsync("recall_demand")
			caps.Degraded = true
			if caps.DegradedReason == "" {
				caps.DegradedReason = "reranker warming"
			}
		} else {
			docs := make([]string, 0, len(ids))
			for _, id := range ids {
				docs = append(docs, sem[id].Text)
			}
			order, e := s.infer.Rerank(deadlineCtx, query, docs)
			if e == nil && len(order) > 0 {
				nid := make([]string, 0, len(order))
				seen := map[string]bool{}
				for _, i := range order {
					if i >= 0 && i < len(ids) && !seen[ids[i]] {
						nid = append(nid, ids[i])
						seen[ids[i]] = true
					}
				}
				// A partial reranker response must not silently discard candidates.
				for _, id := range ids {
					if !seen[id] {
						nid = append(nid, id)
					}
				}
				ids = nid
			} else {
				s.rerankerReady.Store(false)
				s.ensureRerankerWarmAsync("rerank_failure")
				caps.Degraded = true
				if caps.DegradedReason == "" {
					caps.DegradedReason = "reranker unavailable"
				}
			}
		}
	}

	items, used := packRecallContext(&caps, ids, sem, preset.ContextBudgetTokens)
	s.log.Printf("RECALL depth=%s candidate_pool=%d candidates=%d context_budget=%d context_used_est=%d delivered=%d degraded=%t", depth, pool, candidateCount, preset.ContextBudgetTokens, used, items, caps.Degraded)
	return caps
}

func (s *service) rebuildHot() {
	s.mu.Lock()
	s.rebuildHotLocked()
	hot := s.hot
	s.mu.Unlock()
	go s.pushHotSnapshot(hot)
}
func (s *service) rebuildHotLocked() {
	rc := s.retention()
	th := rc.Thresholds["hot_memory_candidate"]
	if th <= 0 {
		th = .72
	}
	items := []model.SemanticMemory{}
	for _, m := range s.semantic {
		if m.SupersededBy == "" && s.activeSemantic(m) && m.RetentionScore >= th {
			items = append(items, m)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RetentionScore == items[j].RetentionScore {
			return items[i].CreatedAt > items[j].CreatedAt
		}
		return items[i].RetentionScore > items[j].RetentionScore
	})
	if len(items) > rc.HotMemoryMaxItems {
		items = items[:rc.HotMemoryMaxItems]
	}
	s.hotSeq++
	s.hot = model.HotMemorySnapshot{Version: s.hotSeq, UpdatedAt: model.Now(), Items: items}
}
func (s *service) pushHot() { s.mu.RLock(); h := s.hot; s.mu.RUnlock(); s.pushHotSnapshot(h) }
func (s *service) pushHotSnapshot(h model.HotMemorySnapshot) {
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	_ = httpjson.Post(ctx, "http://127.0.0.1:8770/internal/memory/hot-v2", h, nil)
}
func (s *service) hotEndpoint(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.hot
	s.mu.RUnlock()
	httpjson.Write(w, 200, h)
}

func (s *service) statusEndpoint(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	httpjson.Write(w, 200, map[string]any{"ok": true, "version": version, "episodes": len(s.episodes), "semantic": len(s.semantic), "vectors": len(s.vectors), "hot": s.hot, "unprocessed": len(s.episodes) - len(s.processed)})
}
func (s *service) modelsStatus(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, 200, map[string]any{"memory_llm": s.infer.Status("memory_llm"), "embedder": s.infer.Status("embedder"), "reranker": s.infer.Status("reranker")})
}
func (s *service) prepareModels(w http.ResponseWriter, r *http.Request) {
	if !s.beginInferOp() {
		httpjson.Write(w, http.StatusServiceUnavailable, map[string]string{"error": "memory service is shutting down"})
		return
	}
	go func() {
		defer s.endInferOp()
		base := s.opsCtx
		if base == nil {
			base = context.Background()
		}
		for _, role := range []string{"memory_llm", "embedder", "reranker"} {
			ctx, cancel := context.WithTimeout(base, 30*time.Minute)
			err := s.infer.EnsureServer(ctx, role)
			cancel()
			if err != nil {
				s.log.Printf("MODEL_PREP role=%s error=%v", role, err)
			} else {
				s.log.Printf("MODEL_PREP role=%s ready=true", role)
			}
		}
	}()
	httpjson.Write(w, 202, map[string]any{"ok": true, "preparing": true})
}

func summaryRules(raw json.RawMessage) model.CharacterSummaryRules {
	c := model.CharacterSummaryRules{FormatVersion: 2, MaxItemsPerSection: 4, MaxItemChars: 140, RetryMaxItemsPerSection: 2, RetryMaxItemChars: 100, ForbiddenDynamicTerms: []string{"current affect", "current expression", "current gaze", "current pose", "touch salience", "recent dialogue", "Hot Memory"}}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &c)
	}
	if c.MaxItemsPerSection <= 0 {
		c.MaxItemsPerSection = 4
	}
	if c.MaxItemsPerSection > 12 {
		c.MaxItemsPerSection = 12
	}
	if c.MaxItemChars <= 0 {
		c.MaxItemChars = 140
	}
	if c.MaxItemChars > 400 {
		c.MaxItemChars = 400
	}
	if c.RetryMaxItemsPerSection <= 0 || c.RetryMaxItemsPerSection > c.MaxItemsPerSection {
		c.RetryMaxItemsPerSection = 2
		if c.MaxItemsPerSection < 2 {
			c.RetryMaxItemsPerSection = c.MaxItemsPerSection
		}
	}
	if c.RetryMaxItemChars <= 0 || c.RetryMaxItemChars > c.MaxItemChars {
		c.RetryMaxItemChars = 100
		if c.MaxItemChars < 100 {
			c.RetryMaxItemChars = c.MaxItemChars
		}
	}
	return c
}

func (s *service) profileCompile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var in struct {
		Character  string          `json:"character"`
		Appearance string          `json:"appearance"`
		Guide      string          `json:"guide"`
		Rules      json.RawMessage `json:"rules"`
	}
	if err := httpjson.Decode(r, &in); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if !s.beginInferOp() {
		httpjson.Write(w, http.StatusServiceUnavailable, map[string]string{"error": "memory service is shutting down"})
		return
	}
	defer s.endInferOp()
	opctx, opcancel := s.inferOpContext(r.Context())
	defer opcancel()
	rules := summaryRules(in.Rules)
	cfg := s.infer.Config()
	var out model.CharacterSummaryProposal
	modelID := cfg.MemoryLLM.ID
	if cfg.Mock {
		out = mockProfile(in.Character, in.Appearance, rules)
	} else {
		system := fmt.Sprintf(`%s
Return one compact JSON object with exactly these array fields: identity, stable_behavior, world_context, stable_appearance, unknowns.
Hard limits: at most %d items per field; each item <= %d Unicode characters; no prose outside JSON; do not repeat source text; do not include dynamic Runtime state, touch policy, memory, or author examples.`, in.Guide, rules.MaxItemsPerSection, rules.MaxItemChars)
		user := "[CHARACTER]\n" + in.Character + "\n[APPEARANCE]\n" + in.Appearance + "\n[RULES]\n" + string(in.Rules)
		ctx1, cancel1 := context.WithTimeout(opctx, 70*time.Second)
		err := s.infer.ChatJSON(ctx1, system, user, &out)
		cancel1()
		if err != nil {
			retrySystem := fmt.Sprintf("%s\nRETRY MODE: output at most %d items per field, each <= %d Unicode characters, and terminate the JSON object immediately.", system, rules.RetryMaxItemsPerSection, rules.RetryMaxItemChars)
			ctx2, cancel2 := context.WithTimeout(opctx, 45*time.Second)
			err2 := s.infer.ChatJSON(ctx2, retrySystem, user, &out)
			cancel2()
			if err2 != nil {
				out = mockProfile(in.Character, in.Appearance, rules)
				modelID = "canonical-fallback-after-local-json-error"
				s.log.Printf("PROFILE_COMPILE_LOCAL_DEGRADED first=%v retry=%v fallback=canonical", err, err2)
			}
		}
	}
	out = normalizeProfileProposal(out, rules)
	httpjson.Write(w, 200, map[string]any{"proposal": out, "model_id": modelID, "generated_at": model.Now()})
}

func normalizeProfileProposal(p model.CharacterSummaryProposal, rules model.CharacterSummaryRules) model.CharacterSummaryProposal {
	norm := func(xs []string) []string {
		out := []string{}
		seen := map[string]bool{}
		for _, x := range xs {
			x = strings.TrimSpace(x)
			if x == "" || seen[x] {
				continue
			}
			seen[x] = true
			out = append(out, truncate(x, rules.MaxItemChars))
			if len(out) >= rules.MaxItemsPerSection {
				break
			}
		}
		return out
	}
	p.Identity = norm(p.Identity)
	p.StableBehavior = norm(p.StableBehavior)
	p.WorldContext = norm(p.WorldContext)
	p.StableAppearance = norm(p.StableAppearance)
	p.Unknowns = norm(p.Unknowns)
	return p
}
func mockProfile(ch, ap string, rules model.CharacterSummaryRules) model.CharacterSummaryProposal {
	first := func(s string) string {
		for _, ln := range strings.Split(s, "\n") {
			ln = strings.TrimSpace(strings.TrimLeft(ln, "#-* "))
			if ln != "" {
				return truncate(ln, rules.MaxItemChars)
			}
		}
		return ""
	}
	p := model.CharacterSummaryProposal{}
	if x := first(ch); x != "" {
		p.Identity = []string{x}
	}
	if x := first(ap); x != "" {
		p.StableAppearance = []string{x}
	}
	return p
}

func (s *service) observe(w http.ResponseWriter, r *http.Request) {
	var x any
	if err := httpjson.Decode(r, &x); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	} // observation is raw-recent only by design
	_ = appendJSON(filepath.Join(s.root, "memory", "raw_observe_v2.jsonl"), map[string]any{"timestamp": model.Now(), "value": x})
	httpjson.Write(w, 200, map[string]any{"ok": true, "semantic": false})
}

func (s *service) migrateFix8RevalidateProcessed() {
	marker := filepath.Join(s.root, "memory", "migrations", "fix8_revalidate_processed.done")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	s.mu.Lock()
	hasSemantic := map[string]bool{}
	for _, m := range s.semantic {
		if !s.activeSemantic(m) {
			continue
		}
		for _, id := range m.SourceEpisodeIDs {
			hasSemantic[id] = true
		}
	}
	ids := []string{}
	for _, id := range s.episodeOrder {
		if !s.processed[id] || hasSemantic[id] {
			continue
		}
		ep := s.episodes[id]
		if s.preEpoch(ep) {
			continue
		}
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		if err := appendJSON(filepath.Join(s.root, "memory", "processed_v2.jsonl"), processedRecord{EpisodeID: id, ProcessedAt: model.Now(), Status: "revalidate_fix8"}); err != nil {
			s.log.Printf("MEMORY_FIX8_REVALIDATE_ERROR episode=%s err=%v", id, err)
			return
		}
		s.mu.Lock()
		delete(s.processed, id)
		s.mu.Unlock()
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err == nil {
		_ = os.WriteFile(marker, []byte(fmt.Sprintf("revalidate_count=%d\nat=%s\n", len(ids), model.Now())), 0644)
	}
	s.audit.Printf("MEMORY_FIX8_REVALIDATE count=%d reason=legacy_processed_without_semantic", len(ids))
}

func (s *service) load() {
	loadLast := func(path string, fn func([]byte)) {
		f, e := os.Open(path)
		if e != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		buf := make([]byte, 64*1024)
		sc.Buffer(buf, 8<<20)
		for sc.Scan() {
			fn(append([]byte(nil), sc.Bytes()...))
		}
	}
	loadLast(filepath.Join(s.root, "memory", "episodes_v2.jsonl"), func(b []byte) {
		var x model.EpisodeCommitV2
		if json.Unmarshal(b, &x) == nil && x.EpisodeID != "" {
			if _, ok := s.episodes[x.EpisodeID]; !ok {
				s.episodeOrder = append(s.episodeOrder, x.EpisodeID)
			}
			s.episodes[x.EpisodeID] = x
		}
	})
	loadLast(filepath.Join(s.root, "memory", "semantic_v2.jsonl"), func(b []byte) {
		var x model.SemanticMemory
		if json.Unmarshal(b, &x) == nil && x.ID != "" {
			s.semantic[x.ID] = x
		}
	})
	// Only the configured generation is active. Mixed generations are retained on disk but rejected from this in-memory index.
	cfg := s.infer.Config()
	path := filepath.Join(s.root, "memory", "index", fmt.Sprintf("vectors_g%d.jsonl", cfg.EmbeddingGeneration))
	loadLast(path, func(b []byte) {
		var x vectorRecord
		if json.Unmarshal(b, &x) == nil && x.MemoryID != "" && x.Generation == cfg.EmbeddingGeneration && x.Dimension == cfg.EmbeddingDimension {
			s.vectors[x.MemoryID] = x
		}
	})
	loadLast(filepath.Join(s.root, "memory", "processed_v2.jsonl"), func(b []byte) {
		var x processedRecord
		if json.Unmarshal(b, &x) != nil || x.EpisodeID == "" {
			return
		}
		applyProcessedStatus(s.processed, x)
	})
	s.log.Printf("LOAD_V2 episodes=%d semantic=%d vectors=%d processed=%d generation=%d epoch=%s", len(s.episodes), len(s.semantic), len(s.vectors), len(s.processed), cfg.EmbeddingGeneration, s.epoch.ID)
}

func tokens(s string) map[string]float64 {
	out := map[string]float64{}
	var latin strings.Builder
	flush := func() {
		x := strings.ToLower(strings.TrimSpace(latin.String()))
		if len(x) >= 2 {
			out[x]++
		}
		latin.Reset()
	}
	var prev rune
	hasPrev := false
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			flush()
			if hasPrev {
				out[string([]rune{prev, r})]++
			}
			out[string(r)]++
			prev = r
			hasPrev = true
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			latin.WriteRune(unicode.ToLower(r))
			hasPrev = false
		} else {
			flush()
			hasPrev = false
		}
	}
	flush()
	return out
}
func similarity(a, b string) float64 {
	aa, bb := tokens(a), tokens(b)
	if len(aa) == 0 || len(bb) == 0 {
		return 0
	}
	inter, den := 0.0, 0.0
	for k, av := range aa {
		den += av
		if bv := bb[k]; bv > 0 {
			if av < bv {
				inter += av
			} else {
				inter += bv
			}
		}
	}
	for _, bv := range bb {
		den += bv
	}
	if den == 0 {
		return 0
	}
	return 2 * inter / den
}
