package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"

	"sspgpt/v07/internal/model"
	"strings"
	"testing"
	"time"
)

func TestCJKUIFontCandidatesPreferTraditionalChinese(t *testing.T) {
	got := cjkUIFontCandidates()
	if len(got) < 2 {
		t.Fatalf("expected CJK font fallback chain, got %#v", got)
	}
	if got[0] != "Microsoft JhengHei UI" || got[1] != "Microsoft JhengHei" {
		t.Fatalf("Traditional Chinese fonts must lead fallback chain: %#v", got)
	}
	wantFallback := map[string]bool{"Yu Gothic UI": false, "Meiryo UI": false, "Segoe UI": false}
	for _, name := range got {
		if _, ok := wantFallback[name]; ok {
			wantFallback[name] = true
		}
	}
	for name, found := range wantFallback {
		if !found {
			t.Fatalf("missing UI fallback %q from %#v", name, got)
		}
	}
}

func TestClassifyGentleStroke(t *testing.T) {
	s := &touchSession{Path: 30, SpeedSum: 160, Samples: 4}
	g, _, speed := classify(s)
	if g != "gentle_stroke" || speed != 40 {
		t.Fatalf("got %s speed=%v", g, speed)
	}
}

func TestClassifyRoughRubNeedsSpeedAndReversals(t *testing.T) {
	// A rapid three-reversal rub remains rough_rub.
	s := &touchSession{Path: 300, SpeedSum: 1600, Samples: 4, Reversals: 3}
	g, _, speed := classify(s)
	if g != "rough_rub" || speed != 400 {
		t.Fatalf("got %s speed=%v", g, speed)
	}

	// The observed 320 px/s ordinary back-and-forth stroke must not cross
	// the alpha2h rough-rub threshold merely because it reversed 3 times.
	s.SpeedSum = 1280
	g, _, speed = classify(s)
	if g != "stroke" || speed != 320 {
		t.Fatalf("320 px/s should remain stroke, got %s speed=%v", g, speed)
	}

	// High speed without enough reversals is still a stroke rather than rub.
	s.SpeedSum = 1600
	s.Reversals = 2
	g, _, _ = classify(s)
	if g == "rough_rub" {
		t.Fatalf("rough_rub without enough reversals")
	}
}

func TestClassifyRoughRubThresholdBoundary(t *testing.T) {
	s := &touchSession{Path: 300, SpeedSum: 1436, Samples: 4, Reversals: 3}
	g, _, speed := classify(s)
	if g != "stroke" || speed != 359 {
		t.Fatalf("below threshold should be stroke, got %s speed=%v", g, speed)
	}
	s.SpeedSum = 1440
	g, _, speed = classify(s)
	if g != "rough_rub" || speed != 360 {
		t.Fatalf("threshold should be rough_rub, got %s speed=%v", g, speed)
	}
}

func TestEndedPhysicalExpiresFromNow(t *testing.T) {
	now := time.Now()
	old := &model.PhysicalEvent{Gesture: "release", Contact: false, Released: true, ObservedAt: now.Add(-3 * time.Second).Format(time.RFC3339Nano)}
	if got := currentPhysicalForEnvelope(old, now); got != nil {
		t.Fatalf("stale ended event remained current: %#v", got)
	}
	recent := &model.PhysicalEvent{Gesture: "release", Contact: false, Released: true, ObservedAt: now.Add(-500 * time.Millisecond).Format(time.RFC3339Nano)}
	if got := currentPhysicalForEnvelope(recent, now); got == nil {
		t.Fatal("recent authoritative release disappeared too early")
	}
	active := &model.PhysicalEvent{Gesture: "resting_touch", Contact: true, Resting: true, ObservedAt: now.Add(-30 * time.Second).Format(time.RFC3339Nano)}
	if got := currentPhysicalForEnvelope(active, now); got == nil {
		t.Fatal("active contact must remain current until release")
	}
}

func TestRecallRouterIsSelective(t *testing.T) {
	if needsRecall("你好，今天好嗎？") {
		t.Fatal("ordinary chat should stay on fast path")
	}
	if !needsRecall("你還記得我之前說過京都嗎？") {
		t.Fatal("explicit memory question should route recall")
	}
}

