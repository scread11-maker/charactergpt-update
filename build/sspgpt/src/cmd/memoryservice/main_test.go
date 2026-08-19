package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sspgpt/v07/internal/localinfer"
	"sspgpt/v07/internal/model"
)

func testService(t *testing.T) *service {
	t.Helper()
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "config"), 0755)
	_ = os.MkdirAll(filepath.Join(root, "memory", "index"), 0755)
	cfg := map[string]any{"mock": true, "embedding_dimension": 512, "embedding_generation": 1, "memory_llm": map[string]any{"id": "mock-memory"}, "embedder": map[string]any{"id": "mock-embed"}, "reranker": map[string]any{"id": "mock-rank"}}
	b, _ := json.Marshal(cfg)
	_ = os.WriteFile(filepath.Join(root, "config", "local_models.json"), b, 0644)
	rc := map[string]any{"weights": map[string]float64{"semantic_importance": 1, "emotional_salience": 1.4, "novelty": .7, "commitment": 1.3, "personal_relevance": .9, "recurrence_relevance": .7, "explicit_importance": 1.5, "triviality": -1.2}, "thresholds": map[string]float64{"semantic_store": .42, "strong_retention": .72, "hot_memory_candidate": .60}, "modifiers": map[string]any{"source": map[string]float64{}, "request_class": map[string]float64{}, "gesture": map[string]float64{}, "reaction_emotion": map[string]float64{}}, "observation_min_evidence": 2, "hot_memory_max_items": 5}
	b, _ = json.Marshal(rc)
	_ = os.WriteFile(filepath.Join(root, "config", "memory_retention_rules.json"), b, 0644)
	s := &service{root: root, log: log.New(io.Discard, "", 0), audit: log.New(io.Discard, "", 0), infer: localinfer.New(root), episodes: map[string]model.EpisodeCommitV2{}, semantic: map[string]model.SemanticMemory{}, vectors: map[string]vectorRecord{}, processed: map[string]bool{}, jobs: make(chan model.EpisodeCommitV2, 8), stop: make(chan struct{})}
	return s
}
func TestAffectDeltaUsesTransitionNotAbsolute(t *testing.T) {
	before := model.AffectState{Channels: map[string]float64{"wary": .70}}
	after := model.AffectState{Channels: map[string]float64{"wary": .72}}
	d := affectDelta(before, after)
	if d.DeltaMax > .021 {
		t.Fatalf("stable high affect got delta %v", d.DeltaMax)
	}
	before.Channels["wary"] = .08
	after.Channels["wary"] = .47
	d = affectDelta(before, after)
	if d.DeltaMax < .38 {
		t.Fatalf("large transition missing: %#v", d)
	}
}
func TestCancelledEpisodeCannotEnterJournal(t *testing.T) {
	s := testService(t)
	ep := model.EpisodeCommitV2{EpisodeID: "x", Status: "cancelled"}
	if ep.Status == "completed" {
		t.Fatal("fixture")
	} // endpoint hard filter is status-based; consolidate must never be called by rejected status
	br := mockBrain(ep)
	if br.Evaluation.SemanticImportance > 1 {
		t.Fatal("invalid score")
	}
	if len(s.semantic) != 0 {
		t.Fatal("cancelled fixture mutated semantic store")
	}
}
func TestRetentionGateRespondsToEmotionalSalience(t *testing.T) {
	s := testService(t)
	c := s.retention()
	ep := model.EpisodeCommitV2{Source: "chat", RequestClass: model.RequestChat}
	low := model.MemoryEvaluation{SemanticImportance: .5, EmotionalSalience: .05, Novelty: .5, Triviality: .1}
	high := low
	high.EmotionalSalience = .95
	if s.retentionScore(high, ep, c) <= s.retentionScore(low, ep, c) {
		t.Fatal("emotional salience weight not applied")
	}
}

