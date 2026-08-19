package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sspgpt/v07/internal/httpjson"
	"sspgpt/v07/internal/model"
	"sspgpt/v07/internal/sstp"
)

type linkedRules struct {
	SessionIdleTimeoutMS            int64 `json:"session_idle_timeout_ms"`
	TurnTimeoutMS                   int64 `json:"turn_timeout_ms"`
	BridgeSecondaryAckEnabled       bool  `json:"bridge_secondary_ack_enabled"`
	BridgeSecondaryAckMaxChars      int   `json:"bridge_secondary_ack_max_chars"`
	BridgeSecondaryTimeoutMS        int64 `json:"bridge_secondary_timeout_ms"`
	MaxThinkingUpdatesPerTurn       int   `json:"max_thinking_updates_per_turn"`
	LocalCompletionReport           bool  `json:"local_completion_report"`
	AllowLocalChatDuringActiveTurn  bool  `json:"allow_local_chat_during_active_turn"`
	PauseAutonomousDuringActiveTurn bool  `json:"pause_autonomous_during_active_turn"`
	RawTranscriptRetention          bool  `json:"raw_transcript_retention"`
}

func (a *app) linkedRules() linkedRules {
	c := linkedRules{SessionIdleTimeoutMS: 600000, TurnTimeoutMS: 300000, BridgeSecondaryAckEnabled: true, BridgeSecondaryAckMaxChars: 40, BridgeSecondaryTimeoutMS: 8000, MaxThinkingUpdatesPerTurn: 3, LocalCompletionReport: true, AllowLocalChatDuringActiveTurn: false, PauseAutonomousDuringActiveTurn: true}
	if b, err := os.ReadFile(filepath.Join(a.root, "config", "linked_chat_rules.json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.SessionIdleTimeoutMS <= 0 {
		c.SessionIdleTimeoutMS = 600000
	}
	if c.TurnTimeoutMS <= 0 {
		c.TurnTimeoutMS = 300000
	}
	if c.BridgeSecondaryAckMaxChars <= 0 {
		c.BridgeSecondaryAckMaxChars = 40
	}
	if c.BridgeSecondaryTimeoutMS <= 0 {
		c.BridgeSecondaryTimeoutMS = 8000
	}
	if c.MaxThinkingUpdatesPerTurn <= 0 {
		c.MaxThinkingUpdatesPerTurn = 3
	}
	return c
}

func capsuleCount(c model.MemoryCapsule) int {
	return len(c.Facts) + len(c.Observations) + len(c.Events) + len(c.Commitments)
}

func computeAffectDelta(before, after model.AffectState) model.AffectDelta {
	keys := map[string]bool{}
	for k := range before.Channels {
		keys[k] = true
	}
	for k := range after.Channels {
		keys[k] = true
	}
	d := model.AffectDelta{Channels: map[string]float64{}}
	for k := range keys {
		v := after.Channels[k] - before.Channels[k]
		d.Channels[k] = v
		av := math.Abs(v)
		d.DeltaTotal += av
		if av > d.DeltaMax {
			d.DeltaMax, d.Dominant = av, k
		}
	}
	return d
}

func opaqueLinkID() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err == nil {
		return "link-" + hex.EncodeToString(b)
	}
	return fmt.Sprintf("link-%d", time.Now().UnixNano())
}

func (a *app) expireLinkedLocked(now time.Time) string {
	if a.linked == nil {
		return ""
	}
	cfg := a.linkedRules()
	if now.Sub(a.linked.LastSeen) > time.Duration(cfg.SessionIdleTimeoutMS)*time.Millisecond {
		old := a.linked.SessionID
		a.linked = nil
		return "session:" + old
	}
	if a.linked.Turn != nil && now.Sub(a.linked.Turn.Created) > time.Duration(cfg.TurnTimeoutMS)*time.Millisecond {
		id := a.linked.Turn.ExternalTurnID
		a.linked.Turn = nil
		return "turn:" + id
	}
	return ""
}

func (a *app) linkedBusy() bool {
	a.mu.Lock()
	expired := a.expireLinkedLocked(time.Now())
	busy := a.linked != nil && a.linked.Turn != nil
	a.mu.Unlock()
	if expired != "" {
		a.log.Printf("LINK_TIMEOUT %s", expired)
	}
	return busy
}