func TestDeferredHoldingLineAndTimerSharePresentationScript(t *testing.T) {
	rr := model.Reaction{Dialogue: "可以，30秒後回覆你。"}
	timer := "\\![timerraise,30000,1,OnCharacterGPTDeferred,cont-test]"
	script := buildPresentationScript(rr, 20, 15000, timer)
	if !strings.Contains(script, rr.Dialogue) {
		t.Fatalf("holding line missing from presentation script: %q", script)
	}
	if !strings.Contains(script, timer) {
		t.Fatalf("continuation timer missing from presentation script: %q", script)
	}
	if strings.Index(script, rr.Dialogue) > strings.Index(script, timer) {
		t.Fatalf("timer must be embedded after the visible holding line: %q", script)
	}
}

func TestAffectSnapshotScriptIsControlOnlyAndComplete(t *testing.T) {
	aff := model.AffectState{Revision: 7, Primary: "shy", Channels: map[string]float64{
		"positive": 0.31, "shy": 0.52, "wary": 0.14, "annoyed": 0.02, "downcast": 0.01,
	}}
	script := buildAffectSnapshotScript("restore", aff)
	if !strings.HasPrefix(script, "\\C\\![raise,OnCharacterGPTAffectSnapshot,restore,") {
		t.Fatalf("affect snapshot must be presentation-transparent: %q", script)
	}
	for _, want := range []string{"0.3100", "0.5200", "0.1400", "0.0200", "0.0100", ",7,shy]"} {
		if !strings.Contains(script, want) {
			t.Fatalf("missing %q from snapshot: %q", want, script)
		}
	}
}

func TestDropConflictingSessionsLockedKeepsOnlyCurrentTarget(t *testing.T) {
	a := &app{
		sessions:       map[string]*touchSession{},
		physicalActive: map[string]string{},
	}
	old := &touchSession{CharacterID: "1", Target: "Owl.Bust", SessionID: "old-bust"}
	current := &touchSession{CharacterID: "1", Target: "Owl.Wing", SessionID: "current-wing"}
	main := &touchSession{CharacterID: "0", Target: "Bust", SessionID: "main-bust"}
	a.sessions[sessionKey(old.CharacterID, old.Target)] = old
	a.sessions[sessionKey(current.CharacterID, current.Target)] = current
	a.sessions[sessionKey(main.CharacterID, main.Target)] = main
	a.physicalActive[old.SessionID] = "phys-old"

	dropped := a.dropConflictingSessionsLocked("1", "Owl.Wing")
	if len(dropped) != 1 || dropped[0].SessionID != old.SessionID {
		t.Fatalf("unexpected dropped sessions: %#v", dropped)
	}
	if _, ok := a.sessions[sessionKey("1", "Owl.Bust")]; ok {
		t.Fatal("stale Owl.Bust Runtime session remained")
	}
	if _, ok := a.sessions[sessionKey("1", "Owl.Wing")]; !ok {
		t.Fatal("current Owl.Wing session was removed")
	}
	if _, ok := a.sessions[sessionKey("0", "Bust")]; !ok {
		t.Fatal("other character session was removed")
	}
	if _, ok := a.physicalActive[old.SessionID]; ok {
		t.Fatal("physicalActive entry for stale session remained")
	}
}

func TestParseDressupRecordsMakesEnabledStateExplicit(t *testing.T) {
	records := []string{
		"0\x01メガネ\x01黒茶グラデ\x01\x011\x01thumb.png",
		"0\x01帽子\x01ベレー\x01\x010\x01hat.png",
	}
	got := parseDressupRecords(records)
	if len(got) != 2 {
		t.Fatalf("expected two dress-up records, got %#v", got)
	}
	glasses, ok := got["0|メガネ|黒茶グラデ"].(map[string]any)
	if !ok || glasses["enabled"] != true {
		t.Fatalf("glasses state not parsed as enabled bool: %#v", glasses)
	}
	hat, ok := got["0|帽子|ベレー"].(map[string]any)
	if !ok || hat["enabled"] != false {
		t.Fatalf("hat state not parsed as disabled bool: %#v", hat)
	}
}