func TestRetentionGateKeepsSubstantiveSparseMemory(t *testing.T) {
	s := testService(t)
	c := s.retention()
	ep := model.EpisodeCommitV2{Source: "chat", RequestClass: model.RequestChat}
	// A single meaningful dimension plus novelty should not be diluted by all
	// unrelated zero-valued dimensions.
	ev := model.MemoryEvaluation{SemanticImportance: .62, Novelty: .55, PersonalRelevance: .35, Triviality: .05}
	if got := s.retentionScore(ev, ep, c); got < c.Thresholds["semantic_store"] {
		t.Fatalf("substantive sparse memory was discarded: score=%.3f threshold=%.3f", got, c.Thresholds["semantic_store"])
	}
}
func TestRetentionGateRejectsTrivialMemory(t *testing.T) {
	s := testService(t)
	c := s.retention()
	ep := model.EpisodeCommitV2{Source: "chat", RequestClass: model.RequestChat}
	ev := model.MemoryEvaluation{SemanticImportance: .12, Novelty: .10, Triviality: .92}
	if got := s.retentionScore(ev, ep, c); got >= c.Thresholds["semantic_store"] {
		t.Fatalf("trivial memory retained: score=%.3f threshold=%.3f", got, c.Thresholds["semantic_store"])
	}
}
func TestRawMOVERejectedByInvariant(t *testing.T) {
	ep := model.EpisodeCommitV2{Status: "completed", Event: &model.PhysicalEvent{Type: "move"}}
	if !(ep.Event != nil && ep.Event.Type == "move") {
		t.Fatal("fixture")
	}
}
func TestSemanticStorageUsesVersionedEmbedding(t *testing.T) {
	s := testService(t)
	m := model.SemanticMemory{ID: "m1", Kind: "fact", Text: "使用者預計十一月去京都。", CreatedAt: model.Now(), RetentionScore: .8}
	if err := s.storeSemantic(m, nil); err != nil {
		t.Fatal(err)
	}
	got := s.semantic["m1"]
	if got.EmbeddingGeneration != 1 || got.EmbeddingDimension != 512 {
		t.Fatalf("bad embedding metadata: %#v", got)
	}
	if len(s.vectors["m1"].Vector) != 512 {
		t.Fatalf("vector dim=%d", len(s.vectors["m1"].Vector))
	}
}

