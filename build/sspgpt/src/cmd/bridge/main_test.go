package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"

	"sspgpt/v07/internal/hotfile"
	"sspgpt/v07/internal/model"
	"strings"
	"testing"
	"time"
)

func TestAppearanceDetailQueryIncludesGlasses(t *testing.T) {
	for _, q := range []string{"慕娜有戴眼鏡嗎", "メガネは？", "wearing glasses?"} {
		if !detailQuery(q, "appearance") {
			t.Fatalf("appearance detail query missed %q", q)
		}
	}
}

func TestPromptDoesNotDefaultOrdinaryTouchToSurprised(t *testing.T) {
	s := &server{}
	env := model.RequestEnvelope{RequestClass: model.RequestPhysical, CurrentState: model.CurrentState{Physical: &model.PhysicalEvent{Gesture: "light_touch", Target: "Head", Contact: true}}}
	prompt := s.buildPrompt(env)
	if !strings.Contains(prompt, "Do not use surprised as the default acknowledgement") {
		t.Fatalf("missing surprise restraint guidance")
	}
}

func TestCanonicalCharacterManifestAndLegacyCleanup(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "character"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"character/character.md":              "CHARACTER-AUTHORITY",
		"character/appearance_master.md":      "APPEARANCE-AUTHORITY",
		"character/manifest.json":             `{"format_version":3,"character_file":"character.md"}`,
		"character/summary.md":                "# Character Profile Summary\n> Local bounded semantic index.\n",
		"character/t.md":                      "<!-- CharacterGPT:RecentPhysicalInteractions:BEGIN -->",
		"character/empty.md":                  "",
		"character/details_.json":             `{"character_file": "character.md", "appearance_file": "appearance.md"}`,
		"config/character_summary_guide.md":   "guide",
		"config/character_summary_rules.json": `{}`,
	}
	for rel, text := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	actions := cleanupLegacyLayout(root)
	if len(actions) < 4 {
		t.Fatalf("expected legacy cleanup actions, got %#v", actions)
	}
	for _, rel := range []string{"summary.md", "t.md", "empty.md", "details_.json"} {
		if _, err := os.Stat(filepath.Join(root, "character", rel)); !os.IsNotExist(err) {
			t.Fatalf("legacy character artifact survived: %s", rel)
		}
	}
	s := &server{root: root}
	ch, ap, _, _, _, _, _, _, _, _, err := s.profileInputs("master")
	if err != nil {
		t.Fatal(err)
	}
	if string(ch) != "CHARACTER-AUTHORITY" || string(ap) != "APPEARANCE-AUTHORITY" {
		t.Fatalf("manifest did not resolve canonical files: ch=%q ap=%q", ch, ap)
	}
}

func TestSafeCharacterFilenameRejectsTraversal(t *testing.T) {
	for _, bad := range []string{"../x.md", "sub/x.md", `sub\\x.md`, ".", ""} {
		if got := safeCharacterFilename(bad, "character.md"); got != "character.md" {
			t.Fatalf("unsafe filename %q accepted as %q", bad, got)
		}
	}
}

func TestConditionsSatisfiedUsesRecentPhysicalAndDialogue(t *testing.T) {
	now := time.Now()
	env := model.RequestEnvelope{
		RequestClass: model.RequestPhysical,
		CurrentState: model.CurrentState{Physical: &model.PhysicalEvent{Target: "Head", Gesture: "stroke"}},
		RecentPhysical: []model.PhysicalEvent{
			{Target: "Head", Gesture: "stroke", ObservedAt: now.Add(-20 * time.Second).Format(time.RFC3339Nano)},
			{Target: "Head", Gesture: "stroke", ObservedAt: now.Add(-5 * time.Second).Format(time.RFC3339Nano)},
		},
		RecentDialogue: []model.DialogueTurn{{Timestamp: now.Add(-30 * time.Second).Format(time.RFC3339Nano), User: "hi"}},
	}
	if !conditionsSatisfied(model.MatchConditions{RepeatWithinSeconds: 90, RepeatCountGTE: 2, RecentChatWithinSeconds: 120}, env) {
		t.Fatal("eligible repeat/recent-chat condition was rejected")
	}
	if conditionsSatisfied(model.MatchConditions{RepeatWithinSeconds: 10, RepeatCountGTE: 2}, env) {
		t.Fatal("repeat window ignored")
	}
}