func TestAppearanceSummaryNeverReusesPreviousShellWhilePending(t *testing.T) {
	x := model.AppearanceState{
		ShellName:        "alternate",
		SnapshotComplete: false,
		Dressup: map[string]any{
			"0|メガネ|黒茶グラデ": map[string]any{"category": "メガネ", "part": "黒茶グラデ", "enabled": true},
		},
	}
	got := appearanceSummary(x)
	if strings.Contains(got, "メガネ") || !strings.Contains(got, "pending") {
		t.Fatalf("pending shell must not expose stale dress-up facts: %q", got)
	}
}

func TestAppearanceSummaryReportsGlassesOnOff(t *testing.T) {
	x := model.AppearanceState{ShellName: "master", SnapshotComplete: true, Dressup: map[string]any{
		"0|メガネ|黒茶グラデ": map[string]any{"category": "メガネ", "part": "黒茶グラデ", "enabled": true},
	}}
	if got := appearanceSummary(x); !strings.Contains(got, "メガネ/黒茶グラデ=ON") {
		t.Fatalf("enabled glasses missing from summary: %q", got)
	}
	x.Dressup["0|メガネ|黒茶グラデ"].(map[string]any)["enabled"] = false
	if got := appearanceSummary(x); !strings.Contains(got, "メガネ/黒茶グラデ=OFF") {
		t.Fatalf("disabled glasses missing from summary: %q", got)
	}
}

func TestBriefPassDoesNotEmitFinalStroke(t *testing.T) {
	s := &touchSession{Path: 40, SpeedSum: 800, Samples: 2}
	g, _, _ := classify(s)
	if g != "stroke" {
		t.Fatalf("fixture must classify as stroke, got %s", g)
	}
	if shouldEmitFinalStroke(s, 74, g) {
		t.Fatal("74 ms collision sweep must not emit a final stroke reaction")
	}
	if shouldEmitFinalStroke(s, 179, g) {
		t.Fatal("below final stroke duration threshold must remain suppressed")
	}
	if !shouldEmitFinalStroke(s, 180, g) {
		t.Fatal("meaningful 180 ms stroke with sufficient path should emit")
	}
	s.Path = 19.9
	if shouldEmitFinalStroke(s, 500, g) {
		t.Fatal("short path must not emit even when duration is long enough")
	}
}

func TestProvisionalReentryGuardOnlyResumesImmediateSameTarget(t *testing.T) {
	now := time.Now()
	fresh := &touchSession{Last: now.Add(-120 * time.Millisecond)}
	if !canResumeProvisional(fresh, now) {
		t.Fatal("120 ms same-target re-entry should remain resumable")
	}
	stale := &touchSession{Last: now.Add(-121 * time.Millisecond)}
	if canResumeProvisional(stale, now) {
		t.Fatal("re-entry older than 120 ms must start a new Runtime session")
	}
	veryStale := &touchSession{Last: now.Add(-4 * time.Second)}
	if canResumeProvisional(veryStale, now) {
		t.Fatal("multi-second stale provisional session must never absorb a new contact")
	}
}

func TestHandleMoveExpiredProvisionalStartsFreshSession(t *testing.T) {
	oldAt := time.Now().Add(-4 * time.Second)
	old := &touchSession{
		CharacterID: "0", Target: "Book", SessionID: "stale-book",
		Started: oldAt, Last: oldAt, LastX: 0, lastY: 0,
	}
	a := &app{
		log:              log.New(io.Discard, "", 0),
		sessions:         map[string]*touchSession{sessionKey("0", "Book"): old},
		physicalActive:   map[string]string{"stale-book": "phys-old"},
		lastPresentation: map[string]presentationMark{},
	}

	a.handleMove([]string{"MOVE", "0", "Book", "25.3", "0", "0", "boundary=1"})

	got := a.sessions[sessionKey("0", "Book")]
	if got == nil {
		t.Fatal("new Book session was not created")
	}
	if got.SessionID == old.SessionID {
		t.Fatal("four-second-old provisional session was incorrectly resumed")
	}
	if _, ok := a.physicalActive[old.SessionID]; ok {
		t.Fatal("stale session remained in physicalActive")
	}
	if a.currentPhysical == nil || a.currentPhysical.DurationMS > 100 {
		t.Fatalf("new authoritative contact inherited stale duration: %#v", a.currentPhysical)
	}
}