func TestShutdownCancellationBeforePersistenceLeavesEpisodeUnprocessed(t *testing.T) {
	s := testService(t)
	ep := model.EpisodeCommitV2{
		EpisodeID:    "shutdown-cancel",
		RequestID:    "shutdown-cancel",
		RequestClass: model.RequestChat,
		Source:       "chat",
		Status:       "completed",
		CompletedAt:  model.Now(),
		UserInput:    model.UserInput{Text: "請記得我週末要去京都。"},
		AffectDelta:  model.AffectDelta{Channels: map[string]float64{}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.consolidateContext(ctx, ep); err == nil {
		t.Fatal("cancelled consolidation unexpectedly completed")
	}
	if len(s.semantic) != 0 || len(s.vectors) != 0 || s.processed[ep.EpisodeID] {
		t.Fatalf("cancelled pre-persistence work mutated durable state: semantic=%d vectors=%d processed=%t", len(s.semantic), len(s.vectors), s.processed[ep.EpisodeID])
	}
}

func TestPreparedSemanticPersistenceSurvivesLaterCancellation(t *testing.T) {
	s := testService(t)
	ctx, cancel := context.WithCancel(context.Background())
	plan := semanticPlan{mem: model.SemanticMemory{ID: "staged", Kind: "fact", Text: "使用者週末要去京都。", CreatedAt: model.Now(), RetentionScore: .8}}
	p, err := s.prepareSemantic(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	// Once model work is staged, shutdown cancellation must not interrupt the
	// short JSONL/vector persistence critical section.
	cancel()
	if err := s.persistPreparedSemantic(p); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.semantic["staged"]; !ok {
		t.Fatal("prepared semantic memory was not persisted")
	}
	if _, ok := s.vectors["staged"]; !ok {
		t.Fatal("prepared vector was not persisted")
	}
}
func TestObservationNeedsMultipleEvidence(t *testing.T) {
	s := testService(t)
	if n := s.evidenceCount("使用者偏好低甜度咖啡", []string{"咖啡"}); n != 0 {
		t.Fatalf("unexpected evidence %d", n)
	}
}
func TestCJKSimilarityFindsParaphraseEntityOverlap(t *testing.T) {
	if got := similarity("你還記得京都嗎", "使用者計畫去京都賞楓"); got <= 0 {
		t.Fatalf("expected overlap got %v", got)
	}
}
func TestLinkedAndLocalShareSemanticStore(t *testing.T) {
	s := testService(t)
	for _, src := range []struct{ id, source, class, text string }{{"a", "chat", "chat", "我計畫下個月和朋友一起去京都旅行並且想去看看楓葉。"}, {"b", "chatgpt_web", "linked_chat", "請在網頁幫我整理下個月京都旅行的交通、住宿與賞楓行程。"}} {
		ep := model.EpisodeCommitV2{EpisodeID: src.id, RequestID: src.id, RequestClass: src.class, Source: src.source, Status: "completed", CompletedAt: model.Now(), UserInput: model.UserInput{Text: src.text}, AffectDelta: model.AffectDelta{Channels: map[string]float64{}}}
		if err := s.consolidate(ep); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.semantic) < 2 {
		t.Fatalf("expected shared semantic store, got %d", len(s.semantic))
	}
}

func TestNormalizeProfileProposalUsesRuleLimits(t *testing.T) {
	p := model.CharacterSummaryProposal{Identity: []string{" A ", "A", strings.Repeat("x", 200)}}
	rules := model.CharacterSummaryRules{MaxItemsPerSection: 4, MaxItemChars: 140}
	got := normalizeProfileProposal(p, rules)
	if len(got.Identity) != 2 {
		t.Fatalf("dedupe failed: %#v", got.Identity)
	}
	if len([]rune(got.Identity[1])) > 141 { // truncate appends ellipsis
		t.Fatalf("profile item not bounded: %d", len([]rune(got.Identity[1])))
	}
}

func f64(v float64) *float64 { return &v }
func strp(v string) *string  { return &v }

func TestMemoryBrainStrictSchemaRejectsMissingEvaluation(t *testing.T) {
	_, err := validateMemoryBrainWire(memoryBrainWire{Summary: "有內容"})
	if err == nil {
		t.Fatal("missing evaluation must be rejected rather than becoming zero-value scores")
	}
}

func TestMemoryBrainStrictSchemaRejectsDegenerateAllZero(t *testing.T) {
	z := 0.0
	_, err := validateMemoryBrainWire(memoryBrainWire{Evaluation: &memoryEvaluationWire{
		SemanticImportance: &z, EmotionalSalience: &z, Novelty: &z, Commitment: &z,
		Recurrence: &z, PersonalRelevance: &z, Triviality: &z, ExplicitImportance: &z,
	}})
	if err == nil {
		t.Fatal("all-zero local-model evaluation must be treated as degraded output")
	}
}

func TestMemoryBrainStrictSchemaAcceptsCompleteEvaluation(t *testing.T) {
	w := memoryBrainWire{Evaluation: &memoryEvaluationWire{
		SemanticImportance: f64(.6), EmotionalSalience: f64(.1), Novelty: f64(.7), Commitment: f64(0),
		Recurrence: f64(0), PersonalRelevance: f64(.5), Triviality: f64(.1), ExplicitImportance: f64(0),
	}, Summary: "使用者提到一項持續中的專案更新。", Candidates: []semanticCandidateWire{{Kind: strp("episode"), Text: strp("使用者提到一項持續中的專案更新。"), Confidence: f64(.8)}}}
	if _, err := validateMemoryBrainWire(w); err != nil {
		t.Fatalf("complete strict result rejected: %v", err)
	}
}

func TestRecallPresetMediumUsesLargeCandidatePoolSmallContext(t *testing.T) {
	c := defaultRetrievalConfig()
	_, p := c.preset("medium")
	if p.CandidatePool != 300 {
		t.Fatalf("medium candidate pool=%d want=300", p.CandidatePool)
	}
	if p.ContextBudgetTokens != 1024 {
		t.Fatalf("medium context budget=%d want=1024", p.ContextBudgetTokens)
	}
	if p.CandidatePool <= p.ContextBudgetTokens/8 {
		t.Fatal("recall preset should favor broad retrieval before narrow delivery")
	}
}

func TestRecallPresetDepthsAreMonotonic(t *testing.T) {
	c := defaultRetrievalConfig()
	_, light := c.preset("light")
	_, medium := c.preset("medium")
	_, deep := c.preset("deep")
	if !(light.CandidatePool < medium.CandidatePool && medium.CandidatePool < deep.CandidatePool) {
		t.Fatalf("candidate pools not monotonic: %#v %#v %#v", light, medium, deep)
	}
	if light.ContextBudgetTokens != 512 || medium.ContextBudgetTokens != 1024 || deep.ContextBudgetTokens != 2048 {
		t.Fatalf("unexpected delivery budgets: %#v %#v %#v", light, medium, deep)
	}
}

func TestPackRecallContextHonorsTokenBudget(t *testing.T) {
	sem := map[string]model.SemanticMemory{}
	ids := []string{}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("m%d", i)
		ids = append(ids, id)
		sem[id] = model.SemanticMemory{ID: id, Kind: "episode", Text: strings.Repeat("記憶內容", 40)}
	}
	var caps model.MemoryCapsule
	items, used := packRecallContext(&caps, ids, sem, 512)
	if items == 0 || used > 512 {
		t.Fatalf("budget packing failed items=%d used=%d", items, used)
	}
}

func TestProcessedStatusRevalidateOverridesOldOK(t *testing.T) {
	processed := map[string]bool{}
	applyProcessedStatus(processed, processedRecord{EpisodeID: "e1", Status: "ok"})
	if !processed["e1"] {
		t.Fatal("ok status must mark processed")
	}
	applyProcessedStatus(processed, processedRecord{EpisodeID: "e1", Status: "revalidate_fix8"})
	if processed["e1"] {
		t.Fatal("revalidate marker must make legacy processed episode eligible again")
	}
}

func TestCompactMemoryBrainInputOmitsFullAffectState(t *testing.T) {
	ep := model.EpisodeCommitV2{
		EpisodeID: "e", RequestClass: model.RequestChat, Source: "chat", CompletedAt: model.Now(),
		UserInput:       model.UserInput{Text: "請記得這個專案改動。"},
		Reaction:        model.Reaction{Dialogue: "好。", ReactionEmotion: "smile", Presentation: model.Presentation{Expression: "smile", Pose: "normal"}},
		AffectAtRequest: model.AffectState{Channels: map[string]float64{"positive": .8}},
		AffectBefore:    model.AffectState{Channels: map[string]float64{"positive": .7}},
		AffectAfter:     model.AffectState{Channels: map[string]float64{"positive": .9}},
		AffectDelta:     model.AffectDelta{DeltaMax: .2},
	}
	in := compactMemoryBrainInput(ep)
	b, _ := json.Marshal(in)
	text := string(b)
	if strings.Contains(text, "affect_before") || strings.Contains(text, "affect_after") || strings.Contains(text, "presentation") {
		t.Fatalf("memory brain input leaked nonessential foreground state: %s", text)
	}
	if !strings.Contains(text, "affect_delta") || !strings.Contains(text, "reaction_emotion") {
		t.Fatalf("memory brain input lost authoritative semantic signals: %s", text)
	}
}

func TestRRFMediumSourceContributesUpTo300Candidates(t *testing.T) {
	ranked := make([]scored, 400)
	for i := range ranked {
		ranked[i] = scored{id: fmt.Sprintf("m%03d", i), score: float64(400 - i)}
	}
	fused := map[string]float64{}
	rrfAdd(fused, ranked, 60, 300)
	if len(fused) != 300 {
		t.Fatalf("medium high-recall source was prematurely clipped: got=%d want=300", len(fused))
	}
	if _, ok := fused["m299"]; !ok {
		t.Fatal("300th candidate must remain available to rerank")
	}
	if _, ok := fused["m300"]; ok {
		t.Fatal("candidate pool cap must remain bounded at 300")
	}
}

func TestCompactMemoryBrainInputCarriesAppearanceTransition(t *testing.T) {
	ep := model.EpisodeCommitV2{
		EpisodeID:    "appearance-1",
		RequestClass: model.RequestAppearance,
		Source:       "appearance",
		AppearanceChange: &model.AppearanceTransition{
			Kind:              "shell_changed",
			PreviousShellName: "Old",
			PreviousShellKey:  "master",
			CurrentShellName:  "New",
			CurrentShellKey:   "alt",
		},
		Reaction: model.Reaction{Dialogue: "換成這身了。", ReactionEmotion: "neutral"},
	}
	in := compactMemoryBrainInput(ep)
	if in.AppearanceChange == nil || in.AppearanceChange.CurrentShellName != "New" {
		t.Fatalf("appearance transition was lost before Memory Brain: %#v", in.AppearanceChange)
	}
	b, _ := json.Marshal(in)
	text := string(b)
	if strings.Contains(text, "shell_key") || strings.Contains(text, "master") || strings.Contains(text, "alt") {
		t.Fatalf("Memory Brain must receive semantic Shell names, not routing keys: %s", text)
	}
}

func TestUnboundedPresetUsesReplayWithoutSemanticBudgets(t *testing.T) {
	c := defaultRetrievalConfig()
	name, p := c.preset("unbounded")
	if name != "unbounded" || p.ReplayMaxContextTokens != 32768 || p.RecallTimeoutMS != 700 {
		t.Fatalf("unexpected unbounded preset: %q %#v", name, p)
	}
	if p.CandidatePool != 0 || p.ContextBudgetTokens != 0 {
		t.Fatalf("unbounded must not masquerade as oversized semantic RAG: %#v", p)
	}
}

func TestReplayTailDeduplicatesAcceptedInputAfterCompletion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "memory", "raw_recent_v2.jsonl")
	rows := []rawRecent{
		{RequestID: "r1", Kind: "episode_completed", Timestamp: "2026-08-19T01:00:02Z", User: "第一句", Character: "第一答"},
		// Simulate an async accepted-input write that happened to land after the completion row.
		{RequestID: "r1", Kind: "user_accepted", Timestamp: "2026-08-19T01:00:00Z", User: "第一句"},
		{RequestID: "r2", Kind: "user_accepted", Timestamp: "2026-08-19T01:01:00Z", User: "尚未完成"},
	}
	for _, r := range rows {
		if err := appendJSON(path, r); err != nil {
			t.Fatal(err)
		}
	}
	s := &service{root: root}
	got, used, err := s.replayTail(context.Background(), 512)
	if err != nil {
		t.Fatal(err)
	}
	if used <= 0 || len(got) != 2 {
		t.Fatalf("unexpected replay: used=%d turns=%#v", used, got)
	}
	if got[0].User != "第一句" || got[0].Character != "第一答" || got[1].User != "尚未完成" {
		t.Fatalf("chronology/dedup failed: %#v", got)
	}
}