func (a *app) validateLinkedLocked(sessionID string, turnRequired bool, turnID string) error {
	expired := a.expireLinkedLocked(time.Now())
	if expired != "" {
		return fmt.Errorf("linked lease expired: %s", expired)
	}
	if a.linked == nil || sessionID == "" || a.linked.SessionID != sessionID {
		return fmt.Errorf("invalid linked session")
	}
	a.linked.LastSeen = time.Now()
	if turnRequired {
		if a.linked.Turn == nil {
			return fmt.Errorf("no active linked turn")
		}
		if turnID != "" && a.linked.Turn.ExternalTurnID != turnID {
			return fmt.Errorf("linked turn mismatch")
		}
	}
	return nil
}

func onlyPOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		httpjson.Write(w, 405, map[string]string{"error": "POST required"})
		return false
	}
	return true
}

type linkedProfileDocuments struct {
	ShellKey         string `json:"shell_key"`
	ShellName        string `json:"shell_name,omitempty"`
	AppearanceFile   string `json:"appearance_file,omitempty"`
	CharacterSummary string `json:"character_summary"`
	Appearance       string `json:"appearance"`
	Ready            bool   `json:"ready"`
	Building         bool   `json:"building"`
	Error            string `json:"error,omitempty"`
}

func publicProfileError(d linkedProfileDocuments) string {
	if strings.TrimSpace(d.Error) == "" {
		return ""
	}
	// The remote cognition surface only needs to know that the exact current-
	// Shell profile is degraded. Local paths, filenames and routing keys remain
	// inside Runtime/Bridge logs.
	return "Current Shell profile is temporarily unavailable; use the supplied current appearance/state only."
}

func publicLinkedState(state model.CurrentState) model.CurrentState {
	out := state
	out.Appearance.ShellPath = ""
	out.Appearance.ShellKey = ""
	return out
}

func (a *app) linkedProfileDocuments(state model.AppearanceState) linkedProfileDocuments {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	var out linkedProfileDocuments
	err := httpjson.Post(ctx, "http://127.0.0.1:8767/v1/profile/context", map[string]string{"shell_key": state.ShellKey, "shell_name": state.ShellName, "shell_path": state.ShellPath}, &out)
	if err != nil {
		out.ShellName = state.ShellName
		out.Error = "Bridge profile unavailable: " + err.Error()
	}
	return out
}

func (a *app) linkActivate(w http.ResponseWriter, r *http.Request) {
	if !onlyPOST(w, r) {
		return
	}
	sid := opaqueLinkID()
	a.mu.Lock()
	a.linked = &linkedSession{SessionID: sid, Activated: time.Now(), LastSeen: time.Now()}
	a.mu.Unlock()
	appearanceState := a.appearance()
	docs := a.linkedProfileDocuments(appearanceState)
	a.log.Printf("LINK_SESSION_ACTIVATE session=%s shell=%q shell_key=%s profile_ready=%t", sid, appearanceState.ShellName, docs.ShellKey, docs.Ready)
	a.auditCognitionf("LINK_SESSION_ACTIVATE session=%s shell_key=%s", sid, docs.ShellKey)
	httpjson.Write(w, 200, map[string]any{"ok": true, "session_id": sid, "character_summary": docs.CharacterSummary, "appearance": docs.Appearance, "profile_ready": docs.Ready, "profile_error": publicProfileError(docs), "version": version})
}