func TestCanonicalLinkedReactionEmotion(t *testing.T) {
	cases := map[string]string{
		"happy":                "smile",
		"satisfied":            "smile",
		"wary":                 "concerned",
		"annoyed":              "angry",
		"downcast":             "sad",
		"unknown_remote_label": "neutral",
	}
	for in, want := range cases {
		if got := canonicalLinkedReactionEmotion(in); got != want {
			t.Fatalf("canonicalLinkedReactionEmotion(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTouchSnapshotHasActiveAndRejectsMissing(t *testing.T) {
	snapshot := map[string]any{"active": []any{map[string]any{"target": "Owl.Bust", "character_id": "1", "contact": true}}}
	if !touchSnapshotHasActive(snapshot, "1", "Owl.Bust") {
		t.Fatal("authoritative active Owl contact not found")
	}
	if touchSnapshotHasActive(snapshot, "1", "Owl.Wing") {
		t.Fatal("unrelated target incorrectly treated as active")
	}
	if touchSnapshotHasActive(map[string]any{"active": []any{}}, "1", "Owl.Bust") {
		t.Fatal("missing TouchProgress active state must not confirm contact")
	}
}

func TestClearPhysicalContactLockedRemovesGhostSession(t *testing.T) {
	a := &app{sessions: map[string]*touchSession{}, physicalActive: map[string]string{}}
	s := &touchSession{CharacterID: "1", Target: "Owl.Bust", SessionID: "ghost"}
	a.sessions[sessionKey("1", "Owl.Bust")] = s
	a.physicalActive[s.SessionID] = "phys-old"
	a.currentPhysical = &model.PhysicalEvent{Target: "Owl.Bust", CharacterID: "1", Contact: true, SessionID: s.SessionID}
	clearPhysicalContactLocked(a, "1", "Owl.Bust")
	if a.currentPhysical != nil || len(a.sessions) != 0 || len(a.physicalActive) != 0 {
		t.Fatalf("ghost contact not fully cleared: physical=%#v sessions=%#v active=%#v", a.currentPhysical, a.sessions, a.physicalActive)
	}
}

func TestAffectImpulseDeltaIsCausalPerCompletion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	rules := `{"enabled":true,"half_life_seconds":999999,"neutral_threshold":0.05,"dialogue_weight":0.65,"physical_weight":1.0,"impulses":{"embarrassed":{"shy":0.4},"concerned":{"wary":0.3}}}`
	if err := os.WriteFile(filepath.Join(root, "config", "emotional_state_rules.json"), []byte(rules), 0644); err != nil {
		t.Fatal(err)
	}
	a := &app{root: root, affectAudit: log.New(io.Discard, "", 0), affect: model.AffectState{Channels: map[string]float64{"positive": 0, "shy": 0, "wary": 0, "annoyed": 0, "downcast": 0}, UpdatedAt: time.Now().Format(time.RFC3339Nano)}}
	b1, a1 := a.updateAffectLocked("r1", "embarrassed", "physical", "head")
	b2, a2 := a.updateAffectLocked("r2", "concerned", "physical", "head")
	d1 := computeAffectDelta(b1, a1)
	d2 := computeAffectDelta(b2, a2)
	if d1.Dominant != "shy" || d2.Dominant != "wary" {
		t.Fatalf("causal deltas crossed requests: d1=%#v d2=%#v", d1, d2)
	}
	if d2.Channels["shy"] != 0 {
		t.Fatalf("second reaction borrowed first reaction's shy delta: %#v", d2.Channels)
	}
}

func TestRecentPhysicalCountsDistinctSessionsNotProgressUpdates(t *testing.T) {
	a := &app{recentPhysical: []model.PhysicalEvent{}}
	now := time.Now()
	a.rememberPhysicalOccurrence(&model.PhysicalEvent{Target: "Head", Gesture: "stroke", SessionID: "s1", ObservedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano), DurationMS: 300})
	a.rememberPhysicalOccurrence(&model.PhysicalEvent{Target: "Head", Gesture: "stroke", SessionID: "s1", ObservedAt: now.Add(-9 * time.Second).Format(time.RFC3339Nano), DurationMS: 900, Phase: "final"})
	a.rememberPhysicalOccurrence(&model.PhysicalEvent{Target: "Head", Gesture: "stroke", SessionID: "s2", ObservedAt: now.Add(-2 * time.Second).Format(time.RFC3339Nano), DurationMS: 400})
	if len(a.recentPhysical) != 2 {
		t.Fatalf("same-session progress inflated occurrence count: %#v", a.recentPhysical)
	}
	if a.recentPhysical[0].DurationMS != 900 {
		t.Fatalf("same-session final facts did not refresh occurrence: %#v", a.recentPhysical[0])
	}
}

func TestShellSemanticSurfaceUsesExplicitPose(t *testing.T) {
	sem := &model.ShellSemantics{
		FormatVersion: 1,
		DefaultPose:   "normal",
		Poses: []model.EmbodimentPose{
			{ID: "normal", Meaning: "default"},
			{ID: "hand_to_chin", Meaning: "raised hand near chin"},
		},
		Expressions: []string{"neutral", "smile"},
		Gazes:       []string{"normal", "user"},
		Surfaces: []model.ShellSurfaceCombination{
			{Pose: "normal", Expression: "neutral", Gaze: "normal", Surface: 0},
			{Pose: "hand_to_chin", Expression: "neutral", Gaze: "normal", Surface: 100},
			{Pose: "hand_to_chin", Expression: "smile", Gaze: "user", Surface: 115},
		},
	}
	if err := validateShellSemantics(sem); err != nil {
		t.Fatal(err)
	}
	rr := model.Reaction{ReactionEmotion: "smile", Presentation: model.Presentation{Pose: "hand_to_chin", Expression: "smile", Gaze: "user"}}
	if got, ok := resolveSemanticSurface(rr, sem); !ok || got != 115 {
		t.Fatalf("explicit semantic pose did not resolve to surface115: got=%d ok=%t", got, ok)
	}
}

func TestShellSemanticSurfaceDoesNotTreatGestureAsPoseAlias(t *testing.T) {
	sem := &model.ShellSemantics{
		FormatVersion: 1,
		DefaultPose:   "normal",
		Poses: []model.EmbodimentPose{
			{ID: "normal", Meaning: "default"},
			{ID: "hand_to_chin", Meaning: "raised hand near chin"},
		},
		Expressions: []string{"neutral"},
		Gazes:       []string{"normal"},
		Surfaces: []model.ShellSurfaceCombination{
			{Pose: "normal", Expression: "neutral", Gaze: "normal", Surface: 0},
			{Pose: "hand_to_chin", Expression: "neutral", Gaze: "normal", Surface: 100},
		},
	}
	rr := model.Reaction{ReactionEmotion: "neutral", Presentation: model.Presentation{Expression: "neutral", Gaze: "normal", Gesture: "raise_hand"}}
	if got, ok := resolveSemanticSurface(rr, sem); ok {
		t.Fatalf("legacy gesture alias must not resolve a pose: got surface=%d", got)
	}
}

func TestShellSemanticSurfacePreservesPoseWhenCombinationMissing(t *testing.T) {
	sem := &model.ShellSemantics{
		FormatVersion: 1,
		DefaultPose:   "normal",
		Poses: []model.EmbodimentPose{
			{ID: "normal", Meaning: "default"},
			{ID: "hand_to_chin", Meaning: "raised hand near chin"},
		},
		Expressions: []string{"neutral", "surprised"},
		Gazes:       []string{"normal", "user"},
		Surfaces: []model.ShellSurfaceCombination{
			{Pose: "normal", Expression: "neutral", Gaze: "normal", Surface: 0},
			{Pose: "hand_to_chin", Expression: "neutral", Gaze: "normal", Surface: 100},
			{Pose: "hand_to_chin", Expression: "surprised", Gaze: "normal", Surface: 102},
		},
	}
	rr := model.Reaction{ReactionEmotion: "surprised", Presentation: model.Presentation{Pose: "hand_to_chin", Expression: "surprised", Gaze: "user"}}
	if got, ok := resolveSemanticSurface(rr, sem); !ok || got != 102 {
		t.Fatalf("missing gaze variant should preserve pose/expression and degrade gaze: got=%d ok=%t", got, ok)
	}
}

func TestPackagedMasterShellSemanticsDeclaresRaisedHandMeaning(t *testing.T) {
	path := filepath.Join("..", "..", "package_overlay", "shell", "master", shellSemanticsFilename)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sem model.ShellSemantics
	if err := json.Unmarshal(b, &sem); err != nil {
		t.Fatal(err)
	}
	if err := validateShellSemantics(&sem); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range sem.Poses {
		if p.ID == "hand_to_chin" {
			found = strings.Contains(strings.Join(p.Uses, " "), "舉手")
		}
	}
	if !found {
		t.Fatal("hand_to_chin pose must explicitly teach the LLM its raised-hand/舉手 affordance")
	}
}

func TestShellSemanticsRejectsUnknownCompatibilityFields(t *testing.T) {
	shell := t.TempDir()
	raw := `{
  "format_version":1,
  "default_pose":"normal",
  "poses":[{"id":"normal","meaning":"default","aliases":["legacy_pose"]}],
  "expressions":["neutral"],
  "gazes":["normal"],
  "surfaces":[{"pose":"normal","expression":"neutral","gaze":"normal","surface":0}]
}`
	if err := os.WriteFile(filepath.Join(shell, shellSemanticsFilename), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadShellSemantics(shell); err == nil {
		t.Fatal("unknown alias/compatibility fields must be rejected instead of silently becoming technical debt")
	}
}

func TestSurfaceForFallsBackToPresentationMapWithoutShellSemantics(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "presentation_map.json"), []byte(`{"smile":25}`), 0644); err != nil {
		t.Fatal(err)
	}
	a := &app{root: root, log: log.New(io.Discard, "", 0)}
	rr := model.Reaction{ReactionEmotion: "smile", Presentation: model.Presentation{Pose: "normal", Expression: "smile", Gaze: "user"}}
	if got := a.surfaceFor(rr); got != 25 {
		t.Fatalf("shell without semantic contract must keep presentation_map fallback, got %d", got)
	}
}