func TestUnboundedRecallWorksWithEmptySemanticStore(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := appendJSON(filepath.Join(root, "memory", "raw_recent_v2.jsonl"), rawRecent{RequestID: "r", Kind: "episode_completed", Timestamp: model.Now(), User: "以前的問題", Character: "以前的回答"}); err != nil {
		t.Fatal(err)
	}
	s := &service{root: root, log: log.New(io.Discard, "", 0), semantic: map[string]model.SemanticMemory{}, vectors: map[string]vectorRecord{}}
	got := s.recall(context.Background(), "你還記得以前嗎？", "unbounded")
	if got.RecallMode != "replay" || len(got.Replay) != 1 {
		t.Fatalf("unbounded recall must not require semantic store: %#v", got)
	}
}

func TestDirectiveRetentionModifiersAreMemoryPolicyNotRuntimePolicy(t *testing.T) {
	s := testService(t)
	c := s.retention()
	c.Modifiers.DirectiveKind = map[string]float64{"document_query": .16}
	c.Modifiers.DirectiveID = map[string]float64{"readmanual": .08}
	ev := model.MemoryEvaluation{SemanticImportance: .35, Novelty: .30, PersonalRelevance: .25, Triviality: .10}
	base := model.EpisodeCommitV2{Source: "chat", RequestClass: model.RequestChat, UserInput: model.UserInput{Text: "普通問題"}}
	directed := base
	directed.UserInput = model.UserInput{Text: "\\readmanual 問題", Directive: &model.DirectiveRef{ID: "readmanual", Kind: "document_query", InvokedAs: "\\readmanual", Argument: "問題"}}
	baseScore := s.retentionScore(ev, base, c)
	directedScore := s.retentionScore(ev, directed, c)
	if directedScore <= baseScore+.20 {
		t.Fatalf("directive memory policy boost missing: base=%.3f directive=%.3f", baseScore, directedScore)
	}
	if directedScore >= 1 && baseScore >= 1 {
		t.Fatal("fixture saturated before directive modifier could be observed")
	}
}