func (a *app) linkContext(w http.ResponseWriter, r *http.Request) {
	if !onlyPOST(w, r) {
		return
	}
	var in struct {
		SessionID     string `json:"session_id"`
		SemanticQuery string `json:"semantic_query,omitempty"`
	}
	if err := httpjson.Decode(r, &in); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.mu.Lock()
	if err := a.validateLinkedLocked(in.SessionID, false, ""); err != nil {
		a.mu.Unlock()
		httpjson.Write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	aff, _, _ := a.decayAffectLocked()
	physical := currentPhysicalForEnvelope(a.currentPhysical, time.Now())
	hot := a.hot
	phase := "session_active"
	turnID := ""
	if a.linked.Turn != nil {
		phase = a.linked.Turn.Phase
		turnID = a.linked.Turn.ExternalTurnID
	}
	a.mu.Unlock()
	state := model.CurrentState{Physical: physical, Touch: a.touchSnapshot(), Affect: aff, Appearance: a.appearance()}
	state.Situation = buildSituation(model.UserInput{}, physical, aff)
	var embodiment *model.EmbodimentCapabilities
	if cap, _, err := embodimentForShell(state.Appearance.ShellPath); err == nil {
		embodiment = cap
	}
	var capsule model.MemoryCapsule
	if strings.TrimSpace(in.SemanticQuery) != "" {
		a.mu.Lock()
		depth := a.recallDepth
		a.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), a.recallTimeout(depth))
		_ = httpjson.Post(ctx, "http://127.0.0.1:8768/v2/recall", map[string]any{"query": in.SemanticQuery, "depth": depth}, &capsule)
		cancel()
	}
	docs := a.linkedProfileDocuments(state.Appearance)
	a.auditCognitionf("LINK_CONTEXT_READ session=%s phase=%s recalled=%d hot=%d shell_key=%s profile_ready=%t", in.SessionID, phase, capsuleCount(capsule), len(hot.Items), docs.ShellKey, docs.Ready)
	publicState := publicLinkedState(state)
	httpjson.Write(w, 200, map[string]any{"ok": true, "session_id": in.SessionID, "phase": phase, "external_turn_id": turnID, "current_state": publicState, "embodiment": embodiment, "hot_memory": hot, "memory_capsule": capsule, "character_summary": docs.CharacterSummary, "appearance": docs.Appearance, "profile_ready": docs.Ready, "profile_error": publicProfileError(docs)})
}