func TestRecallRouterHistoricalReferencesWithoutDomainKeywords(t *testing.T) {
	if !needsRecall("我最後給慕娜做的事情是什麼？") {
		t.Fatal("historical superlative question should route recall")
	}
	if !needsRecall("我們上一次談到哪裡？") {
		t.Fatal("generic previous-time reference should route recall")
	}
	if needsRecall("我們來做一次大改版吧") {
		t.Fatal("domain phrase 大改版 must not itself be a recall trigger")
	}
	if needsRecall("最後請把門關上") {
		t.Fatal("non-historical use of 最後 must stay on fast path")
	}
}

func TestRecallDepthNormalization(t *testing.T) {
	if normalizeRecallDepth("DEEP") != "deep" || normalizeRecallDepth("unbounded") != "unbounded" || normalizeRecallDepth("unknown") != "medium" {
		t.Fatal("recall depth normalization contract changed")
	}
}

func TestShellChangeCognitionRequiresRealEstablishedShellTransition(t *testing.T) {
	boot := model.AppearanceState{ShellName: "New", ShellKey: "new"}
	if shouldReactToShellChange(model.AppearanceState{}, boot) {
		t.Fatal("initial shell discovery must not trigger appearance cognition")
	}
	if shouldReactToShellChange(model.AppearanceState{ShellName: "Same", ShellKey: "master"}, model.AppearanceState{ShellName: "Renamed", ShellKey: "master"}) {
		t.Fatal("display-name-only change with the same authoritative shell key must not trigger cognition")
	}
	if !shouldReactToShellChange(model.AppearanceState{ShellName: "Old", ShellKey: "master"}, model.AppearanceState{ShellName: "New", ShellKey: "alternate"}) {
		t.Fatal("real shell-key transition must trigger appearance cognition")
	}
}