func TestCharacterDialogueExampleSelectionIsBoundedAndNotMemory(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"character/examples", "config", "profile/generated"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"format_version":3,"character_file":"character.md","example_files":["examples/dialogue.jsonl"]}`
	if err := os.WriteFile(filepath.Join(root, "character", "manifest.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	examples := `{"id":"return","kind":"dialogue","match":{"request_class":["chat"],"text_hints":["回來"]},"situation":"使用者再次出現","user":"我回來了","response":"嗯，歡迎回來。","emotion":"smile"}` + "\n" +
		`{"id":"other","kind":"dialogue","match":{"request_class":["chat"],"text_hints":["天氣"]},"response":"天氣嗎？"}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "character", "examples", "dialogue.jsonl"), []byte(examples), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "reaction_style.json"), []byte(`{"max_examples":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := &server{root: root, hot: hotfile.New()}
	g := s.characterExampleGuidance(model.RequestEnvelope{RequestClass: model.RequestChat, UserInput: model.UserInput{Text: "我回來了"}})
	if !strings.Contains(g, `"id":"return"`) || strings.Contains(g, `"id":"other"`) {
		t.Fatalf("unexpected example selection: %s", g)
	}
	if !strings.Contains(g, "not past events") {
		t.Fatalf("missing example-vs-memory boundary: %s", g)
	}
}

func TestLegacyReactionExamplesMigrateIntoCharacterChannel(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"legacy","target":"Head","gesture":"stroke","situation":"repeat","reaction":"嗯。","emotion":"neutral","conditions":{"repeat_within_seconds":90,"repeat_count_gte":2}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "config", "reaction_examples.jsonl"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	action, ok := migrateLegacyReactionExamples(root)
	if !ok || !strings.Contains(action, "migrated=") {
		t.Fatalf("migration failed: %q", action)
	}
	b, err := os.ReadFile(filepath.Join(root, "character", "examples", "interaction.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var ex model.CharacterExample
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(b))), &ex); err != nil {
		t.Fatal(err)
	}
	if ex.Match.RepeatCountGTE != 2 || ex.Response != "嗯。" {
		t.Fatalf("legacy semantics lost: %#v", ex)
	}
	if _, err := os.Stat(filepath.Join(root, "config", "reaction_examples.jsonl")); !os.IsNotExist(err) {
		t.Fatal("legacy file survived successful migration")
	}
}

func TestReactionStyleRecentContextWindowIsEnforced(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "reaction_style.json"), []byte(`{"max_examples":2,"recent_context_seconds":120}`), 0644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s := &server{root: root, hot: hotfile.New()}
	got := s.recentDialogueForPrompt([]model.DialogueTurn{
		{Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), User: "too old"},
		{Timestamp: now.Add(-30 * time.Second).Format(time.RFC3339Nano), User: "recent"},
	})
	if len(got) != 1 || got[0].User != "recent" {
		t.Fatalf("recent_context_seconds was not enforced: %#v", got)
	}
}

func TestEmbodimentPromptTeachesExactPoseWithoutSurfaceNumbers(t *testing.T) {
	s := &server{}
	env := model.RequestEnvelope{
		RequestClass: model.RequestChat,
		UserInput:    model.UserInput{Text: "把手舉起來看看"},
		Embodiment: &model.EmbodimentCapabilities{
			FormatVersion: 1,
			DefaultPose:   "normal",
			Poses: []model.EmbodimentPose{
				{ID: "normal", Meaning: "ordinary standing pose"},
				{ID: "hand_to_chin", Meaning: "one hand is visibly raised near the chin", Uses: []string{"raising one hand / 舉手 when explicitly requested"}},
			},
			Expressions: []string{"neutral", "smile"},
			Gazes:       []string{"normal", "user"},
		},
	}
	prompt := s.buildPrompt(env)
	for _, want := range []string{"[CURRENT SHELL EMBODIMENT SEMANTICS]", "hand_to_chin", "raising one hand / 舉手", "presentation.gesture never substitutes for pose"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing embodiment guidance %q", want)
		}
	}
	if strings.Contains(prompt, "surface115") || strings.Contains(prompt, "surface 115") {
		t.Fatal("surface renderer IDs must not enter the LLM capability prompt")
	}
}

func TestPresentationSchemaRequiresCanonicalPose(t *testing.T) {
	env := model.RequestEnvelope{Embodiment: &model.EmbodimentCapabilities{
		DefaultPose: "normal",
		Poses:       []model.EmbodimentPose{{ID: "normal", Meaning: "default"}, {ID: "hand_to_chin", Meaning: "raised hand"}},
		Expressions: []string{"neutral", "smile"},
		Gazes:       []string{"normal", "user"},
	}}
	schema := presentationSchema(env)
	required := schema["required"].([]string)
	if !listContains(required, "pose") {
		t.Fatalf("presentation schema does not require pose: %#v", required)
	}
	props := schema["properties"].(map[string]any)
	pose := props["pose"].(map[string]any)
	enum := pose["enum"].([]string)
	if !listContains(enum, "hand_to_chin") || listContains(enum, "raise_hand") {
		t.Fatalf("pose enum must expose canonical pose IDs only: %#v", enum)
	}
}

func listContains(in []string, want string) bool {
	for _, x := range in {
		if x == want {
			return true
		}
	}
	return false
}

func TestProfileCacheAcceptsUnchangedSeedWithoutRebuild(t *testing.T) {
	meta := model.CharacterSummaryMeta{
		FormatVersion: 1,
		Generation:    1,
		SourceHash:    "source",
		GuideHash:     "guide",
		ConfigHash:    "config",
		ModelID:       "seed-v0.7.1-fix5-clean",
	}
	if !profileCacheMatches(false, meta, "source", "guide", "config", true) {
		t.Fatal("unchanged packaged seed should be a valid cache and must not force local Qwen work")
	}
	if profileCacheMatches(true, meta, "source", "guide", "config", true) {
		t.Fatal("explicit rebuild must bypass cache")
	}
}

func TestProfileCacheInvalidatesOnlyRelevantContentChanges(t *testing.T) {
	meta := model.CharacterSummaryMeta{SourceHash: "source-a", GuideHash: "guide-a", ConfigHash: "config-a"}
	cases := []struct {
		name                        string
		source, guide, config       string
		summaryExists, wantCacheHit bool
	}{
		{"unchanged", "source-a", "guide-a", "config-a", true, true},
		{"character changed", "source-b", "guide-a", "config-a", true, false},
		{"guide changed", "source-a", "guide-b", "config-a", true, false},
		{"rules changed", "source-a", "guide-a", "config-b", true, false},
		{"summary missing", "source-a", "guide-a", "config-a", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := profileCacheMatches(false, meta, tc.source, tc.guide, tc.config, tc.summaryExists); got != tc.wantCacheHit {
				t.Fatalf("cache hit=%v want=%v", got, tc.wantCacheHit)
			}
		})
	}
}

func TestProfileSourceUpdatedAtTracksCanonicalDocumentMtime(t *testing.T) {
	root := t.TempDir()
	charDir := filepath.Join(root, "character")
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(charDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(charDir, "character.md"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(charDir, "appearance_master.md"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(charDir, "manifest.json"), []byte(`{"format_version":3,"character_file":"character.md"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "character_summary_guide.md"), []byte("g"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "character_summary_rules.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	newer := old.Add(time.Hour)
	if err := os.Chtimes(filepath.Join(charDir, "character.md"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(charDir, "appearance_master.md"), newer, newer); err != nil {
		t.Fatal(err)
	}
	s := &server{root: root, hot: hotfile.New()}
	_, _, _, _, _, _, _, updated, _, _, err := s.profileInputs("master")
	if err != nil {
		t.Fatal(err)
	}
	want := newer.Format(time.RFC3339Nano)
	if updated != want {
		t.Fatalf("source_updated_at=%q want=%q", updated, want)
	}
}

func TestShellScopedAppearanceUsesExactShellKey(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"character", "config"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	must := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	must("character/character.md", "CHAR")
	must("character/appearance_master.md", "MASTER-ONLY")
	must("character/appearance_alt.md", "ALT-ONLY")
	must("character/manifest.json", `{"format_version":3,"character_file":"character.md"}`)
	must("config/character_summary_guide.md", "guide")
	must("config/character_summary_rules.json", `{}`)
	s := &server{root: root, hot: hotfile.New(), profiles: map[string]*profileState{}}
	_, ap, _, _, _, apName, _, _, _, _, err := s.profileInputs("alt")
	if err != nil {
		t.Fatal(err)
	}
	if apName != "appearance_alt.md" || string(ap) != "ALT-ONLY" {
		t.Fatalf("wrong shell appearance: file=%q body=%q", apName, ap)
	}
}

func TestShellScopedAppearanceNeverFallsBackToGenericOrOtherShell(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"character", "config"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	must := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	must("character/character.md", "CHAR")
	must("character/appearance.md", "LEGACY-GENERIC-MUST-NOT-BE-USED")
	must("character/appearance_master.md", "MASTER-MUST-NOT-BE-USED")
	must("character/manifest.json", `{"format_version":3,"character_file":"character.md"}`)
	must("config/character_summary_guide.md", "guide")
	must("config/character_summary_rules.json", `{}`)
	s := &server{root: root, hot: hotfile.New(), profiles: map[string]*profileState{}}
	_, _, _, _, _, apName, _, _, _, _, err := s.profileInputs("alt")
	if err == nil || apName != "appearance_alt.md" {
		t.Fatalf("missing alt appearance must fail exact lookup: file=%q err=%v", apName, err)
	}
	if !strings.Contains(err.Error(), "appearance_alt.md") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPerShellSummaryCachePathsDoNotCollide(t *testing.T) {
	a, am := profileCachePaths("root", "master")
	b, bm := profileCachePaths("root", "alt")
	if a == b || am == bm || !strings.Contains(a, "character_summary__master") || !strings.Contains(b, "character_summary__alt") {
		t.Fatalf("per-shell cache collision: %q %q %q %q", a, am, b, bm)
	}
}

func TestExactShellKeyRequiresRuntimeKeyAndValidatesPath(t *testing.T) {
	if _, err := exactShellKey("", `C:/SSP/shell/master/`, "Display"); err == nil {
		t.Fatal("Bridge must not derive current shell authority when Runtime shell_key is absent")
	}
	if got, err := exactShellKey("master", `C:/SSP/shell/master/`, "表示名"); err != nil || got != "master" {
		t.Fatalf("valid Runtime shell key rejected: got=%q err=%v", got, err)
	}
	if _, err := exactShellKey("other", `C:/SSP/shell/master/`, "表示名"); err == nil {
		t.Fatal("shell key/path mismatch must be rejected")
	}
}

func TestShellDisplayNameDoesNotBecomeAppearanceFilename(t *testing.T) {
	if got := appearanceFileForShell("master"); got != "appearance_master.md" {
		t.Fatalf("appearance file=%q", got)
	}
	if strings.Contains(appearanceFileForShell("master"), "カフェオレ") {
		t.Fatal("display name leaked into filesystem key")
	}
}

func TestAppearanceChangePromptUsesCurrentShellAppearanceAndStructuredTransition(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"character", "config", "profile/generated"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"character/character.md":              "MUNA-CHARACTER",
		"character/appearance_alt.md":         "ALT-SHELL-APPEARANCE",
		"character/manifest.json":             `{"format_version":3,"character_file":"character.md"}`,
		"config/runtime_context_rules.json":   `{}`,
		"config/reaction_style.json":          `{}`,
		"config/character_summary_guide.md":   "guide",
		"config/character_summary_rules.json": `{}`,
	}
	for rel, text := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.WriteFile(p, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := &server{root: root, hot: hotfile.New(), log: log.New(io.Discard, "", 0), profiles: map[string]*profileState{}}
	env := model.RequestEnvelope{
		RequestID:        "appearance-1",
		RequestClass:     model.RequestAppearance,
		Source:           "appearance",
		CurrentState:     model.CurrentState{Appearance: model.AppearanceState{ShellName: "Alt Display", ShellKey: "alt", ShellPath: `C:\\secret\\shell\\alt\\`, SnapshotComplete: false}},
		AppearanceChange: &model.AppearanceTransition{Kind: "shell_changed", PreviousShellName: "Old Display", PreviousShellKey: "master", CurrentShellName: "Alt Display", CurrentShellKey: "alt"},
	}
	prompt := s.buildPrompt(env)
	for _, want := range []string{"ALT-SHELL-APPEARANCE", "[CURRENT APPEARANCE CHANGE]", "previous_shell=Old Display", "current_shell=Alt Display", "React to the appearance change that just occurred."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("appearance-change prompt missing %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{`C:\\secret\\shell\\alt`, `"shell_key": "alt"`, `"shell_path"`, "appearance_alt.md", "character.md", "source_hash"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt leaked Runtime/Bridge routing metadata %q: %s", forbidden, prompt)
		}
	}
}

func TestReplayPromptIsChronologicalAndSkipsDuplicateRecentDialogue(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"character", "config", "profile/generated"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"character/character.md":              "MUNA",
		"character/appearance_master.md":      "APPEARANCE",
		"character/manifest.json":             `{"format_version":3,"character_file":"character.md"}`,
		"config/runtime_context_rules.json":   `{}`,
		"config/reaction_style.json":          `{"recent_context_seconds":120}`,
		"config/character_summary_guide.md":   "guide",
		"config/character_summary_rules.json": `{}`,
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := &server{root: root, hot: hotfile.New(), log: log.New(io.Discard, "", 0), profiles: map[string]*profileState{}}
	env := model.RequestEnvelope{
		RequestID: "r", RequestClass: model.RequestChat, Source: "chat",
		UserInput:      model.UserInput{Text: "我們之前怎麼決定的？"},
		CurrentState:   model.CurrentState{Appearance: model.AppearanceState{ShellName: "Master", ShellKey: "master", ShellPath: filepath.Join(root, "shell", "master")}},
		RecentDialogue: []model.DialogueTurn{{Timestamp: model.Now(), User: "DUPLICATE-RECENT", Character: "DUPLICATE"}},
		MemoryCapsule: model.MemoryCapsule{RecallMode: "replay", Replay: []model.DialogueTurn{
			{Timestamp: "2026-08-19T01:00:00Z", User: "OLDER", Character: "OLD-ANSWER"},
			{Timestamp: "2026-08-19T02:00:00Z", User: "NEWER", Character: "NEW-ANSWER"},
		}},
	}
	prompt := s.buildPromptWithSettings(env, settings{ContextWindowTokens: 8192, MaxOutputTokens: 420, ContextSafetyMarginTokens: 1024})
	if !strings.Contains(prompt, "[CHRONOLOGICAL REPLAY - RAW DIALOGUE]") || !strings.Contains(prompt, "OLDER") || !strings.Contains(prompt, "NEWER") {
		t.Fatalf("replay missing: %s", prompt)
	}
	if strings.Index(prompt, "OLDER") > strings.Index(prompt, "NEWER") {
		t.Fatal("replay must be restored to chronological order")
	}
	if strings.Contains(prompt, "DUPLICATE-RECENT") {
		t.Fatal("recent dialogue must not be duplicated when full replay is present")
	}
}

func TestFitReplayForPromptKeepsNewestContiguousHistory(t *testing.T) {
	in := []model.DialogueTurn{
		{User: strings.Repeat("舊", 20), Character: "A"},
		{User: strings.Repeat("中", 20), Character: "B"},
		{User: strings.Repeat("新", 20), Character: "C"},
	}
	lastCost := replayPromptTurnCost(in[2])
	got, used := fitReplayForPrompt(in, lastCost+4)
	if len(got) != 1 || got[0].Character != "C" || used != lastCost {
		t.Fatalf("provider guard must keep newest contiguous replay: used=%d got=%#v", used, got)
	}
}

func TestDocumentDirectiveInjectsRegisteredCharacterDocument(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"character", "config", "profile/generated"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"character/character.md":              "MUNA",
		"character/appearance_master.md":      "APPEARANCE",
		"character/manual.md":                 "MANUAL-ONLY-FACT: the owl button opens the link menu.",
		"character/manifest.json":             `{"format_version":3,"character_file":"character.md"}`,
		"config/runtime_context_rules.json":   `{}`,
		"config/reaction_style.json":          `{}`,
		"config/character_summary_guide.md":   "guide",
		"config/character_summary_rules.json": `{}`,
		"config/directive_rules.json": `{
  "format_version":1,
  "enabled":true,
  "documents":{"manual":{"label":"Manual","path":"character/manual.md","max_context_tokens":1000}},
  "directives":{"readmanual":{"kind":"document_query","match":"prefix","aliases":["\\readmanual"],"fallback_aliases":["/readmanual"],"document":"manual"}}
}`,
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := &server{root: root, hot: hotfile.New(), log: log.New(io.Discard, "", 0), profiles: map[string]*profileState{}}
	env := model.RequestEnvelope{
		RequestID:    "directive-1",
		RequestClass: model.RequestChat,
		Source:       "chat",
		UserInput: model.UserInput{
			Text:      "\\readmanual 這個按鈕做什麼？",
			Directive: &model.DirectiveRef{ID: "readmanual", Kind: "document_query", InvokedAs: "\\readmanual", Argument: "這個按鈕做什麼？"},
		},
		CurrentState: model.CurrentState{Appearance: model.AppearanceState{ShellName: "Master", ShellKey: "master"}},
	}
	prompt := s.buildPromptWithSettings(env, settings{ContextWindowTokens: 16000, MaxOutputTokens: 420, ContextSafetyMarginTokens: 1024})
	for _, want := range []string{"[ACTIVE COGNITION DIRECTIVE]", "kind=document_query", "[DIRECTIVE DOCUMENT: Manual]", "MANUAL-ONLY-FACT", "[CURRENT USER INPUT]\n這個按鈕做什麼？"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("directive prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "character/manual.md") {
		t.Fatalf("directive prompt leaked local document path: %s", prompt)
	}
}

func TestSemanticAliasDirectiveUsesRegisteredMeaningNaturally(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"character", "config", "profile/generated"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"character/character.md":              "MUNA",
		"character/appearance_master.md":      "APPEARANCE",
		"character/manifest.json":             `{"format_version":3,"character_file":"character.md"}`,
		"config/runtime_context_rules.json":   `{}`,
		"config/reaction_style.json":          `{}`,
		"config/character_summary_guide.md":   "guide",
		"config/character_summary_rules.json": `{}`,
		"config/directive_rules.json": `{
  "format_version":1,
  "enabled":true,
  "directives":{"ukagaka_en_i":{"kind":"semantic_alias","match":"exact","aliases":["えんいー"],"culture":"ukagaka","meaning":"SakuraScript end-tag culture and customary farewell"}}
}`,
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := &server{root: root, hot: hotfile.New(), log: log.New(io.Discard, "", 0), profiles: map[string]*profileState{}}
	env := model.RequestEnvelope{
		RequestID:    "directive-2",
		RequestClass: model.RequestChat,
		Source:       "chat",
		UserInput:    model.UserInput{Text: "えんいー", Directive: &model.DirectiveRef{ID: "ukagaka_en_i", Kind: "semantic_alias", InvokedAs: "えんいー"}},
		CurrentState: model.CurrentState{Appearance: model.AppearanceState{ShellName: "Master", ShellKey: "master"}},
	}
	prompt := s.buildPromptWithSettings(env, settings{ContextWindowTokens: 16000, MaxOutputTokens: 420, ContextSafetyMarginTokens: 1024})
	for _, want := range []string{"kind=semantic_alias", "culture=ukagaka", "meaning=SakuraScript end-tag culture and customary farewell", "[CURRENT USER INPUT]\nえんいー"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("semantic alias prompt missing %q: %s", want, prompt)
		}
	}
}

func TestDirectiveDocumentPathCannotEscapeCharacterTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "directive_rules.json"), []byte(`{
  "format_version":1,"enabled":true,
  "documents":{"manual":{"path":"profile/secrets.txt"}},
  "directives":{"readmanual":{"kind":"document_query","aliases":["\\readmanual"],"fallback_aliases":["/readmanual"],"document":"manual"}}
}`), 0644); err != nil {
		t.Fatal(err)
	}
	s := &server{root: root, hot: hotfile.New(), log: log.New(io.Discard, "", 0)}
	env := model.RequestEnvelope{RequestID: "r", UserInput: model.UserInput{Directive: &model.DirectiveRef{ID: "readmanual", Kind: "document_query"}}}
	got := s.directiveGuidance(env, settings{ContextWindowTokens: 16000, MaxOutputTokens: 420, ContextSafetyMarginTokens: 1024})
	if !strings.Contains(got, "registered document path is invalid") || strings.Contains(got, "profile/secrets.txt") {
		t.Fatalf("unsafe document directive was not safely degraded: %s", got)
	}
}