func (a *app) linkTurnBegin(w http.ResponseWriter, r *http.Request) {
	if !onlyPOST(w, r) {
		return
	}
	var in struct {
		SessionID      string  `json:"session_id"`
		ExternalTurnID string  `json:"external_turn_id"`
		Expression     string  `json:"expression,omitempty"`
		Intensity      float64 `json:"intensity,omitempty"`
	}
	if err := httpjson.Decode(r, &in); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	in.ExternalTurnID = strings.TrimSpace(in.ExternalTurnID)
	if in.ExternalTurnID == "" {
		httpjson.Write(w, 400, map[string]string{"error": "external_turn_id required"})
		return
	}
	a.mu.Lock()
	if err := a.validateLinkedLocked(in.SessionID, false, ""); err != nil {
		a.mu.Unlock()
		httpjson.Write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	if a.linked.Turn != nil {
		a.mu.Unlock()
		httpjson.Write(w, 409, map[string]string{"error": "linked turn already active"})
		return
	}
	before := cloneAffect(a.affect)
	a.linked.Turn = &linkedTurn{ExternalTurnID: in.ExternalTurnID, Phase: "received", Created: time.Now(), AffectBefore: before}
	a.mu.Unlock()
	// Web ChatGPT becomes Primary Brain for this turn. Any local appearance
	// cognition already in flight is now competing foreground work and must not
	// later mutate affect/presentation/memory.
	a.supersedeActiveAppearance()
	a.sendPresentation("\\![raise,OnCharacterGPTLinkedPresence," + sstp.EscapeArg(in.Expression) + "," + fmt.Sprintf("%.3f", in.Intensity) + "]\\e")
	a.log.Printf("LINK_TURN_RECEIVED session=%s turn=%s", in.SessionID, in.ExternalTurnID)
	a.auditCognitionf("LINK_TURN_RECEIVED session=%s turn=%s", in.SessionID, in.ExternalTurnID)
	httpjson.Write(w, 200, map[string]any{"ok": true, "phase": "received"})
}

func (a *app) linkBridgeReaction(w http.ResponseWriter, r *http.Request) {
	if !onlyPOST(w, r) {
		return
	}
	var in struct {
		SessionID      string `json:"session_id"`
		ExternalTurnID string `json:"external_turn_id"`
		SemanticDigest string `json:"semantic_digest"`
		ReactionIntent string `json:"reaction_intent,omitempty"`
	}
	if err := httpjson.Decode(r, &in); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.mu.Lock()
	if err := a.validateLinkedLocked(in.SessionID, true, in.ExternalTurnID); err != nil {
		a.mu.Unlock()
		httpjson.Write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	phase := a.linked.Turn.Phase
	a.mu.Unlock()
	if phase != "received" && phase != "thinking" {
		httpjson.Write(w, 409, map[string]string{"error": "bridge reaction not allowed in phase " + phase})
		return
	}
	cfg := a.linkedRules()
	if !cfg.BridgeSecondaryAckEnabled {
		httpjson.Write(w, 200, map[string]any{"ok": true, "skipped": true})
		return
	}
	digest := strings.TrimSpace(in.SemanticDigest)
	if len([]rune(digest)) > 600 {
		digest = string([]rune(digest)[:600])
	}
	id := a.newID("linkack")
	env := a.buildEnvelope(id, model.RequestLinkedChat, "chatgpt_web", model.UserInput{}, nil, 0)
	env.RequestPolicy.Secondary = true
	env.Linked = &model.LinkedRef{SessionID: in.SessionID, ExternalTurnID: in.ExternalTurnID, Phase: phase}
	env.SemanticDigest = digest
	env.ReactionIntent = strings.TrimSpace(in.ReactionIntent)
	timeout := time.Duration(cfg.BridgeSecondaryTimeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var rr model.Reaction
	err := httpjson.Post(ctx, "http://127.0.0.1:8767/v1/respond", env, &rr)
	if err != nil || strings.TrimSpace(rr.Dialogue) == "" {
		rr = model.Reaction{RequestID: id, RequestClass: model.RequestLinkedChat, Action: "speak", Dialogue: "嗯，我來看看。", ReactionEmotion: "neutral", Presentation: model.Presentation{Expression: "neutral", Pose: "normal"}}
		a.log.Printf("LINK_BRIDGE_REACTION_FALLBACK turn=%s error=%v", in.ExternalTurnID, err)
	}
	rrs := []rune(strings.TrimSpace(rr.Dialogue))
	if len(rrs) > cfg.BridgeSecondaryAckMaxChars {
		rr.Dialogue = string(rrs[:cfg.BridgeSecondaryAckMaxChars])
	}
	a.sendPresentation("\\![raise,OnCharacterGPTLinkedBridgeReaction," + sstp.EscapeArg(rr.Dialogue) + "," + sstp.EscapeArg(rr.Presentation.Expression) + "]\\e")
	a.auditCognitionf("LINK_BRIDGE_REACTION_COMPLETE session=%s turn=%s chars=%d fallback=%t", in.SessionID, in.ExternalTurnID, len([]rune(rr.Dialogue)), err != nil)
	httpjson.Write(w, 200, map[string]any{"ok": true, "dialogue": rr.Dialogue, "presentation": rr.Presentation})
}

func (a *app) linkThinking(w http.ResponseWriter, r *http.Request) {
	if !onlyPOST(w, r) {
		return
	}
	var in struct {
		SessionID      string  `json:"session_id"`
		ExternalTurnID string  `json:"external_turn_id"`
		Milestone      string  `json:"milestone"`
		Expression     string  `json:"expression,omitempty"`
		Intensity      float64 `json:"intensity,omitempty"`
	}
	if err := httpjson.Decode(r, &in); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	allowed := map[string]bool{"start": true, "progress": true, "difficulty": true, "resolved": true}
	if !allowed[in.Milestone] {
		httpjson.Write(w, 400, map[string]string{"error": "invalid thinking milestone"})
		return
	}
	a.mu.Lock()
	if err := a.validateLinkedLocked(in.SessionID, true, in.ExternalTurnID); err != nil {
		a.mu.Unlock()
		httpjson.Write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	cfg := a.linkedRules()
	if a.linked.Turn.ThinkingUpdates >= cfg.MaxThinkingUpdatesPerTurn {
		a.mu.Unlock()
		httpjson.Write(w, 200, map[string]any{"ok": true, "capped": true})
		return
	}
	a.linked.Turn.ThinkingUpdates++
	a.linked.Turn.Phase = "thinking"
	n := a.linked.Turn.ThinkingUpdates
	a.mu.Unlock()
	a.sendPresentation("\\![raise,OnCharacterGPTLinkedThinking," + sstp.EscapeArg(in.Milestone) + "," + sstp.EscapeArg(in.Expression) + "," + fmt.Sprintf("%.3f", in.Intensity) + "]\\e")
	a.auditCognitionf("LINK_THINKING_UPDATE session=%s turn=%s milestone=%s count=%d", in.SessionID, in.ExternalTurnID, in.Milestone, n)
	httpjson.Write(w, 200, map[string]any{"ok": true, "phase": "thinking", "count": n})
}

func (a *app) linkResponse(w http.ResponseWriter, r *http.Request) {
	if !onlyPOST(w, r) {
		return
	}
	var in struct {
		SessionID      string `json:"session_id"`
		ExternalTurnID string `json:"external_turn_id"`
		Expression     string `json:"expression,omitempty"`
		ResponseLength int    `json:"response_length,omitempty"`
	}
	if err := httpjson.Decode(r, &in); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.mu.Lock()
	if err := a.validateLinkedLocked(in.SessionID, true, in.ExternalTurnID); err != nil {
		a.mu.Unlock()
		httpjson.Write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	a.linked.Turn.Phase = "responding"
	a.mu.Unlock()
	a.sendPresentation("\\![raise,OnCharacterGPTLinkedResponse," + sstp.EscapeArg(in.Expression) + "," + fmt.Sprintf("%d", in.ResponseLength) + "]\\e")
	a.auditCognitionf("LINK_RESPONSE_BEGIN session=%s turn=%s length=%d", in.SessionID, in.ExternalTurnID, in.ResponseLength)
	httpjson.Write(w, 200, map[string]any{"ok": true, "phase": "responding"})
}

func canonicalLinkedReactionEmotion(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "neutral", "normal":
		return "neutral"
	case "happy", "positive", "satisfied", "pleased", "content", "smile":
		return "smile"
	case "cheerful", "excited":
		return "cheerful"
	case "shy", "embarrassed":
		return "embarrassed"
	case "embarrassed_smile", "blush":
		return "embarrassed_smile"
	case "wary", "focused", "concerned":
		return "concerned"
	case "annoyed", "angry":
		return "angry"
	case "embarrassed_angry", "blush_angry":
		return "embarrassed_angry"
	case "downcast", "sad":
		return "sad"
	case "surprised":
		return "surprised"
	default:
		// Unknown remote labels never become an unmodelled persistent affect.
		return "neutral"
	}
}

func (a *app) linkCommit(w http.ResponseWriter, r *http.Request) {
	if !onlyPOST(w, r) {
		return
	}
	var in struct {
		SessionID       string `json:"session_id"`
		ExternalTurnID  string `json:"external_turn_id"`
		Status          string `json:"status"`
		UserRequest     string `json:"user_request,omitempty"`
		WebResponse     string `json:"web_response,omitempty"`
		RequestSummary  string `json:"request_summary,omitempty"`
		ResponseSummary string `json:"response_summary,omitempty"`
		Topic           string `json:"topic,omitempty"`
		Outcome         string `json:"outcome,omitempty"`
		ReactionEmotion string `json:"reaction_emotion"`
		Expression      string `json:"expression,omitempty"`
	}
	if err := httpjson.Decode(r, &in); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if in.Status != "" && in.Status != "completed" {
		httpjson.Write(w, 400, map[string]string{"error": "only completed commits are accepted"})
		return
	}
	key := in.SessionID + "|" + in.ExternalTurnID
	a.mu.Lock()
	if a.linkedCommitted[key] {
		a.mu.Unlock()
		httpjson.Write(w, 200, map[string]any{"ok": true, "duplicate": true})
		return
	}
	if err := a.validateLinkedLocked(in.SessionID, true, in.ExternalTurnID); err != nil {
		a.mu.Unlock()
		httpjson.Write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	requestBefore := a.linked.Turn.AffectBefore
	in.ReactionEmotion = canonicalLinkedReactionEmotion(in.ReactionEmotion)
	causalBefore, after := a.updateAffectLocked("linked-"+in.ExternalTurnID, in.ReactionEmotion, "chatgpt_web", strings.TrimSpace(in.RequestSummary))
	a.linkedCommitted[key] = true
	a.linked.Turn.Committed = true
	a.linked.Turn.Phase = "completed"
	a.linked.Turn = nil
	a.linked.LastSeen = time.Now()
	a.mu.Unlock()
	cfg := a.linkedRules()
	material := &model.EpisodeMaterial{RequestSummary: strings.TrimSpace(in.RequestSummary), ResponseSummary: strings.TrimSpace(in.ResponseSummary), Topic: strings.TrimSpace(in.Topic), Outcome: strings.TrimSpace(in.Outcome)}
	if cfg.RawTranscriptRetention {
		material.UserRequest = strings.TrimSpace(in.UserRequest)
		material.WebResponse = strings.TrimSpace(in.WebResponse)
	}
	// Even when raw persistence is disabled, preserve a bounded semantic form as the episode's user/reaction text so the local Memory Brain receives useful material.
	userText := strings.TrimSpace(in.RequestSummary)
	if userText == "" {
		userText = truncateRunes(strings.TrimSpace(in.UserRequest), 1200)
	}
	responseText := strings.TrimSpace(in.ResponseSummary)
	if responseText == "" {
		responseText = truncateRunes(strings.TrimSpace(in.WebResponse), 1600)
	}
	rid := a.newID("linked")
	rr := model.Reaction{RequestID: rid, RequestClass: model.RequestLinkedChat, Action: "speak", Dialogue: responseText, ReactionEmotion: in.ReactionEmotion, Presentation: model.Presentation{Expression: in.Expression, Pose: "normal"}}
	ep := model.EpisodeCommitV2{EpisodeID: rid, RequestID: rid, RequestClass: model.RequestLinkedChat, CompletedAt: model.Now(), Source: "chatgpt_web", UserInput: model.UserInput{Text: userText}, Reaction: rr, AffectAtRequest: requestBefore, AffectBefore: causalBefore, AffectAfter: after, AffectDelta: computeAffectDelta(causalBefore, after), Status: "completed", Linked: &model.LinkedRef{SessionID: in.SessionID, ExternalTurnID: in.ExternalTurnID, Phase: "completed"}, Material: material}
	go a.commitEpisode(ep)
	if cfg.LocalCompletionReport {
		a.sendPresentation("\\![raise,OnCharacterGPTLinkedResponseComplete]\\e")
	} else {
		// Honor the hot-editable policy without changing lifecycle semantics.
		// LinkedRelease clears the local busy presentation without speaking.
		a.sendPresentation("\\![raise,OnCharacterGPTLinkedRelease]\\e")
	}
	a.auditCognitionf("LINK_TURN_COMMIT session=%s turn=%s episode=%s emotion=%s delta_max=%.4f completion_report=%t", in.SessionID, in.ExternalTurnID, rid, in.ReactionEmotion, ep.AffectDelta.DeltaMax, cfg.LocalCompletionReport)
	httpjson.Write(w, 200, map[string]any{"ok": true, "phase": "completed", "episode_id": rid, "affect": after})
}

func (a *app) linkAbort(w http.ResponseWriter, r *http.Request) {
	if !onlyPOST(w, r) {
		return
	}
	var in struct {
		SessionID      string `json:"session_id"`
		ExternalTurnID string `json:"external_turn_id"`
		Reason         string `json:"reason,omitempty"`
	}
	if err := httpjson.Decode(r, &in); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.mu.Lock()
	if err := a.validateLinkedLocked(in.SessionID, true, in.ExternalTurnID); err != nil {
		a.mu.Unlock()
		httpjson.Write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	a.linked.Turn = nil
	a.linked.LastSeen = time.Now()
	a.mu.Unlock()
	a.auditCognitionf("LINK_TURN_ABORT session=%s turn=%s reason=%q", in.SessionID, in.ExternalTurnID, in.Reason)
	httpjson.Write(w, 200, map[string]any{"ok": true, "phase": "aborted"})
}

func (a *app) linkDeactivate(w http.ResponseWriter, r *http.Request) {
	if !onlyPOST(w, r) {
		return
	}
	var in struct {
		SessionID string `json:"session_id"`
	}
	if err := httpjson.Decode(r, &in); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.mu.Lock()
	if a.linked == nil || (in.SessionID != "" && a.linked.SessionID != in.SessionID) {
		a.mu.Unlock()
		httpjson.Write(w, 409, map[string]string{"error": "invalid linked session"})
		return
	}
	old := a.linked.SessionID
	a.linked = nil
	a.mu.Unlock()
	a.auditCognitionf("LINK_SESSION_DEACTIVATE session=%s", old)
	httpjson.Write(w, 200, map[string]any{"ok": true, "state": "available"})
}

func (a *app) linkStatus(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	expired := a.expireLinkedLocked(time.Now())
	state := "available"
	sid := ""
	phase := ""
	turn := ""
	if a.linked != nil {
		state = "session_active"
		sid = a.linked.SessionID
		if a.linked.Turn != nil {
			state = "turn_active"
			phase = a.linked.Turn.Phase
			turn = a.linked.Turn.ExternalTurnID
		}
	}
	a.mu.Unlock()
	if expired != "" {
		a.log.Printf("LINK_TIMEOUT %s", expired)
	}
	httpjson.Write(w, 200, map[string]any{"ok": true, "state": state, "session_id": sid, "phase": phase, "external_turn_id": turn})
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