func TestLinkedPublicStateDoesNotExposeShellRoutingMetadata(t *testing.T) {
	in := model.CurrentState{Appearance: model.AppearanceState{ShellName: "表示名", ShellKey: "master", ShellPath: `C:\\SSP\\shell\\master\\`, SnapshotComplete: true, Dressup: map[string]any{"glasses": true}}}
	out := publicLinkedState(in)
	if out.Appearance.ShellName != "表示名" || out.Appearance.ShellKey != "" || out.Appearance.ShellPath != "" {
		t.Fatalf("linked public state leaked or lost Shell identity semantics: %#v", out.Appearance)
	}
	if len(out.Appearance.Dressup) == 0 {
		t.Fatal("linked public state must preserve semantic current appearance")
	}
}

func TestLinkedProfileErrorIsSemanticOnly(t *testing.T) {
	d := linkedProfileDocuments{Error: `open C:\\secret\\character\\appearance_master.md: file not found`}
	got := publicProfileError(d)
	if strings.Contains(got, "C:") || strings.Contains(got, "appearance_master.md") || strings.Contains(got, "master") {
		t.Fatalf("linked profile error leaked routing metadata: %q", got)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatal("degraded linked profile should expose a semantic availability notice")
	}
}

func TestAppearanceResultMustMatchAuthoritativeCurrentShell(t *testing.T) {
	env := model.RequestEnvelope{RequestClass: model.RequestAppearance, AppearanceChange: &model.AppearanceTransition{CurrentShellKey: "b"}}
	if !appearanceResultMatchesCurrentShell(env, model.AppearanceState{ShellKey: "b"}) {
		t.Fatal("current appearance result was rejected")
	}
	if appearanceResultMatchesCurrentShell(env, model.AppearanceState{ShellKey: "c"}) {
		t.Fatal("stale appearance result must not affect/present/commit after a newer Shell switch")
	}
	if appearanceResultMatchesCurrentShell(env, model.AppearanceState{}) {
		t.Fatal("appearance result with no authoritative current Shell must be rejected")
	}
}