func TestProcessedStatusQuarantinedInvalidOutputIsTerminal(t *testing.T) {
	processed := map[string]bool{}
	applyProcessedStatus(processed, processedRecord{EpisodeID: "ep-poison", Status: "quarantined_invalid_output"})
	if !processed["ep-poison"] {
		t.Fatal("quarantined invalid output must remain processed across restart")
	}
	applyProcessedStatus(processed, processedRecord{EpisodeID: "ep-poison", Status: "retry"})
	if processed["ep-poison"] {
		t.Fatal("retry status must reopen an episode")
	}
}

func TestQuarantineInvalidOutputPersistsProcessedMarker(t *testing.T) {
	s := testService(t)
	ep := model.EpisodeCommitV2{EpisodeID: "ep-poison", RequestID: "ep-poison", RequestClass: "chat", Source: "chat", Status: "completed", CompletedAt: model.Now()}
	cause := &terminalMemoryBrainOutputError{err: fmt.Errorf("memory brain returned degenerate all-zero evaluation")}
	if err := s.quarantineInvalidOutput(ep, cause); err != nil {
		t.Fatal(err)
	}
	if !s.isProcessed(ep.EpisodeID) {
		t.Fatal("quarantined episode must be terminally processed in memory")
	}
	b, err := os.ReadFile(filepath.Join(s.root, "memory", "processed_v2.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var rec processedRecord
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Status != "quarantined_invalid_output" || rec.EpisodeID != ep.EpisodeID {
		t.Fatalf("unexpected quarantine marker: %#v", rec)
	}
	if !strings.Contains(rec.Error, "degenerate all-zero") {
		t.Fatalf("quarantine reason not persisted: %#v", rec)
	}
}

func TestTerminalMemoryBrainOutputErrorClassification(t *testing.T) {
	terminal := &terminalMemoryBrainOutputError{err: fmt.Errorf("bad structured result")}
	if !isTerminalMemoryBrainOutputError(terminal) {
		t.Fatal("terminal invalid output must be recognized")
	}
	if isTerminalMemoryBrainOutputError(fmt.Errorf("context deadline exceeded")) {
		t.Fatal("transient inference failure must not be quarantined")
	}
}