func TestShellIdentityUsesKeyNotDisplayName(t *testing.T) {
	previous := model.AppearanceState{ShellName: "Old Display", ShellKey: "master", ShellPath: `C:\\SSP\\shell\\master\\`}
	renamed := model.AppearanceState{ShellName: "New Display", ShellKey: "master", ShellPath: `C:\\SSP\\shell\\master\\`}
	if shouldReactToShellChange(previous, renamed) {
		t.Fatal("display-name change on the same directory key must not become appearance cognition")
	}
	moved := model.AppearanceState{ShellName: "Other", ShellKey: "alternate", ShellPath: `C:\\SSP\\shell\\alternate\\`}
	if !shouldReactToShellChange(previous, moved) {
		t.Fatal("different Shell directory key must be treated as a real embodiment transition")
	}
}

func TestSSPUserMirrorUsesSecondaryScopeWithoutCognitionEvent(t *testing.T) {
	got := buildSSPUserMirrorScript("測試\\內容\n第二行")
	if !strings.HasPrefix(got, `\1`) || !strings.HasSuffix(got, `\e`) {
		t.Fatalf("enabled mirror must present on secondary character scope: %q", got)
	}
	if strings.Contains(got, `\p[-100]`) || strings.Contains(got, `\_q`) || strings.Contains(got, `balloonrepaint`) || strings.Contains(got, `\b[-1]`) {
		t.Fatalf("retired hidden-mirror experiments must not remain in the mirror script: %q", got)
	}
	if strings.Contains(got, `\![raise`) || strings.Contains(got, "CHAT|") {
		t.Fatalf("mirror must not raise a SHIORI/cognition event: %q", got)
	}
	if !strings.Contains(got, `\\內容`) || !strings.Contains(got, `\n第二行`) {
		t.Fatalf("mirror text escaping changed: %q", got)
	}
	if escaped := buildSSPUserMirrorScript(`\e`); !strings.Contains(escaped, `\\e`) {
		t.Fatalf("literal cultural \\e must be escaped as presentation text, got %q", escaped)
	}
}

func TestBacklogMirrorSettingIsBoolean(t *testing.T) {
	for _, x := range []string{"1", "true", "on", "yes", "是"} {
		if !parseBacklogMirrorEnabled(x) {
			t.Fatalf("%q should enable mirror", x)
		}
	}
	for _, x := range []string{"0", "false", "off", "no", "否"} {
		if parseBacklogMirrorEnabled(x) {
			t.Fatalf("%q should disable mirror", x)
		}
	}
}

func TestRuntimeDirectiveRecognitionUsesEditableRegistry(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `{
  "format_version": 1,
  "enabled": true,
  "directives": {
    "readmanual": {"kind":"document_query","match":"prefix","aliases":["\\readmanual"],"fallback_aliases":["/readmanual"],"document":"manual"},
    "ukagaka_en_i": {"kind":"semantic_alias","match":"exact","aliases":["えんいー","\\えんいー","\\e"],"meaning":"closing"}
  }
}`
	if err := os.WriteFile(filepath.Join(root, "config", "directive_rules.json"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	a := &app{root: root, log: log.New(io.Discard, "", 0)}
	ref := a.matchDirective("\\readmanual 這段設定是什麼？")
	if ref == nil || ref.ID != "readmanual" || ref.Kind != "document_query" || ref.Argument != "這段設定是什麼？" {
		t.Fatalf("unexpected readmanual directive: %#v", ref)
	}
	adjacent := a.matchDirective("\\readmanual真的不知道?")
	if adjacent == nil || adjacent.ID != "readmanual" || adjacent.Argument != "真的不知道?" {
		t.Fatalf("CJK-adjacent readmanual must be accepted without whitespace: %#v", adjacent)
	}
	for _, bad := range []string{"\\readmanualfoo", "\\readmanual123", "\\readmanual_test", "\\readmanual-next"} {
		if got := a.matchDirective(bad); got != nil {
			t.Fatalf("ASCII command-identifier continuation must not match readmanual: input=%q got=%#v", bad, got)
		}
	}
	if got := userQueryText(model.UserInput{Text: "\\readmanual 之前怎麼設定？", Directive: ref}); got != "這段設定是什麼？" {
		t.Fatalf("directive argument must be the semantic query, got %q", got)
	}
	alias := a.matchDirective("えんいー")
	if alias == nil || alias.ID != "ukagaka_en_i" || alias.Kind != "semantic_alias" {
		t.Fatalf("unexpected cultural alias: %#v", alias)
	}
	for _, cultural := range []string{"\\えんいー", "\\e"} {
		x := a.matchDirective(cultural)
		if x == nil || x.ID != "ukagaka_en_i" {
			t.Fatalf("cultural spelling %q must normalize to ukagaka_en_i: %#v", cultural, x)
		}
	}
	if bad := a.matchDirective("/えんいー"); bad != nil {
		t.Fatalf("invalid slash-en-i spelling must remain ordinary chat: %#v", bad)
	}
	legacy := a.matchDirective("/readmanual 防呆")
	if legacy == nil || legacy.ID != "readmanual" || legacy.Argument != "防呆" {
		t.Fatalf("legacy /readmanual typo must disambiguate to readmanual: %#v", legacy)
	}
	if got := a.matchDirective("今天提到えんいー的由來"); got != nil {
		t.Fatalf("exact cultural alias must not trigger inside ordinary prose: %#v", got)
	}
	for _, unknown := range []string{"/unknown hello", "\\unknown hello"} {
		if got := a.matchDirective(unknown); got != nil {
			t.Fatalf("unknown command-like text must remain ordinary chat: %#v", got)
		}
	}
}
