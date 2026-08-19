package main

import (
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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sspgpt/v07/internal/directive"
	"sspgpt/v07/internal/httpjson"
	"sspgpt/v07/internal/model"
	"sspgpt/v07/internal/paths"
	"sspgpt/v07/internal/profilepath"
	"sspgpt/v07/internal/shellid"
	"sspgpt/v07/internal/singleinstance"
	"sspgpt/v07/internal/sstp"
	"sspgpt/v07/internal/ws"
)

const version = "0.7.1-fix13f"

type inputUIConfig struct {
	Emotions         []string `json:"emotions"`
	SSPBacklogMirror bool     `json:"ssp_backlog_mirror"`
	CheckThresholds  []struct {
		Label        string `json:"label"`
		Milliseconds int64  `json:"milliseconds"`
	} `json:"check_thresholds"`
	DefaultEmotion string `json:"default_emotion"`
	DefaultCheckMS int64  `json:"default_check_ms"`
}

type autonomousConfig struct {
	Enabled                      bool  `json:"autonomous_enabled"`
	TickIntervalMS               int64 `json:"autonomous_tick_interval_ms"`
	MinimumIdleMS                int64 `json:"minimum_idle_before_tick_ms"`
	MaximumPendingContinuations  int   `json:"maximum_pending_continuations"`
	MinimumDeferMS               int64 `json:"minimum_defer_ms"`
	MaximumDeferMS               int64 `json:"maximum_defer_ms"`
	ContinuationGraceMS          int64 `json:"continuation_grace_ms"`
	CancelDeferredOnNewUserInput bool  `json:"cancel_deferred_on_new_user_input"`
	AllowAutonomousDuringTouch   bool  `json:"allow_autonomous_during_touch"`
}

type physicalResponseRules struct {
	LowerImpulseGuardMS           int64 `json:"lower_impulse_guard_ms"`
	RestingReactionAfterMS        int64 `json:"resting_reaction_after_ms"`
	SupersedeInflightOnEscalation bool  `json:"supersede_inflight_on_escalation"`
}

type auditRules struct {
	Affect    bool `json:"affect_audit"`
	Cognition bool `json:"cognition_audit"`
	Memory    bool `json:"memory_audit"`
}

type touchSession struct {
	CharacterID, Target, SessionID string
	Started, Last                  time.Time
	LastX, lastY, Path, SpeedSum   float64
	Samples, Reversals             int
	LastDX, LastDY                 float64
	EmittedGesture                 string
	LastProgressMS                 int64
}

type requestJob struct {
	env          model.RequestEnvelope
	ctx          context.Context
	cancel       context.CancelFunc
	affectBefore model.AffectState
	physical     *model.PhysicalEvent
}

type lifecycle struct {
	RequestID, State, Source, Class string
	CheckAfterMS                    int64
	Created                         time.Time
	cancel                          context.CancelFunc
	SessionID                       string
	Impulse                         float64
}

type presentationMark struct {
	Impulse float64
	Gesture string
	At      time.Time
}

type continuationCapsule struct {
	Ref     model.ContinuationRef
	Created time.Time
}

type linkedTurn struct {
	ExternalTurnID  string
	Phase           string
	Created         time.Time
	AffectBefore    model.AffectState
	ThinkingUpdates int
	Committed       bool
}

type linkedSession struct {
	SessionID string
	Activated time.Time
	LastSeen  time.Time
	Turn      *linkedTurn
}

type app struct {
	root             string
	log              *log.Logger
	affectAudit      *log.Logger
	cognitionAudit   *log.Logger
	mu               sync.Mutex
	affect           model.AffectState
	currentPhysical  *model.PhysicalEvent
	sessions         map[string]*touchSession
	lastPresentation map[string]presentationMark
	life             map[string]*lifecycle
	physicalActive   map[string]string
	activeChat       string
	activeAppearance string
	hot              model.HotMemorySnapshot
	continuations    map[string]continuationCapsule
	linked           *linkedSession
	linkedCommitted  map[string]bool
	recentDialogue   []model.DialogueTurn
	recentPhysical   []model.PhysicalEvent
	wsConn           *ws.Conn
	ui               ChatUI
	seq              atomic.Uint64
	balloonMS        int
	recallDepth      string
	backlogMirror    bool
	lastUserActivity time.Time
	shutdownOnce     sync.Once
}

func main() {
	root := paths.GhostRoot()
	if !singleinstance.Acquire("Runtime", root) {
		return
	}
	_ = os.MkdirAll(filepath.Join(root, "logs"), 0755)
	lf, _ := os.OpenFile(filepath.Join(root, "logs", "runtime.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	af, _ := os.OpenFile(filepath.Join(root, "logs", "affect_audit.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	cf, _ := os.OpenFile(filepath.Join(root, "logs", "cognition_audit.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	a := &app{root: root, log: log.New(lf, "", log.LstdFlags|log.Lmicroseconds), affectAudit: log.New(af, "", log.LstdFlags|log.Lmicroseconds), cognitionAudit: log.New(cf, "", log.LstdFlags|log.Lmicroseconds), sessions: map[string]*touchSession{}, lastPresentation: map[string]presentationMark{}, life: map[string]*lifecycle{}, physicalActive: map[string]string{}, continuations: map[string]continuationCapsule{}, linkedCommitted: map[string]bool{}, balloonMS: 15000, recallDepth: "medium", backlogMirror: true, lastUserActivity: time.Now()}
	for _, action := range profilepath.MigrateRuntime(root) {
		a.log.Printf("PROFILE_LAYOUT %s", action)
	}
	a.loadAffect()
	a.auditAffectf("AFFECT_LOAD revision=%d primary=%s intensity=%.4f current=%s", a.affect.Revision, a.affect.Primary, a.affect.Intensity, formatAffect(a.affect))
	a.loadSettings()
	a.ui = NewChatUI(a.submitChatFromUI, a.checkFromUI, a.log.Printf)
	go a.internalHTTP()
	go a.pullHot()
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleWS)
	addr := "127.0.0.1:8766"
	a.log.Printf("Runtime %s listening ws=%s internal=127.0.0.1:8770 root=%s", version, addr, root)
	if err := http.ListenAndServe(addr, mux); err != nil {
		a.log.Fatal(err)
	}
}

func (a *app) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := ws.Upgrade(w, r)
	if err != nil {
		return
	}
	a.mu.Lock()
	a.wsConn = c
	a.mu.Unlock()
	a.log.Printf("WS connected")
	defer func() {
		a.mu.Lock()
		if a.wsConn == c {
			a.wsConn = nil
		}
		a.mu.Unlock()
		c.Close()
		a.log.Printf("WS disconnected")
	}()
	for {
		msg, err := c.ReadText()
		if err != nil {
			return
		}
		a.handleMessage(strings.TrimSpace(msg))
	}
}

func (a *app) handleMessage(msg string) {
	if msg == "" {
		return
	}
	p := strings.Split(msg, "|")
	switch p[0] {
	case "SYSTEM":
		if len(p) > 1 && p[1] == "CLOSE" {
			a.beginShutdown("websocket")
		} else if len(p) > 1 && p[1] == "BOOT" {
			go a.reportAPIStatus("boot")
			go a.pullHot()
			a.armAutonomousTimer()
		}
	case "COMMAND":
		if len(p) > 1 && p[1] == "OPEN_CHAT" {
			a.ui.Open(a.inputConfig())
		}
	case "CONFIG":
		a.handleConfig(p)
	case "QUERY":
		if len(p) > 1 && p[1] == "AFFECT" {
			reason := "restore"
			if len(p) > 2 && p[2] != "" {
				reason = p[2]
			}
			a.sendAffectSnapshot(reason)
		}
	case "CONTROL":
		a.handleControl(p)
	case "POINTER":
		a.handlePointer(p)
	case "CHAT":
		if len(p) >= 2 {
			emotion := "neutral"
			check := int64(15000)
			if len(p) >= 3 && p[2] != "" {
				emotion = p[2]
			}
			if len(p) >= 4 {
				if n, e := strconv.ParseInt(p[3], 10, 64); e == nil {
					check = n
				}
			}
			a.submitChatFromUI(p[1], emotion, check)
		}
	case "MOVE":
		a.handleMove(p)
	case "AUTONOMOUS":
		if len(p) > 1 && p[1] == "SKIP" {
			a.log.Printf("AUTONOMOUS_SKIP reason=presentation_busy")
			a.armAutonomousTimer()
		} else {
			a.handleAutonomousTick()
		}
	case "DEFERRED":
		if len(p) >= 2 {
			a.handleDeferred(p[1])
		}
	}
}

func (a *app) handleConfig(p []string) {
	if len(p) < 2 {
		return
	}
	switch p[1] {
	case "APIKEY":
		if len(p) >= 3 {
			key := strings.Join(p[2:], "|")
			ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
			err := httpjson.Post(ctx, "http://127.0.0.1:8767/v1/config/apikey", map[string]string{"api_key": key}, nil)
			c()
			if err != nil {
				a.sendPresentation("\\![raise,OnCharacterGPTAPIKeyStatus,error]")
			} else {
				go a.reportAPIStatus("set")
			}
		}
	case "CLEARKEY":
		req, _ := http.NewRequest(http.MethodDelete, "http://127.0.0.1:8767/v1/config/apikey", nil)
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if resp != nil {
			resp.Body.Close()
		}
		c()
		if err != nil {
			a.sendPresentation("\\![raise,OnCharacterGPTAPIKeyStatus,error]")
		} else {
			go a.reportAPIStatus("clear")
		}
	case "CHECKKEY":
		go a.reportAPIStatus("manual")
	case "BALLOON":
		if len(p) >= 3 {
			n, _ := strconv.Atoi(p[2])
			a.mu.Lock()
			a.balloonMS = n * 1000
			a.mu.Unlock()
			a.saveSettings()
		}
	case "RECALL_DEPTH":
		if len(p) >= 3 {
			depth := normalizeRecallDepth(p[2])
			a.mu.Lock()
			a.recallDepth = depth
			a.mu.Unlock()
			a.saveSettings()
			a.log.Printf("RECALL_DEPTH_SET depth=%s", depth)
		}
	case "BACKLOG_MIRROR":
		if len(p) >= 3 {
			enabled := parseBacklogMirrorEnabled(p[2])
			a.mu.Lock()
			a.backlogMirror = enabled
			a.mu.Unlock()
			a.saveSettings()
			a.log.Printf("SSP_USER_MIRROR_SET enabled=%t", enabled)
		}
	}
}

func (a *app) sendAffectSnapshot(reason string) {
	switch reason {
	case "restore", "idle":
	default:
		reason = "restore"
	}
	a.mu.Lock()
	aff, elapsedMS, factor := a.decayAffectLocked()
	a.mu.Unlock()
	script := buildAffectSnapshotScript(reason, aff)
	a.sendPresentation(script)
	a.auditAffectf("AFFECT_PRESENTATION reason=%s revision=%d decay_elapsed_ms=%d decay_factor=%.6f primary=%s intensity=%.4f current=%s", reason, aff.Revision, elapsedMS, factor, aff.Primary, aff.Intensity, formatAffect(aff))
}

func buildAffectSnapshotScript(reason string, aff model.AffectState) string {
	ch := aff.Channels
	if ch == nil {
		ch = map[string]float64{}
	}
	return fmt.Sprintf("\\C\\![raise,OnCharacterGPTAffectSnapshot,%s,%.4f,%.4f,%.4f,%.4f,%.4f,%d,%s]\\e",
		reason, ch["positive"], ch["shy"], ch["wary"], ch["annoyed"], ch["downcast"], aff.Revision, sstp.EscapeSakuraText(aff.Primary))
}

func (a *app) reportAPIStatus(reason string) {
	var x struct {
		State, Model string
		Mock         bool
	}
	var err error
	for i := 0; i < 4; i++ {
		ctx, c := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		err = httpGetJSON(ctx, "http://127.0.0.1:8767/v1/status", &x)
		c()
		if err == nil {
			break
		}
		time.Sleep(time.Duration(150+i*150) * time.Millisecond)
	}
	if err != nil {
		x.State = "unavailable"
	}
	a.log.Printf("API_KEY_STATUS reason=%s state=%s model=%s mock=%t", reason, x.State, x.Model, x.Mock)
	a.sendPresentation("\\![raise,OnCharacterGPTAPIKeyStatus," + sstp.EscapeSakuraText(x.State) + "]")
}

func (a *app) handleControl(p []string) {
	if len(p) < 3 {
		return
	}
	action, id := p[1], p[2]
	a.mu.Lock()
	lc := a.life[id]
	a.mu.Unlock()
	if lc == nil {
		return
	}
	switch action {
	case "CONTINUE":
		a.mu.Lock()
		if lc.State == "processing_checkable" {
			lc.State = "processing"
		}
		a.mu.Unlock()
		a.ui.RearmCheck(lc.CheckAfterMS)
		a.armCheckable(id, lc.CheckAfterMS)
	case "CANCEL":
		a.cancelRequest(id, "user")
	}
}

func (a *app) submitChatFromUI(text, emotion string, checkMS int64) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.log.Printf("UI_SUBMIT received chars=%d emotion=%s check_after_ms=%d", len([]rune(text)), emotion, checkMS)
	if a.linkedBusy() && !a.linkedRules().AllowLocalChatDuringActiveTurn {
		a.log.Printf("UI_SUBMIT suppressed reason=linked_turn_active")
		a.sendPresentation("\\![raise,OnCharacterGPTLinkedBusy]\\e")
		return
	}
	a.mu.Lock()
	if a.activeChat != "" {
		a.mu.Unlock()
		return
	}
	cfg := a.autonomousConfig()
	if cfg.CancelDeferredOnNewUserInput {
		a.continuations = map[string]continuationCapsule{}
	}
	a.lastUserActivity = time.Now()
	a.mu.Unlock()
	id := a.newID("chat")
	input := model.UserInput{Text: text, UserEmotion: emotion}
	if ref := a.matchDirective(text); ref != nil {
		input.Directive = ref
		a.log.Printf("DIRECTIVE_MATCH request=%s id=%s kind=%s invoked=%q arg_chars=%d", id, ref.ID, ref.Kind, ref.InvokedAs, len([]rune(ref.Argument)))
	}
	env := a.buildEnvelope(id, model.RequestChat, "chat", input, nil, checkMS)
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.life[id] = &lifecycle{RequestID: id, State: "processing", Source: "chat", Class: model.RequestChat, CheckAfterMS: checkMS, Created: time.Now(), cancel: cancel}
	a.activeChat = id
	before := cloneAffect(a.affect)
	a.mu.Unlock()
	a.ui.SetProcessing(id, checkMS)
	a.armCheckable(id, checkMS)
	// Accepted dialogue is history/presentation fan-out only. It must never
	// create a second cognition request or affect transition.
	go a.recordAcceptedUser(id, text)
	if a.currentBacklogMirrorEnabled() {
		go a.mirrorUserInputToSSP(id, text)
	}
	go a.runJob(requestJob{env: env, ctx: ctx, cancel: cancel, affectBefore: before})
	a.log.Printf("REQUEST_CREATED id=%s class=chat route=%s user_emotion=%s check_after_ms=%d", id, routeName(env), emotion, checkMS)
}

func (a *app) recordAcceptedUser(requestID, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	payload := map[string]any{
		"request_id": requestID,
		"kind":       "user_accepted",
		"timestamp":  model.Now(),
		"user":       text,
	}
	if err := httpjson.Post(ctx, "http://127.0.0.1:8768/v2/dialogue", payload, nil); err != nil {
		a.log.Printf("RAW_DIALOGUE_ACCEPT request=%s result=degraded error=%v", requestID, err)
	} else {
		a.log.Printf("RAW_DIALOGUE_ACCEPT request=%s result=ok chars=%d", requestID, len([]rune(text)))
	}
}

// SSP user-input mirroring is presentation-only. Accepted user text is sent
// once to character scope 1 so SSP can include it in the native presentation
// history. No SHIORI event is raised back into Runtime, so the mirrored text
// cannot become a duplicate cognition turn. The feature is simply on/off;
// Raw Replay is recorded independently and remains authoritative for recall.
func parseBacklogMirrorEnabled(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "off", "no", "否":
		return false
	default:
		return true
	}
}

func buildSSPUserMirrorScript(text string) string {
	escaped := sstp.EscapeSakuraText(text)
	return "\\1" + escaped + "\\e"
}

func (a *app) currentBacklogMirrorEnabled() bool {
	a.mu.Lock()
	enabled := a.backlogMirror
	a.mu.Unlock()
	return enabled
}

func (a *app) mirrorUserInputToSSP(requestID, text string) {
	script := buildSSPUserMirrorScript(text)
	if err := sstp.SendScript("SSPGPT Runtime User Mirror", script); err != nil {
		a.log.Printf("SSP_USER_MIRROR request=%s result=degraded error=%v", requestID, err)
		return
	}
	a.log.Printf("SSP_USER_MIRROR request=%s result=sent chars=%d", requestID, len([]rune(text)))
}

func (a *app) checkFromUI() {
	a.mu.Lock()
	id := a.activeChat
	lc := a.life[id]
	eligible := lc != nil && lc.State == "processing_checkable"
	a.mu.Unlock()
	if eligible && id != "" {
		a.sendPresentation("\\![raise,OnCharacterGPTPendingCheck," + sstp.EscapeSakuraText(id) + "]")
	}
}
func (a *app) armCheckable(id string, ms int64) {
	mark := func() {
		changed := false
		a.mu.Lock()
		if lc := a.life[id]; lc != nil && lc.State == "processing" {
			lc.State = "processing_checkable"
			changed = true
			a.log.Printf("REQUEST_CHECKABLE id=%s", id)
		}
		a.mu.Unlock()
		if changed {
			a.ui.SetCheckable()
		}
	}
	if ms <= 0 {
		mark()
		return
	}
	time.AfterFunc(time.Duration(ms)*time.Millisecond, mark)
}

func (a *app) cancelRequest(id, reason string) {
	a.mu.Lock()
	lc := a.life[id]
	if lc == nil {
		a.mu.Unlock()
		return
	}
	if reason == "superseded" {
		lc.State = "superseded"
	} else {
		lc.State = "cancel_requested"
	}
	c := lc.cancel
	a.mu.Unlock()
	a.log.Printf("CANCEL_REQUESTED id=%s reason=%s", id, reason)
	if c != nil {
		c()
	}
	ctx, cc := context.WithTimeout(context.Background(), 900*time.Millisecond)
	_ = httpjson.Post(ctx, "http://127.0.0.1:8767/v1/cancel", map[string]string{"request_id": id}, nil)
	cc()
}
func (a *app) cancelAll() {
	a.mu.Lock()
	var cs []context.CancelFunc
	for _, x := range a.life {
		if x.cancel != nil {
			cs = append(cs, x.cancel)
		}
	}
	a.mu.Unlock()
	for _, c := range cs {
		c()
	}
}

// beginShutdown is the single Runtime-owned shutdown coordinator.  It is
// deliberately callable from both the legacy websocket close notification and
// the authoritative loopback HTTP endpoint because SSP may tear down its
// websocket before the final frame is delivered.
func (a *app) beginShutdown(reason string) bool {
	started := false
	a.shutdownOnce.Do(func() {
		started = true
		a.log.Printf("SHUTDOWN_BEGIN reason=%s", reason)
		a.cancelAll()
		if a.ui != nil {
			a.ui.Close()
		}
		go a.shutdownServices()
	})
	return started
}

func (a *app) shutdownServices() {
	// Ownership order: stop optional/external work, then sensing/foreground
	// cognition, and MemoryService last so any already-completed foreground
	// response has the best chance to finish its EpisodeCommit journal write.
	services := []struct {
		name    string
		url     string
		timeout time.Duration
	}{
		{"ContextService", "http://127.0.0.1:8782/shutdown", 900 * time.Millisecond},
		{"TouchProgress", "http://127.0.0.1:8769/shutdown", 900 * time.Millisecond},
		{"Bridge", "http://127.0.0.1:8767/shutdown", 1200 * time.Millisecond},
		{"MemoryService", "http://127.0.0.1:8768/shutdown", 1500 * time.Millisecond},
	}
	for _, svc := range services {
		ctx, cancel := context.WithTimeout(context.Background(), svc.timeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, svc.url, nil)
		if err == nil {
			resp, doErr := http.DefaultClient.Do(req)
			if resp != nil {
				resp.Body.Close()
			}
			if doErr != nil {
				a.log.Printf("SHUTDOWN_SERVICE name=%s result=unavailable error=%v", svc.name, doErr)
			} else {
				a.log.Printf("SHUTDOWN_SERVICE name=%s result=accepted", svc.name)
			}
		}
		cancel()
	}
	a.log.Printf("SHUTDOWN_COMPLETE coordinator=Runtime")
	time.Sleep(180 * time.Millisecond)
	os.Exit(0)
}

func (a *app) handlePointer(p []string) {
	if len(p) < 6 {
		return
	}
	kind, cid, target := p[1], p[2], canonicalTarget(p[2], p[3])
	gesture, intensity := "light_touch", .2
	if kind == "double" {
		gesture, intensity = "heavy_tap", .55
	}
	if target == "Book" && kind == "single" {
		gesture, intensity = "look_at", .05
	}
	if (target == "Bust" || target == "Book") && kind == "double" {
		gesture, intensity = "grab", .65
	}
	ev := &model.PhysicalEvent{Type: "physical", Gesture: gesture, Target: target, CharacterID: cid, Phase: "instant", Contact: false, Released: gesture != "look_at", ObservedAt: model.Now(), Authoritative: true, Intensity: intensity}
	a.noteUserActivity()
	a.submitPhysical(ev)
}
func canonicalTarget(cid, target string) string {
	if cid == "1" && !strings.HasPrefix(target, "Owl.") {
		return "Owl." + target
	}
	return target
}
func sessionKey(cid, target string) string { return cid + "|" + target }

func (a *app) handleMove(p []string) {
	if len(p) < 6 {
		return
	}
	cid := p[1]
	target := canonicalTarget(cid, p[2])
	x, _ := strconv.ParseFloat(p[3], 64)
	y, _ := strconv.ParseFloat(p[4], 64)
	boundary := len(p) > 6 && strings.Contains(p[6], "boundary=1")
	now := time.Now()
	k := sessionKey(cid, target)
	a.noteUserActivity()
	a.mu.Lock()
	superseded := a.dropConflictingSessionsLocked(cid, target)
	s := a.sessions[k]
	// A non-boundary MOVE is only a continuation. If Runtime has no matching
	// session, it must not invent contact=true from that packet alone. YAYA's
	// first authoritative MOVE is marked boundary=1 and TouchProgress owns the
	// body lifecycle. Ignoring an orphan continuation is safer than creating a
	// ghost contact that can survive into later chat prompts.
	if s == nil && !boundary {
		a.mu.Unlock()
		for _, old := range superseded {
			a.log.Printf("PHYSICAL_SESSION_SUPERSEDED session=%s target=%s character=%s replacement=%s reason=new_move_target", old.SessionID, old.Target, old.CharacterID, target)
		}
		a.log.Printf("PHYSICAL_MOVE_IGNORED target=%s character=%s reason=orphan_without_boundary", target, cid)
		return
	}
	if s != nil && boundary && !canResumeProvisional(s, now) {
		age := now.Sub(s.Last)
		if age < 0 {
			age = 0
		}
		a.log.Printf("PHYSICAL_SESSION_REPLACED session=%s target=%s character=%s reason=provisional_reentry_expired age_ms=%d", s.SessionID, target, cid, age.Milliseconds())
		delete(a.sessions, k)
		delete(a.physicalActive, s.SessionID)
		s = nil
	}
	if s == nil {
		s = &touchSession{CharacterID: cid, Target: target, SessionID: fmt.Sprintf("r-%d", now.UnixNano()), Started: now, Last: now, LastX: x, lastY: y}
		a.sessions[k] = s
		a.log.Printf("PHYSICAL_SESSION_START session=%s target=%s character=%s boundary=%t", s.SessionID, target, cid, boundary)
	} else if boundary {
		a.log.Printf("PHYSICAL_SESSION_RESUME session=%s target=%s character=%s reason=provisional_reentry", s.SessionID, target, cid)
	}
	dt := now.Sub(s.Last).Seconds()
	dx, dy := x-s.LastX, y-s.lastY
	dist := math.Hypot(dx, dy)
	if dt > .001 && dt < 1.5 && dist > 0 {
		s.Path += dist
		sp := dist / dt
		s.SpeedSum += sp
		s.Samples++
		if s.Samples > 2 && dist > 2 {
			dot := dx*s.LastDX + dy*s.LastDY
			if dot < 0 && math.Hypot(s.LastDX, s.LastDY) > 2 {
				s.Reversals++
			}
		}
		s.LastDX, s.LastDY = dx, dy
	}
	s.LastX, s.lastY = x, y
	s.Last = now
	gesture, intensity, speed := classify(s)
	a.currentPhysical = &model.PhysicalEvent{Type: "physical", Gesture: gesture, Target: target, CharacterID: cid, Phase: "moving", Contact: true, Moving: true, SessionID: s.SessionID, DurationMS: now.Sub(s.Started).Milliseconds(), Speed: speed, Reversals: s.Reversals, ObservedAt: model.Now(), Authoritative: true, Intensity: intensity}
	emit := gesture != "" && s.Path >= 20 && now.Sub(s.Started) >= 350*time.Millisecond && (s.EmittedGesture == "" || a.impulse(gesture) > a.impulse(s.EmittedGesture))
	if emit {
		s.EmittedGesture = gesture
	}
	ev := clonePhysical(a.currentPhysical)
	a.mu.Unlock()
	for _, old := range superseded {
		a.log.Printf("PHYSICAL_SESSION_SUPERSEDED session=%s target=%s character=%s replacement=%s reason=new_move_target", old.SessionID, old.Target, old.CharacterID, target)
	}
	if emit {
		a.log.Printf("PHYSICAL_RESOLVED session=%s gesture=%s target=%s phase=moving duration_ms=%d speed=%.1f reversals=%d intensity=%.2f path=%.1f", ev.SessionID, ev.Gesture, ev.Target, ev.DurationMS, ev.Speed, ev.Reversals, ev.Intensity, s.Path)
		a.rememberTouch(ev)
		a.submitPhysical(ev)
	}
}

// dropConflictingSessionsLocked keeps Runtime NOW consistent even if the
// SHIORI/TouchProgress release edge is lost. A newly observed MOVE on another
// target is positive physical evidence that older same-character contacts are
// no longer current. This only clears local lifecycle state; it does not infer
// release from elapsed time and does not create an LLM reaction.
func (a *app) dropConflictingSessionsLocked(cid, target string) []*touchSession {
	dropped := []*touchSession{}
	for k, s := range a.sessions {
		if s == nil || s.CharacterID != cid || s.Target == target {
			continue
		}
		dropped = append(dropped, s)
		delete(a.sessions, k)
		delete(a.physicalActive, s.SessionID)
	}
	return dropped
}

const (
	// Same-target boundary re-entry may reconnect only an immediately adjacent
	// provisional lifecycle. Anything older is stale local bookkeeping and must
	// not stretch a new authoritative TouchProgress contact across old time.
	provisionalReentryGuard  = 120 * time.Millisecond
	gentleStrokeMaxSpeed     = 105.0
	roughRubMinSpeed         = 360.0
	roughRubMinReversals     = 3
	finalStrokeMinPath       = 20.0
	finalStrokeMinDurationMS = int64(180)
)

func canResumeProvisional(s *touchSession, now time.Time) bool {
	if s == nil || s.Last.IsZero() {
		return false
	}
	age := now.Sub(s.Last)
	return age >= 0 && age <= provisionalReentryGuard
}

func classify(s *touchSession) (string, float64, float64) {
	speed := 0.0
	if s.Samples > 0 {
		speed = s.SpeedSum / float64(s.Samples)
	}
	if s.Path < 8 {
		return "", 0, speed
	}
	if speed >= roughRubMinSpeed && s.Reversals >= roughRubMinReversals {
		return "rough_rub", .8, speed
	}
	if speed < gentleStrokeMaxSpeed {
		return "gentle_stroke", .35, speed
	}
	return "stroke", .5, speed
}

// shouldEmitFinalStroke prevents a cursor merely sweeping across a collision
// region from becoming a conversational touch reaction.  Physical release
// remains authoritative; this gate suppresses presentation only. Moving-phase
// strokes already require 350 ms, so this applies to release-finalized strokes.
func shouldEmitFinalStroke(s *touchSession, durationMS int64, gesture string) bool {
	return s != nil && gesture != "" && s.EmittedGesture == "" &&
		s.Path >= finalStrokeMinPath && durationMS >= finalStrokeMinDurationMS
}

func (a *app) internalHTTP() {
	mux := http.NewServeMux()
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		started := a.beginShutdown("http")
		httpjson.Write(w, http.StatusAccepted, map[string]any{"ok": true, "started": started, "service": "Runtime", "version": version})
	})
	mux.HandleFunc("/internal/touch/event", a.touchEvent)
	mux.HandleFunc("/internal/touch/progress", a.touchProgress)
	mux.HandleFunc("/internal/memory/hot-v2", a.hotUpdate)
	mux.HandleFunc("/internal/memory/hot", a.hotUpdate)
	mux.HandleFunc("/linked/session/activate", a.linkActivate)
	mux.HandleFunc("/linked/context", a.linkContext)
	mux.HandleFunc("/linked/turn/begin", a.linkTurnBegin)
	mux.HandleFunc("/linked/turn/bridge-reaction", a.linkBridgeReaction)
	mux.HandleFunc("/linked/turn/thinking", a.linkThinking)
	mux.HandleFunc("/linked/turn/response", a.linkResponse)
	mux.HandleFunc("/linked/turn/commit", a.linkCommit)
	mux.HandleFunc("/linked/turn/abort", a.linkAbort)
	mux.HandleFunc("/linked/session/deactivate", a.linkDeactivate)
	mux.HandleFunc("/linked/status", a.linkStatus)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		httpjson.Write(w, 200, map[string]any{"ok": true, "service": "Runtime-internal", "version": version})
	})
	mux.HandleFunc("/state/dressup-info", a.dressupInfo)
	mux.HandleFunc("/state/dressup-changed", a.dressupChanged)
	mux.HandleFunc("/state/shell-info", a.shellInfo)
	mux.HandleFunc("/state/shell-changed", a.shellChanged)
	if err := http.ListenAndServe("127.0.0.1:8770", mux); err != nil {
		a.log.Printf("internal HTTP: %v", err)
	}
}
func (a *app) hotUpdate(w http.ResponseWriter, r *http.Request) {
	var x model.HotMemorySnapshot
	if err := httpjson.Decode(r, &x); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.mu.Lock()
	if x.Version >= a.hot.Version {
		a.hot = x
	}
	a.mu.Unlock()
	a.log.Printf("HOT_MEMORY_UPDATE version=%d items=%d", x.Version, len(x.Items))
	httpjson.Write(w, 200, map[string]any{"ok": true})
}
func (a *app) pullHot() {
	ctx, c := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer c()
	var x model.HotMemorySnapshot
	if err := httpGetJSON(ctx, "http://127.0.0.1:8768/v2/hot", &x); err == nil {
		a.mu.Lock()
		a.hot = x
		a.mu.Unlock()
		a.log.Printf("HOT_MEMORY_PULL version=%d items=%d", x.Version, len(x.Items))
	}
}

func (a *app) dressupInfo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	x := a.appearance()
	records := dressupRecordsFromForm(r)
	x.Raw = strings.Join(records, "\n")
	x.Dressup = parseDressupRecords(records)
	x.SnapshotComplete = true
	x.Source = "OnNotifyDressupInfo"
	x.UpdatedAt = model.Now()
	x.Summary = appearanceSummary(x)
	a.persistAppearance(x)
	a.log.Printf("APPEARANCE_SNAPSHOT shell=%q items=%d complete=true summary=%q", x.ShellName, len(x.Dressup), x.Summary)
	httpjson.Write(w, 200, map[string]any{"ok": true})
}
func (a *app) dressupChanged(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	x := a.appearance()
	if x.Dressup == nil {
		x.Dressup = map[string]any{}
	}
	characterID := r.Form.Get("character_id")
	part := r.Form.Get("part")
	category := r.Form.Get("category")
	k := dressupKey(characterID, category, part)
	if k != "" {
		x.Dressup[k] = map[string]any{"character_id": characterID, "part": part, "enabled": enabledValue(r.Form.Get("enabled")), "category": category, "source": r.Form.Get("source"), "changed_at": model.Now()}
	}
	x.Source = "OnDressupChanged"
	x.UpdatedAt = model.Now()
	x.Summary = appearanceSummary(x)
	a.persistAppearance(x)
	a.log.Printf("APPEARANCE_DELTA shell=%q category=%q part=%q enabled=%v summary=%q", x.ShellName, category, part, enabledValue(r.Form.Get("enabled")), x.Summary)
	httpjson.Write(w, 200, map[string]any{"ok": true})
}

func (a *app) warmBridgeProfile(x model.AppearanceState) {
	if strings.TrimSpace(x.ShellKey) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		defer cancel()
		var out struct {
			ShellKey string `json:"shell_key"`
		}
		err := httpjson.Post(ctx, "http://127.0.0.1:8767/v1/profile/warm", map[string]string{"shell_key": x.ShellKey, "shell_name": x.ShellName, "shell_path": x.ShellPath}, &out)
		if err != nil {
			a.log.Printf("PROFILE_WARM_DEFER shell=%q path=%q error=%v", x.ShellName, x.ShellPath, err)
			return
		}
		a.log.Printf("PROFILE_WARM shell=%q shell_key=%s", x.ShellName, out.ShellKey)
	}()
}

func (a *app) shellInfo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	previous := a.appearance()
	x := previous
	name, path := r.Form.Get("name"), r.Form.Get("path")
	metadataChanged := (name != "" && name != x.ShellName) || (path != "" && path != x.ShellPath)
	if name != "" {
		x.ShellName = name
	}
	if path != "" {
		x.ShellPath = path
	}
	x.ShellKey = shellid.Key(x.ShellPath, x.ShellName)
	identityChanged := previous.ShellKey != x.ShellKey
	if identityChanged {
		x.Dressup = map[string]any{}
		x.Raw = ""
		x.SnapshotComplete = false
	}
	x.Source = "OnNotifyShellInfo"
	x.UpdatedAt = model.Now()
	x.Summary = appearanceSummary(x)
	a.persistAppearance(x)
	a.warmBridgeProfile(x)
	a.log.Printf("APPEARANCE_SHELL_INFO shell=%q shell_key=%s path=%q metadata_changed=%t identity_changed=%t complete=%t", x.ShellName, x.ShellKey, x.ShellPath, metadataChanged, identityChanged, x.SnapshotComplete)
	httpjson.Write(w, 200, map[string]any{"ok": true})
}

func (a *app) shellChanged(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	previous := a.appearance()
	x := previous
	x.ShellName = r.Form.Get("name")
	x.ShellPath = r.Form.Get("path")
	x.ShellKey = shellid.Key(x.ShellPath, x.ShellName)
	// Only a real Shell identity change invalidates dress-up facts. A display-
	// name change or path spelling change that preserves the same directory key
	// is metadata, not a new body.
	if previous.ShellKey != x.ShellKey {
		x.Dressup = map[string]any{}
		x.Raw = ""
		x.SnapshotComplete = false
	}
	x.Source = "OnShellChanged"
	x.UpdatedAt = model.Now()
	x.Summary = appearanceSummary(x)
	a.persistAppearance(x)
	a.warmBridgeProfile(x)
	a.log.Printf("APPEARANCE_SHELL_CHANGED shell=%q shell_key=%s path=%q complete=false", x.ShellName, x.ShellKey, x.ShellPath)
	if shouldReactToShellChange(previous, x) {
		a.submitAppearanceChange(previous, x)
	}
	httpjson.Write(w, 200, map[string]any{"ok": true})
}

func shouldReactToShellChange(previous, current model.AppearanceState) bool {
	return previous.ShellKey != "" && current.ShellKey != "" && previous.ShellKey != current.ShellKey
}

func (a *app) supersedeActiveAppearance() {
	a.mu.Lock()
	id := a.activeAppearance
	if id == "" {
		a.mu.Unlock()
		return
	}
	lc := a.life[id]
	if lc == nil || (lc.State != "processing" && lc.State != "processing_checkable") {
		a.activeAppearance = ""
		a.mu.Unlock()
		return
	}
	lc.State = "superseded"
	cancel := lc.cancel
	a.activeAppearance = ""
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.log.Printf("APPEARANCE_COGNITION_SUPERSEDED request=%s reason=new_shell_change", id)
	go func(requestID string) {
		ctx, cc := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer cc()
		_ = httpjson.Post(ctx, "http://127.0.0.1:8767/v1/cancel", map[string]string{"request_id": requestID}, nil)
	}(id)
}

func (a *app) submitAppearanceChange(previous, current model.AppearanceState) {
	if a.linkedBusy() {
		a.log.Printf("APPEARANCE_COGNITION_SUPPRESSED previous_shell=%q shell=%q reason=linked_turn_active", previous.ShellName, current.ShellName)
		return
	}
	a.supersedeActiveAppearance()
	transition := &model.AppearanceTransition{
		Kind:              "shell_changed",
		PreviousShellName: previous.ShellName,
		PreviousShellKey:  previous.ShellKey,
		CurrentShellName:  current.ShellName,
		CurrentShellKey:   current.ShellKey,
		ChangedAt:         model.Now(),
	}
	id := a.newID("appearance")
	env := a.buildEnvelope(id, model.RequestAppearance, "appearance", model.UserInput{}, nil, 0)
	env.AppearanceChange = transition
	changeSituation := fmt.Sprintf("Authoritative appearance transition: the current Shell changed from %q to %q. The new Shell is already active.", previous.ShellName, current.ShellName)
	if strings.TrimSpace(env.CurrentState.Situation) == "" {
		env.CurrentState.Situation = changeSituation
	} else {
		env.CurrentState.Situation += ". " + changeSituation
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.life[id] = &lifecycle{RequestID: id, State: "processing", Source: "appearance", Class: model.RequestAppearance, Created: time.Now(), cancel: cancel}
	a.activeAppearance = id
	before := cloneAffect(a.affect)
	a.mu.Unlock()
	go a.runJob(requestJob{env: env, ctx: ctx, cancel: cancel, affectBefore: before})
	a.log.Printf("REQUEST_CREATED id=%s class=%s route=fast previous_shell=%q previous_shell_key=%s shell=%q shell_key=%s", id, model.RequestAppearance, previous.ShellName, previous.ShellKey, current.ShellName, current.ShellKey)
}

func dressupRecordsFromForm(r *http.Request) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, v := range r.Form["raw"] {
		add(v)
	}
	for i := 0; i < 32; i++ {
		for _, v := range r.Form[fmt.Sprintf("raw%d", i)] {
			add(v)
		}
	}
	return out
}

func parseDressupRecords(records []string) map[string]any {
	out := map[string]any{}
	for _, raw := range records {
		f := strings.Split(raw, "\x01")
		if len(f) < 5 {
			continue
		}
		characterID := strings.TrimSpace(f[0])
		category := strings.TrimSpace(f[1])
		part := strings.TrimSpace(f[2])
		options := strings.TrimSpace(f[3])
		enabled := enabledValue(strings.TrimSpace(f[4]))
		thumb := ""
		if len(f) > 5 {
			thumb = strings.TrimSpace(f[5])
		}
		k := dressupKey(characterID, category, part)
		if k == "" {
			continue
		}
		out[k] = map[string]any{"character_id": characterID, "category": category, "part": part, "options": options, "enabled": enabled, "thumbnail": thumb}
	}
	return out
}

func dressupKey(characterID, category, part string) string {
	category = strings.TrimSpace(category)
	part = strings.TrimSpace(part)
	if category == "" && part == "" {
		return ""
	}
	return strings.TrimSpace(characterID) + "|" + category + "|" + part
}

func enabledValue(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "on" || v == "yes"
}

func appearanceSummary(x model.AppearanceState) string {
	parts := make([]string, 0, len(x.Dressup))
	for _, raw := range x.Dressup {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		category, _ := m["category"].(string)
		part, _ := m["part"].(string)
		enabled := false
		switch v := m["enabled"].(type) {
		case bool:
			enabled = v
		case string:
			enabled = enabledValue(v)
		}
		if category == "" && part == "" {
			continue
		}
		state := "OFF"
		if enabled {
			state = "ON"
		}
		label := strings.TrimSpace(category + "/" + part)
		parts = append(parts, label+"="+state)
	}
	// Stable ordering makes prompt diffs and audits deterministic.
	sort.Strings(parts)
	shell := x.ShellName
	if shell == "" {
		shell = "unknown"
	}
	if !x.SnapshotComplete {
		return fmt.Sprintf("Current shell=%s. Dress-up snapshot is pending; do not reuse dress-up facts from the previous shell.", shell)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("Current shell=%s. Dress-up snapshot is complete; no dress-up definitions are active/known in the snapshot.", shell)
	}
	return fmt.Sprintf("Current shell=%s. Dress-up snapshot is complete: %s.", shell, strings.Join(parts, "; "))
}
func (a *app) persistAppearance(x model.AppearanceState) {
	_ = os.MkdirAll(profilepath.State(a.root), 0755)
	b, _ := json.MarshalIndent(x, "", "  ")
	_ = os.WriteFile(profilepath.Appearance(a.root), b, 0644)
}

func (a *app) touchEvent(w http.ResponseWriter, r *http.Request) {
	var ev model.PhysicalEvent
	if err := httpjson.Decode(r, &ev); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if !strings.HasPrefix(ev.Target, "Owl.") && ev.CharacterID == "1" {
		ev.Target = "Owl." + ev.Target
	}
	k := sessionKey(ev.CharacterID, ev.Target)
	a.mu.Lock()
	if rs := a.sessions[k]; rs != nil {
		ev.SessionID = rs.SessionID
	}
	a.mu.Unlock()
	a.noteUserActivity()
	if ev.Gesture == "release" {
		a.handleRelease(&ev)
	} else {
		a.mu.Lock()
		a.currentPhysical = clonePhysical(&ev)
		a.mu.Unlock()
		if ev.Gesture == "resting_touch" {
			a.submitPhysical(&ev)
		} else {
			a.submitPhysical(&ev)
		}
	}
	httpjson.Write(w, 200, map[string]any{"ok": true})
}

func (a *app) touchProgress(w http.ResponseWriter, r *http.Request) {
	var ev model.PhysicalEvent
	if err := httpjson.Decode(r, &ev); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	ev.Target = canonicalTarget(ev.CharacterID, ev.Target)
	k := sessionKey(ev.CharacterID, ev.Target)
	a.mu.Lock()
	s := a.sessions[k]
	if s == nil {
		for _, x := range a.sessions {
			if x.Target == ev.Target && x.CharacterID == ev.CharacterID {
				s = x
				break
			}
		}
	}
	emit := false
	if s != nil {
		g, inten, sp := classify(s)
		if g != "" {
			ev.Gesture = g
			ev.Intensity = inten
			ev.Speed = sp
			ev.Reversals = s.Reversals
			ev.SessionID = s.SessionID
			s.LastProgressMS = ev.DurationMS
			if s.EmittedGesture == "" || a.impulse(g) > a.impulse(s.EmittedGesture) {
				emit = true
				s.EmittedGesture = g
			}
		}
	}
	a.currentPhysical = clonePhysical(&ev)
	a.mu.Unlock()
	if emit {
		a.rememberTouch(&ev)
		a.submitPhysical(&ev)
	}
	httpjson.Write(w, 200, map[string]any{"ok": true})
}

func (a *app) handleRelease(ev *model.PhysicalEvent) {
	ev.Target = canonicalTarget(ev.CharacterID, ev.Target)
	k := sessionKey(ev.CharacterID, ev.Target)
	a.mu.Lock()
	s := a.sessions[k]
	if s == nil {
		for kk, x := range a.sessions {
			if x.Target == ev.Target && x.CharacterID == ev.CharacterID {
				s = x
				k = kk
				break
			}
		}
	}
	var final *model.PhysicalEvent
	if s != nil {
		g, inten, sp := classify(s)
		if shouldEmitFinalStroke(s, ev.DurationMS, g) {
			f := *ev
			f.Gesture = g
			f.Phase = "final"
			f.Contact = false
			f.Released = true
			f.Intensity = inten
			f.Speed = sp
			f.Reversals = s.Reversals
			f.SessionID = s.SessionID
			final = &f
		}
		delete(a.sessions, k)
		delete(a.physicalActive, s.SessionID)
	}
	a.currentPhysical = clonePhysical(ev)
	a.mu.Unlock()
	a.log.Printf("PHYSICAL_RELEASE session=%s target=%s duration_ms=%d authoritative=true", ev.SessionID, ev.Target, ev.DurationMS)
	a.observePhysical(ev, .18)
	if final != nil {
		a.rememberTouch(final)
		a.submitPhysical(final)
	}
}

func (a *app) submitPhysical(ev *model.PhysicalEvent) {
	if ev == nil || ev.Gesture == "" {
		return
	}
	a.rememberPhysicalOccurrence(ev)
	// Body sensing and authoritative release continue during linked cognition,
	// but the local Bridge must not start a competing Primary-Brain reaction.
	if a.linkedBusy() {
		a.observePhysical(ev, .45)
		a.log.Printf("PHYSICAL_COGNITION_DEFER target=%s gesture=%s reason=linked_turn_active", ev.Target, ev.Gesture)
		return
	}
	allowed, escalation, previous := a.presentationDecision(ev)
	if !allowed {
		a.log.Printf("PHYSICAL_PRESENT_SUPPRESSED session=%s target=%s gesture=%s impulse=%.1f previous=%.1f reason=same_or_lower_impulse", ev.SessionID, ev.Target, ev.Gesture, a.impulse(ev.Gesture), previous)
		a.observePhysical(ev, .3)
		return
	}
	id := a.newID("phys")
	env := a.buildEnvelope(id, model.RequestPhysical, "physical", model.UserInput{}, ev, 0)
	ctx, cancel := context.WithCancel(context.Background())
	imp := a.impulse(ev.Gesture)
	var supersede string
	a.mu.Lock()
	if escalation && ev.SessionID != "" {
		if old := a.physicalActive[ev.SessionID]; old != "" {
			if lc := a.life[old]; lc != nil && lc.State != "completed" {
				supersede = old
			}
		}
	}
	a.life[id] = &lifecycle{RequestID: id, State: "processing", Source: "physical", Class: model.RequestPhysical, Created: time.Now(), cancel: cancel, SessionID: ev.SessionID, Impulse: imp}
	if ev.SessionID != "" {
		a.physicalActive[ev.SessionID] = id
	}
	before := cloneAffect(a.affect)
	a.mu.Unlock()
	if supersede != "" {
		a.log.Printf("PHYSICAL_PRESENT_ESCALATION session=%s target=%s gesture=%s impulse=%.1f supersede=%s", ev.SessionID, ev.Target, ev.Gesture, imp, supersede)
		a.cancelRequest(supersede, "superseded")
	}
	go a.runJob(requestJob{env: env, ctx: ctx, cancel: cancel, affectBefore: before, physical: clonePhysical(ev)})
	a.log.Printf("REQUEST_CREATED id=%s class=physical route=fast gesture=%s target=%s phase=%s", id, ev.Gesture, ev.Target, ev.Phase)
}

func (a *app) presentationDecision(ev *model.PhysicalEvent) (bool, bool, float64) {
	if ev.Gesture == "release" {
		return false, false, 0
	}
	rules := a.physicalRules()
	imp := a.impulse(ev.Gesture)
	key := ev.SessionID
	if key == "" {
		key = ev.Target + "|instant|" + ev.ObservedAt
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	last, ok := a.lastPresentation[key]
	if !ok {
		a.lastPresentation[key] = presentationMark{Impulse: imp, Gesture: ev.Gesture, At: time.Now()}
		return true, false, 0
	}
	if imp > last.Impulse {
		a.lastPresentation[key] = presentationMark{Impulse: imp, Gesture: ev.Gesture, At: time.Now()}
		return true, true, last.Impulse
	}
	if ev.Gesture == "resting_touch" && ev.DurationMS >= rules.RestingReactionAfterMS && time.Since(last.At) >= time.Duration(rules.LowerImpulseGuardMS)*time.Millisecond {
		a.lastPresentation[key] = presentationMark{Impulse: imp, Gesture: ev.Gesture, At: time.Now()}
		return true, false, last.Impulse
	}
	return false, false, last.Impulse
}

func (a *app) physicalRules() physicalResponseRules {
	c := physicalResponseRules{LowerImpulseGuardMS: 5000, RestingReactionAfterMS: 5000, SupersedeInflightOnEscalation: true}
	b, e := os.ReadFile(filepath.Join(a.root, "config", "physical_response_rules.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}
func (a *app) impulse(g string) float64 {
	var c model.TouchMemoryRules
	b, e := os.ReadFile(filepath.Join(a.root, "config", "touch_memory_rules.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Impulses != nil && c.Impulses[g] > 0 {
		return c.Impulses[g]
	}
	switch g {
	case "grab":
		return 500
	case "rough_rub":
		return 450
	case "heavy_tap":
		return 340
	case "stroke":
		return 300
	case "gentle_stroke":
		return 260
	case "light_touch":
		return 220
	case "resting_touch":
		return 120
	}
	return 0
}

func (a *app) buildEnvelope(id, class, source string, input model.UserInput, ev *model.PhysicalEvent, check int64) model.RequestEnvelope {
	touch := a.touchSnapshot()
	a.mu.Lock()
	aff, decayElapsedMS, decayFactor := a.decayAffectLocked()
	if ev != nil {
		a.currentPhysical = clonePhysical(ev)
	}
	physical := currentPhysicalForEnvelope(a.currentPhysical, time.Now())
	// TouchProgress is the body-sensing authority. Before any contact=true fact
	// enters a Bridge prompt, reconcile it against the same authoritative
	// snapshot already fetched for this envelope. This is a bounded local check
	// and therefore does not alter the Runtime -> Bridge fast-path topology.
	if physical != nil && physical.Contact {
		if touch == nil {
			a.log.Printf("PHYSICAL_CONTEXT_SUPPRESSED target=%s character=%s reason=touch_snapshot_unavailable", physical.Target, physical.CharacterID)
			physical = nil
		} else if !touchSnapshotHasActive(touch, physical.CharacterID, physical.Target) {
			a.log.Printf("PHYSICAL_STALE_CLEARED target=%s character=%s session=%s reason=touch_snapshot_no_active", physical.Target, physical.CharacterID, physical.SessionID)
			clearPhysicalContactLocked(a, physical.CharacterID, physical.Target)
			physical = nil
		}
	}
	hot := a.hot
	recent := a.recentDialogueLocked(time.Now())
	recentPhysical := a.recentPhysicalLocked(time.Now())
	a.mu.Unlock()
	a.auditAffectf("AFFECT_CONTEXT request=%s class=%s source=%s revision=%d decay_elapsed_ms=%d decay_factor=%.6f primary=%s intensity=%.4f current=%s", id, class, source, aff.Revision, decayElapsedMS, decayFactor, aff.Primary, aff.Intensity, formatAffect(aff))
	appearance := a.appearance()
	situation := buildSituation(input, physical, aff)
	env := model.RequestEnvelope{RequestID: id, RequestClass: class, Source: source, CreatedAt: model.Now(), UserInput: input, CurrentState: model.CurrentState{Physical: physical, Touch: touch, Affect: aff, Appearance: appearance, Situation: situation}, RequestPolicy: model.RequestPolicy{CheckAfterMS: check, Cancellable: true, Presentation: true, Priority: priorityFor(class)}, HotMemory: hot, RecentDialogue: recent, RecentPhysical: recentPhysical}
	if cap, _, err := embodimentForShell(appearance.ShellPath); err == nil {
		env.Embodiment = cap
	} else if !errors.Is(err, os.ErrNotExist) && strings.TrimSpace(appearance.ShellPath) != "" {
		a.log.Printf("SHELL_SEMANTICS_INVALID shell=%q error=%v", appearance.ShellName, err)
	}
	recallAttempted := false
	recallMS := int64(0)
	if class == model.RequestChat && needsRecall(userQueryText(input)) {
		recallAttempted = true
		started := time.Now()
		a.mu.Lock()
		depth := a.recallDepth
		a.mu.Unlock()
		ctx, c := context.WithTimeout(context.Background(), a.recallTimeout(depth))
		var mc model.MemoryCapsule
		err := httpjson.Post(ctx, "http://127.0.0.1:8768/v2/recall", map[string]any{"query": userQueryText(input), "depth": depth, "envelope": env}, &mc)
		c()
		recallMS = time.Since(started).Milliseconds()
		if err == nil {
			env.MemoryCapsule = mc
			a.log.Printf("ROUTE_RECALL request=%s result=ok memories=%d degraded=%t", id, capsuleCount(mc), mc.Degraded)
		} else {
			a.log.Printf("ROUTE_RECALL request=%s result=fallback error=%v", id, err)
		}
	}
	a.auditCognitionf("REQUEST_CONTEXT request=%s class=%s route=%s recall_attempted=%t recall_ms=%d hot_version=%d hot_items=%d recalled=%d affect_revision=%d affect_primary=%s affect_intensity=%.4f physical=%s", id, class, routeName(env), recallAttempted, recallMS, hot.Version, len(hot.Items), capsuleCount(env.MemoryCapsule), aff.Revision, aff.Primary, aff.Intensity, physicalAudit(physical))
	return env
}

func routeName(env model.RequestEnvelope) string {
	if env.RequestClass == model.RequestChat && needsRecall(userQueryText(env.UserInput)) {
		return "recall"
	}
	if capsuleCount(env.MemoryCapsule) > 0 {
		return "recall"
	}
	return "fast"
}

func priorityFor(class string) int {
	switch class {
	case model.RequestChat, model.RequestPhysical, model.RequestLinkedChat:
		return 100
	case model.RequestAppearance:
		return 95
	case model.RequestDeferred:
		return 80
	case model.RequestAutonomous:
		return 50
	default:
		return 60
	}
}
func (a *app) matchDirective(text string) *model.DirectiveRef {
	r, err := directive.Load(filepath.Join(a.root, "config", "directive_rules.json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			a.log.Printf("DIRECTIVE_REGISTRY result=degraded error=%v", err)
		}
		return nil
	}
	m, ok := r.MatchText(text)
	if !ok {
		return nil
	}
	kind := strings.ToLower(strings.TrimSpace(m.Rule.Kind))
	a.log.Printf("DIRECTIVE_MATCH id=%s kind=%s invoked=%q argument_chars=%d", m.ID, kind, m.InvokedAs, len([]rune(m.Argument)))
	return &model.DirectiveRef{ID: m.ID, Kind: kind, InvokedAs: m.InvokedAs, Argument: m.Argument}
}

func userQueryText(input model.UserInput) string {
	if input.Directive != nil && strings.TrimSpace(input.Directive.Argument) != "" {
		return strings.TrimSpace(input.Directive.Argument)
	}
	return strings.TrimSpace(input.Text)
}

func needsRecall(text string) bool {
	q := strings.ToLower(strings.TrimSpace(text))
	if q == "" {
		return false
	}
	for _, x := range []string{
		"之前", "上次", "上一次", "前一次", "最近一次", "最後一次", "昨天", "前天", "曾經", "還記得", "你記得", "我說過", "你說過", "以前",
		"earlier", "previously", "remember", "last time", "yesterday", "previous time", "most recent time",
	} {
		if strings.Contains(q, x) {
			return true
		}
	}
	// Historical superlatives are contextual: "最後請關門" stays fast, while
	// "我最後給你的東西是什麼？" is a backward-looking query.
	historicalSuperlative := strings.Contains(q, "最後") || strings.Contains(q, "most recent") || strings.Contains(q, "last ")
	questionCue := false
	for _, x := range []string{"什麼", "哪", "何時", "什麼時候", "多久", "嗎", "?", "？", "what", "which", "when"} {
		if strings.Contains(q, x) {
			questionCue = true
			break
		}
	}
	return historicalSuperlative && questionCue
}

func normalizeRecallDepth(depth string) string {
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "light", "medium", "deep", "unbounded":
		return strings.ToLower(strings.TrimSpace(depth))
	default:
		return "medium"
	}
}

type runtimeRetrievalPreset struct {
	RecallTimeoutMS int64 `json:"recall_timeout_ms"`
}
type runtimeRetrievalConfig struct {
	Presets map[string]runtimeRetrievalPreset `json:"presets"`
}

func (a *app) recallTimeout(depth string) time.Duration {
	defaults := map[string]int64{"light": 700, "medium": 1200, "deep": 2000, "unbounded": 700}
	depth = normalizeRecallDepth(depth)
	ms := defaults[depth]
	var c runtimeRetrievalConfig
	b, e := os.ReadFile(filepath.Join(a.root, "config", "memory_retrieval_rules.json"))
	if e == nil && json.Unmarshal(b, &c) == nil {
		if p, ok := c.Presets[depth]; ok && p.RecallTimeoutMS > 0 {
			ms = p.RecallTimeoutMS
		}
	}
	// Runtime is the outer lifecycle authority; leave a small transport margin
	// beyond MemoryService's shared internal recall deadline.
	return time.Duration(ms+250) * time.Millisecond
}

func touchSnapshotHasActive(snapshot map[string]any, cid, target string) bool {
	if snapshot == nil {
		return false
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return false
	}
	var x struct {
		Active []model.PhysicalEvent `json:"active"`
	}
	if json.Unmarshal(b, &x) != nil {
		return false
	}
	target = canonicalTarget(cid, target)
	for _, ev := range x.Active {
		if ev.Contact && ev.CharacterID == cid && canonicalTarget(ev.CharacterID, ev.Target) == target {
			return true
		}
	}
	return false
}

func clearPhysicalContactLocked(a *app, cid, target string) {
	target = canonicalTarget(cid, target)
	for k, sess := range a.sessions {
		if sess != nil && sess.CharacterID == cid && sess.Target == target {
			delete(a.sessions, k)
			delete(a.physicalActive, sess.SessionID)
		}
	}
	if a.currentPhysical != nil && a.currentPhysical.CharacterID == cid && canonicalTarget(cid, a.currentPhysical.Target) == target {
		a.currentPhysical = nil
	}
}

func currentPhysicalForEnvelope(p *model.PhysicalEvent, now time.Time) *model.PhysicalEvent {
	physical := clonePhysical(p)
	if physical == nil || physical.Contact || physical.ObservedAt == "" {
		return physical
	}
	if t, err := time.Parse(time.RFC3339Nano, physical.ObservedAt); err == nil && now.Sub(t) > 2*time.Second {
		return nil
	}
	return physical
}
func buildSituation(input model.UserInput, p *model.PhysicalEvent, a model.AffectState) string {
	parts := []string{}
	if p != nil {
		if p.Gesture != "" {
			parts = append(parts, fmt.Sprintf("Current physical fact: gesture=%s target=%s phase=%s contact=%t released=%t", p.Gesture, p.Target, p.Phase, p.Contact, p.Released))
		} else {
			parts = append(parts, fmt.Sprintf("Current physical state: target=%s contact=%t", p.Target, p.Contact))
		}
	}
	if a.Primary != "neutral" && a.Intensity > .05 {
		parts = append(parts, fmt.Sprintf("Current character affect: %s (persistent local state)", a.Primary))
	}
	if input.UserEmotion != "" && input.UserEmotion != "neutral" {
		parts = append(parts, "User reports current emotion="+input.UserEmotion)
	}
	return strings.Join(parts, ". ")
}

func appearanceResultMatchesCurrentShell(env model.RequestEnvelope, current model.AppearanceState) bool {
	if env.RequestClass != model.RequestAppearance || env.AppearanceChange == nil {
		return true
	}
	target := strings.TrimSpace(env.AppearanceChange.CurrentShellKey)
	return target != "" && strings.TrimSpace(current.ShellKey) == target
}

func (a *app) runJob(job requestJob) {
	id := job.env.RequestID
	started := time.Now()
	var rr model.Reaction
	err := httpjson.Post(job.ctx, "http://127.0.0.1:8767/v1/respond", job.env, &rr)
	latencyMS := time.Since(started).Milliseconds()
	if job.env.RequestClass == model.RequestAppearance && job.env.AppearanceChange != nil {
		current := a.appearance()
		reason := ""
		if !appearanceResultMatchesCurrentShell(job.env, current) {
			reason = "stale_shell"
		} else if a.linkedBusy() {
			reason = "linked_primary_active"
		}
		if reason != "" {
			a.mu.Lock()
			if lc := a.life[id]; lc != nil {
				lc.State = "superseded"
			}
			if a.activeAppearance == id {
				a.activeAppearance = ""
			}
			a.mu.Unlock()
			a.log.Printf("APPEARANCE_COGNITION_DISCARDED request=%s target_shell_key=%s current_shell_key=%s reason=%s", id, job.env.AppearanceChange.CurrentShellKey, current.ShellKey, reason)
			a.auditCognitionf("REQUEST_RESULT request=%s class=%s state=superseded latency_ms=%d discarded=true reason=%s", id, job.env.RequestClass, latencyMS, reason)
			return
		}
	}
	a.mu.Lock()
	lc := a.life[id]
	state := ""
	if lc != nil {
		state = lc.State
	}
	discard := state == "superseded" || state == "cancel_requested" || state == "cancelled"
	a.mu.Unlock()
	if discard {
		a.mu.Lock()
		if a.activeAppearance == id {
			a.activeAppearance = ""
		}
		a.mu.Unlock()
		a.log.Printf("REQUEST_END id=%s state=%s result_discarded=true", id, state)
		a.auditCognitionf("REQUEST_RESULT request=%s class=%s state=%s latency_ms=%d discarded=true", id, job.env.RequestClass, state, latencyMS)
		return
	}
	if err != nil {
		a.mu.Lock()
		lc = a.life[id]
		cancelled := job.ctx.Err() != nil
		if lc != nil {
			if cancelled {
				lc.State = "cancelled"
			} else {
				lc.State = "error"
			}
		}
		if job.env.Source == "chat" && a.activeChat == id {
			a.activeChat = ""
		}
		if a.activeAppearance == id {
			a.activeAppearance = ""
		}
		a.mu.Unlock()
		if job.env.Source == "chat" {
			if cancelled {
				a.ui.SetIdle()
			} else {
				a.ui.SetError(err.Error())
			}
			if !cancelled {
				a.sendPresentation("\\0\\s[3]請求失敗：" + sstp.EscapeSakuraText(err.Error()) + "\\e")
			}
		}
		endState := func() string {
			if cancelled {
				return "cancelled"
			}
			return "error"
		}()
		a.log.Printf("REQUEST_END id=%s state=%s error=%v", id, endState, err)
		a.auditCognitionf("REQUEST_RESULT request=%s class=%s state=%s latency_ms=%d presentation=false error=true", id, job.env.RequestClass, endState, latencyMS)
		return
	}
	a.mu.Lock()
	causalBefore, after := a.updateAffectLocked(id, rr.ReactionEmotion, job.env.Source, cause(job.env))
	lc = a.life[id]
	if lc != nil {
		lc.State = "completed"
	}
	if job.env.Source == "chat" && a.activeChat == id {
		a.activeChat = ""
	}
	if a.activeAppearance == id {
		a.activeAppearance = ""
	}
	if job.physical != nil && job.physical.SessionID != "" && a.physicalActive[job.physical.SessionID] == id {
		delete(a.physicalActive, job.physical.SessionID)
	}
	a.mu.Unlock()
	if job.env.Source == "chat" {
		a.ui.SetIdle()
	}
	continuationTimer := ""
	if rr.Action == "defer" && rr.Continuation != nil {
		continuationTimer = a.prepareContinuationTimer(job.env, rr)
	}
	shouldPresent := rr.Action != "silent" && job.env.RequestPolicy.Presentation && strings.TrimSpace(rr.Dialogue) != ""
	if shouldPresent {
		// For action=defer the holding line and continuation timer must be one
		// atomic SakuraScript. A second websocket message can replace/interrupt
		// the just-opened SHIORI balloon before the user sees it.
		a.present(rr, continuationTimer)
	} else if continuationTimer != "" {
		// A defer with no visible holding line still needs a SHIORI-owned timer.
		a.sendPresentation(continuationTimer + "\\e")
	}
	if strings.TrimSpace(job.env.UserInput.Text) != "" || strings.TrimSpace(rr.Dialogue) != "" {
		a.rememberDialogue(model.DialogueTurn{Timestamp: model.Now(), Source: job.env.Source, User: job.env.UserInput.Text, Character: rr.Dialogue})
	}
	a.auditCognitionf("REQUEST_RESULT request=%s class=%s state=completed latency_ms=%d action=%s emotion=%s dialogue_chars=%d presentation=%t continuation=%t", id, job.env.RequestClass, latencyMS, rr.Action, rr.ReactionEmotion, len([]rune(strings.TrimSpace(rr.Dialogue))), shouldPresent, continuationTimer != "")
	if job.env.RequestClass != model.RequestAutonomous || rr.Action != "silent" {
		ep := model.EpisodeCommitV2{EpisodeID: id, RequestID: id, RequestClass: job.env.RequestClass, CompletedAt: model.Now(), Source: job.env.Source, UserInput: job.env.UserInput, Event: job.physical, AppearanceChange: job.env.AppearanceChange, Situation: job.env.CurrentState.Situation, Reaction: rr, AffectAtRequest: job.affectBefore, AffectBefore: causalBefore, AffectAfter: after, AffectDelta: computeAffectDelta(causalBefore, after), Status: "completed"}
		go a.commitEpisode(ep)
	}
	a.log.Printf("REQUEST_END id=%s class=%s state=completed action=%s emotion=%s", id, job.env.RequestClass, rr.Action, rr.ReactionEmotion)
	if job.env.RequestClass == model.RequestAutonomous {
		a.armAutonomousTimer()
	}
}

func cause(env model.RequestEnvelope) string {
	if env.AppearanceChange != nil {
		return fmt.Sprintf("appearance_change=%s->%s", env.AppearanceChange.PreviousShellName, env.AppearanceChange.CurrentShellName)
	}
	if env.CurrentState.Physical != nil {
		return fmt.Sprintf("gesture=%s target=%s", env.CurrentState.Physical.Gesture, env.CurrentState.Physical.Target)
	}
	return strings.TrimSpace(env.UserInput.Text)
}
func (a *app) commitEpisode(ep model.EpisodeCommitV2) {
	ctx, c := context.WithTimeout(context.Background(), 1800*time.Millisecond)
	defer c()
	if e := httpjson.Post(ctx, "http://127.0.0.1:8768/v2/episode", ep, nil); e != nil {
		a.log.Printf("EPISODE_COMMIT id=%s error=%v", ep.RequestID, e)
	} else {
		a.log.Printf("EPISODE_COMMIT id=%s state=queued", ep.RequestID)
	}
}

func (a *app) present(rr model.Reaction, extraTimerScript string) {
	surface := a.surfaceFor(rr)
	a.mu.Lock()
	ms := a.balloonMS
	a.mu.Unlock()
	if ms < 0 {
		ms = 15000
	}
	a.sendPresentation("\\![raise,OnCharacterGPTReactionBegin," + strconv.Itoa(ms) + "]")
	script := buildPresentationScript(rr, surface, ms, extraTimerScript)
	a.sendPresentation(script)
	a.log.Printf("PRESENT_FIXED surface=%d balloon_ms=%d chars=%d", surface, ms, len([]rune(rr.Dialogue)))
}
func buildPresentationScript(rr model.Reaction, surface, ms int, extraTimerScript string) string {
	script := "\\0\\s[" + strconv.Itoa(surface) + "]\\![set,balloontimeout,0]" + sstp.EscapeSakuraText(rr.Dialogue)
	// Deferred timer is deliberately embedded after the holding line so it is
	// scheduled without creating a second OnCharacterGPT websocket script.
	if extraTimerScript != "" {
		script += extraTimerScript
	}
	if ms > 0 {
		script += "\\![timerraise," + strconv.Itoa(ms) + ",1,OnCharacterGPTBalloonTimeout]"
	}
	return script + "\\e"
}

func (a *app) sendPresentation(script string) {
	a.mu.Lock()
	c := a.wsConn
	a.mu.Unlock()
	if c != nil {
		if err := c.WriteText(script); err == nil {
			a.log.Printf("PRESENTATION_TX transport=websocket chars=%d", len([]rune(script)))
			return
		}
	}
	_ = sstp.SendScript("SSPGPT Runtime", script)
	a.log.Printf("PRESENTATION_TX transport=sstp_fallback chars=%d", len([]rune(script)))
}
func (a *app) surfaceFor(rr model.Reaction) int {
	appearance := a.appearance()
	if _, sem, err := embodimentForShell(appearance.ShellPath); err == nil {
		if n, ok := resolveSemanticSurface(rr, sem); ok {
			a.log.Printf("PRESENT_SEMANTIC pose=%s expression=%s gaze=%s surface=%d", rr.Presentation.Pose, rr.Presentation.Expression, rr.Presentation.Gaze, n)
			return n
		}
	}
	m := map[string]int{"normal": 20, "neutral": 20, "smile": 25, "cheerful": 25, "happy": 25, "surprised": 3, "concerned": 3, "wary": 3, "angry": 22, "embarrassed_angry": 22, "sad": 3, "embarrassed": 2, "embarrassed_smile": 2, "wry_smile": 2, "blush": 2, "blush_angry": 22}
	b, e := os.ReadFile(filepath.Join(a.root, "config", "presentation_map.json"))
	if e == nil {
		_ = json.Unmarshal(b, &m)
	}
	if n, ok := m[rr.Presentation.Expression]; ok {
		return n
	}
	if n, ok := m[rr.ReactionEmotion]; ok {
		return n
	}
	return 20
}

func (a *app) autonomousConfig() autonomousConfig {
	c := autonomousConfig{Enabled: true, TickIntervalMS: 180000, MinimumIdleMS: 90000, MaximumPendingContinuations: 1, MinimumDeferMS: 10000, MaximumDeferMS: 300000, ContinuationGraceMS: 120000, CancelDeferredOnNewUserInput: true, AllowAutonomousDuringTouch: false}
	b, e := os.ReadFile(filepath.Join(a.root, "config", "autonomous_rules.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.TickIntervalMS < 5000 {
		c.TickIntervalMS = 180000
	}
	if c.MinimumDeferMS < 1000 {
		c.MinimumDeferMS = 10000
	}
	if c.MaximumDeferMS < c.MinimumDeferMS {
		c.MaximumDeferMS = 300000
	}
	if c.MaximumPendingContinuations <= 0 {
		c.MaximumPendingContinuations = 1
	}
	if c.ContinuationGraceMS <= 0 {
		c.ContinuationGraceMS = 120000
	}
	return c
}
func (a *app) armAutonomousTimer() {
	c := a.autonomousConfig()
	if !c.Enabled {
		return
	}
	a.sendPresentation("\\![timerraise," + strconv.FormatInt(c.TickIntervalMS, 10) + ",1,OnCharacterGPTAutonomousTick]")
	a.log.Printf("AUTONOMOUS_TIMER_ARM delay_ms=%d", c.TickIntervalMS)
}
func (a *app) handleAutonomousTick() {
	c := a.autonomousConfig()
	if !c.Enabled {
		return
	}
	a.mu.Lock()
	idle := time.Since(a.lastUserActivity)
	busy := false
	for _, lc := range a.life {
		if lc.State == "processing" || lc.State == "processing_checkable" {
			if lc.Class == model.RequestChat || lc.Class == model.RequestPhysical || lc.Class == model.RequestDeferred {
				busy = true
				break
			}
		}
	}
	physicalActive := a.currentPhysical != nil && a.currentPhysical.Contact
	linkedActive := a.linked != nil && a.linked.Turn != nil && a.linkedRules().PauseAutonomousDuringActiveTurn
	a.mu.Unlock()
	if idle < time.Duration(c.MinimumIdleMS)*time.Millisecond || busy || linkedActive || (!c.AllowAutonomousDuringTouch && physicalActive) {
		a.log.Printf("AUTONOMOUS_SKIP idle_ms=%d busy=%t linked=%t physical=%t", idle.Milliseconds(), busy, linkedActive, physicalActive)
		a.auditCognitionf("AUTONOMOUS_DECISION action=skip idle_ms=%d busy=%t linked=%t physical=%t", idle.Milliseconds(), busy, linkedActive, physicalActive)
		a.armAutonomousTimer()
		return
	}
	id := a.newID("auto")
	env := a.buildEnvelope(id, model.RequestAutonomous, "autonomous", model.UserInput{}, nil, 0)
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	before := cloneAffect(a.affect)
	a.life[id] = &lifecycle{RequestID: id, State: "processing", Source: "autonomous", Class: model.RequestAutonomous, Created: time.Now(), cancel: cancel}
	a.mu.Unlock()
	go a.runJob(requestJob{env: env, ctx: ctx, cancel: cancel, affectBefore: before})
	a.log.Printf("REQUEST_CREATED id=%s class=autonomous route=fast", id)
}

func (a *app) prepareContinuationTimer(parent model.RequestEnvelope, rr model.Reaction) string {
	cfg := a.autonomousConfig()
	if rr.Continuation == nil {
		return ""
	}
	after := rr.Continuation.AfterMS
	if after < cfg.MinimumDeferMS {
		after = cfg.MinimumDeferMS
	}
	if after > cfg.MaximumDeferMS {
		after = cfg.MaximumDeferMS
	}
	a.mu.Lock()
	if len(a.continuations) >= cfg.MaximumPendingContinuations {
		var oldest string
		var ot time.Time
		for id, x := range a.continuations {
			if oldest == "" || x.Created.Before(ot) {
				oldest = id
				ot = x.Created
			}
		}
		delete(a.continuations, oldest)
	}
	cid := a.newID("cont")
	due := time.Now().Add(time.Duration(after) * time.Millisecond)
	ref := model.ContinuationRef{ContinuationID: cid, ParentRequest: parent.RequestID, Mode: "deferred_reply", DueAt: due.Format(time.RFC3339Nano), OriginalText: parent.UserInput.Text, UserEmotion: parent.UserInput.UserEmotion}
	a.continuations[cid] = continuationCapsule{Ref: ref, Created: time.Now()}
	a.mu.Unlock()
	a.log.Printf("CONTINUATION_SCHEDULED id=%s parent=%s after_ms=%d presentation_timer=embedded", cid, parent.RequestID, after)
	a.auditCognitionf("CONTINUATION action=schedule id=%s parent=%s after_ms=%d", cid, parent.RequestID, after)
	return "\\![timerraise," + strconv.FormatInt(after, 10) + ",1,OnCharacterGPTDeferred," + sstp.EscapeSakuraText(cid) + "]"
}
func (a *app) handleDeferred(cid string) {
	cfg := a.autonomousConfig()
	a.mu.Lock()
	cap, ok := a.continuations[cid]
	if ok {
		delete(a.continuations, cid)
	}
	busy := a.activeChat != ""
	a.mu.Unlock()
	if !ok {
		a.log.Printf("CONTINUATION_DROP id=%s reason=missing", cid)
		a.auditCognitionf("CONTINUATION action=drop id=%s reason=missing", cid)
		return
	}
	due, _ := time.Parse(time.RFC3339Nano, cap.Ref.DueAt)
	latenessMS := time.Since(due).Milliseconds()
	if time.Since(due) > time.Duration(cfg.ContinuationGraceMS)*time.Millisecond {
		a.log.Printf("CONTINUATION_DROP id=%s reason=stale", cid)
		a.auditCognitionf("CONTINUATION action=drop id=%s reason=stale lateness_ms=%d", cid, latenessMS)
		return
	}
	if busy {
		a.mu.Lock()
		a.continuations[cid] = cap
		a.mu.Unlock()
		a.sendPresentation("\\![timerraise,5000,1,OnCharacterGPTDeferred," + sstp.EscapeSakuraText(cid) + "]")
		a.log.Printf("CONTINUATION_REARM id=%s reason=busy", cid)
		a.auditCognitionf("CONTINUATION action=rearm id=%s reason=busy lateness_ms=%d", cid, latenessMS)
		return
	}
	id := a.newID("defer")
	env := a.buildEnvelope(id, model.RequestDeferred, "deferred", model.UserInput{}, nil, 0)
	env.Continuation = &cap.Ref
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	before := cloneAffect(a.affect)
	a.life[id] = &lifecycle{RequestID: id, State: "processing", Source: "deferred", Class: model.RequestDeferred, Created: time.Now(), cancel: cancel}
	a.mu.Unlock()
	go a.runJob(requestJob{env: env, ctx: ctx, cancel: cancel, affectBefore: before})
	a.log.Printf("REQUEST_CREATED id=%s class=deferred continuation=%s parent=%s", id, cid, cap.Ref.ParentRequest)
	a.auditCognitionf("CONTINUATION action=fire id=%s request=%s parent=%s lateness_ms=%d", cid, id, cap.Ref.ParentRequest, latenessMS)
}

func (a *app) touchSnapshot() map[string]any {
	ctx, c := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer c()
	var x map[string]any
	if err := httpGetJSON(ctx, "http://127.0.0.1:8769/state/snapshot", &x); err != nil {
		return nil
	}
	return x
}
func (a *app) appearance() model.AppearanceState {
	var x model.AppearanceState
	b, e := os.ReadFile(profilepath.Appearance(a.root))
	if e == nil {
		_ = json.Unmarshal(b, &x)
	}
	return x
}
func (a *app) rememberTouch(ev *model.PhysicalEvent) {
	if ev == nil {
		return
	}
	go func(v model.PhysicalEvent) {
		ctx, c := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer c()
		_ = httpjson.Post(ctx, "http://127.0.0.1:8769/touch/remember", v, nil)
	}(*ev)
}
func (a *app) observePhysical(ev *model.PhysicalEvent, importance float64) {
	if ev == nil {
		return
	}
	obs := map[string]any{"id": a.newID("obs"), "timestamp": model.Now(), "kind": "physical", "importance": importance, "event": ev}
	go func() {
		ctx, c := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer c()
		_ = httpjson.Post(ctx, "http://127.0.0.1:8768/v2/observe", obs, nil)
	}()
}

func (a *app) loadAffect() {
	p := profilepath.Affect(a.root)
	b, e := os.ReadFile(p)
	if e == nil && json.Unmarshal(b, &a.affect) == nil && a.affect.Channels != nil {
		return
	}
	a.affect = model.AffectState{FormatVersion: 1, UpdatedAt: model.Now(), Primary: "neutral", Channels: map[string]float64{"positive": 0, "shy": 0, "wary": 0, "annoyed": 0, "downcast": 0}}
	a.persistAffectLocked()
}
func (a *app) emotionRules() model.EmotionRules {
	c := model.EmotionRules{Enabled: true, HalfLifeSeconds: 300, NeutralThreshold: .05, DialogueWeight: .65, PhysicalWeight: 1, Impulses: map[string]map[string]float64{}}
	b, e := os.ReadFile(filepath.Join(a.root, "config", "emotional_state_rules.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.HalfLifeSeconds <= 0 {
		c.HalfLifeSeconds = 300
	}
	return c
}
func (a *app) decayAffectLocked() (model.AffectState, int64, float64) {
	r := a.emotionRules()
	now := time.Now()
	elapsedMS := int64(0)
	factor := 1.0
	t, e := time.Parse(time.RFC3339Nano, a.affect.UpdatedAt)
	if e == nil {
		elapsed := now.Sub(t)
		if elapsed < 0 {
			elapsed = 0
		}
		elapsedMS = elapsed.Milliseconds()
		factor = math.Pow(.5, elapsed.Seconds()/r.HalfLifeSeconds)
		for k, v := range a.affect.Channels {
			a.affect.Channels[k] = v * factor
		}
	}
	a.affect.UpdatedAt = now.Format(time.RFC3339Nano)
	a.recomputeAffectLocked(r)
	return cloneAffect(a.affect), elapsedMS, factor
}

func (a *app) decayedAffectLocked() model.AffectState {
	x, _, _ := a.decayAffectLocked()
	return x
}

func (a *app) updateAffectLocked(requestID, reaction, source, causeText string) (model.AffectState, model.AffectState) {
	r := a.emotionRules()
	_, decayElapsedMS, decayFactor := a.decayAffectLocked()
	beforeImpulse := cloneAffect(a.affect)
	weight := r.DialogueWeight
	if source == "physical" {
		weight = r.PhysicalWeight
	}
	applied := map[string]float64{}
	for ch, delta := range r.Impulses[reaction] {
		cur := a.affect.Channels[ch]
		next := math.Min(1.5, cur+delta*weight*(1-math.Min(cur, 1)*.35))
		applied[ch] = next - cur
		a.affect.Channels[ch] = next
	}
	a.affect.Revision++
	a.affect.LastReaction = reaction
	a.affect.LastSource = source
	a.affect.LastCause = causeText
	a.recomputeAffectLocked(r)
	a.persistAffectLocked()
	a.auditAffectf("AFFECT_REACTION request=%s reaction=%s source=%s revision=%d decay_elapsed_ms=%d decay_factor=%.6f weight=%.3f applied=%s primary=%s intensity=%.4f current=%s", requestID, reaction, source, a.affect.Revision, decayElapsedMS, decayFactor, weight, formatDelta(applied), a.affect.Primary, a.affect.Intensity, formatAffect(a.affect))
	return beforeImpulse, cloneAffect(a.affect)
}

func (a *app) recomputeAffectLocked(r model.EmotionRules) {
	primary := "neutral"
	max := 0.0
	for _, k := range []string{"positive", "shy", "wary", "annoyed", "downcast"} {
		if a.affect.Channels[k] > max {
			max = a.affect.Channels[k]
			primary = k
		}
	}
	if max < r.NeutralThreshold {
		primary = "neutral"
	}
	a.affect.Primary = primary
	a.affect.Intensity = max
}
func (a *app) persistAffectLocked() {
	b, _ := json.MarshalIndent(a.affect, "", "  ")
	_ = os.MkdirAll(profilepath.State(a.root), 0755)
	_ = os.WriteFile(profilepath.Affect(a.root), b, 0644)
}
func (a *app) auditConfig() auditRules {
	c := auditRules{Affect: true, Cognition: true, Memory: true}
	b, err := os.ReadFile(filepath.Join(a.root, "config", "audit_rules.json"))
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func (a *app) auditAffectf(format string, args ...any) {
	if a.affectAudit != nil && a.auditConfig().Affect {
		a.affectAudit.Printf(format, args...)
	}
}

func (a *app) auditCognitionf(format string, args ...any) {
	if a.cognitionAudit != nil && a.auditConfig().Cognition {
		a.cognitionAudit.Printf(format, args...)
	}
}

func formatAffect(x model.AffectState) string {
	return fmt.Sprintf("{positive:%.4f,shy:%.4f,wary:%.4f,annoyed:%.4f,downcast:%.4f}", x.Channels["positive"], x.Channels["shy"], x.Channels["wary"], x.Channels["annoyed"], x.Channels["downcast"])
}

func formatDelta(x map[string]float64) string {
	return fmt.Sprintf("{positive:%+.4f,shy:%+.4f,wary:%+.4f,annoyed:%+.4f,downcast:%+.4f}", x["positive"], x["shy"], x["wary"], x["annoyed"], x["downcast"])
}

func physicalAudit(x *model.PhysicalEvent) string {
	if x == nil {
		return "none"
	}
	return fmt.Sprintf("%s/%s/contact=%t/released=%t", x.Target, x.Gesture, x.Contact, x.Released)
}

func cloneAffect(x model.AffectState) model.AffectState {
	y := x
	y.Channels = map[string]float64{}
	for k, v := range x.Channels {
		y.Channels[k] = v
	}
	return y
}
func clonePhysical(x *model.PhysicalEvent) *model.PhysicalEvent {
	if x == nil {
		return nil
	}
	y := *x
	return &y
}
func (a *app) newID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), a.seq.Add(1))
}
func (a *app) noteUserActivity() { a.mu.Lock(); a.lastUserActivity = time.Now(); a.mu.Unlock() }

func (a *app) recentDialogueLocked(now time.Time) []model.DialogueTurn {
	cut := now.Add(-10 * time.Minute)
	start := 0
	for start < len(a.recentDialogue) {
		t, err := time.Parse(time.RFC3339Nano, a.recentDialogue[start].Timestamp)
		if err == nil && !t.Before(cut) {
			break
		}
		start++
	}
	if start > 0 {
		a.recentDialogue = append([]model.DialogueTurn(nil), a.recentDialogue[start:]...)
	}
	if len(a.recentDialogue) > 6 {
		a.recentDialogue = append([]model.DialogueTurn(nil), a.recentDialogue[len(a.recentDialogue)-6:]...)
	}
	return append([]model.DialogueTurn(nil), a.recentDialogue...)
}
func (a *app) rememberDialogue(t model.DialogueTurn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t.Timestamp == "" {
		t.Timestamp = model.Now()
	}
	a.recentDialogue = append(a.recentDialogue, t)
	_ = a.recentDialogueLocked(time.Now())
}

func (a *app) recentPhysicalLocked(now time.Time) []model.PhysicalEvent {
	cut := now.Add(-3 * time.Minute)
	out := a.recentPhysical[:0]
	for _, ev := range a.recentPhysical {
		t, err := time.Parse(time.RFC3339Nano, ev.ObservedAt)
		if err == nil && t.Before(cut) {
			continue
		}
		out = append(out, ev)
	}
	a.recentPhysical = out
	if len(a.recentPhysical) > 32 {
		a.recentPhysical = append([]model.PhysicalEvent(nil), a.recentPhysical[len(a.recentPhysical)-32:]...)
	}
	return append([]model.PhysicalEvent(nil), a.recentPhysical...)
}

func (a *app) rememberPhysicalOccurrence(ev *model.PhysicalEvent) {
	if ev == nil || strings.TrimSpace(ev.Gesture) == "" || ev.Gesture == "release" {
		return
	}
	x := *ev
	if x.ObservedAt == "" {
		x.ObservedAt = model.Now()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if x.SessionID != "" {
		for i := len(a.recentPhysical) - 1; i >= 0; i-- {
			old := a.recentPhysical[i]
			if old.SessionID != x.SessionID {
				continue
			}
			// Progress/final updates from one physical session are one occurrence.
			// Keep the highest-impulse classification, while allowing the same
			// classification to refresh duration/final facts.
			if old.Gesture == x.Gesture || a.impulse(x.Gesture) >= a.impulse(old.Gesture) {
				a.recentPhysical[i] = x
			}
			_ = a.recentPhysicalLocked(time.Now())
			return
		}
	}
	a.recentPhysical = append(a.recentPhysical, x)
	_ = a.recentPhysicalLocked(time.Now())
}

func (a *app) inputConfig() inputUIConfig {
	c := inputUIConfig{Emotions: []string{"neutral", "happy", "cheerful", "shy", "surprised", "concerned", "annoyed", "angry", "sad"}, SSPBacklogMirror: true, DefaultEmotion: "neutral", DefaultCheckMS: 15000}
	for _, x := range []struct {
		l string
		m int64
	}{{"5 秒", 5000}, {"10 秒", 10000}, {"15 秒", 15000}, {"30 秒", 30000}, {"60 秒", 60000}, {"手動", 0}} {
		c.CheckThresholds = append(c.CheckThresholds, struct {
			Label        string `json:"label"`
			Milliseconds int64  `json:"milliseconds"`
		}{x.l, x.m})
	}
	b, e := os.ReadFile(filepath.Join(a.root, "config", "input_ui.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}
func (a *app) loadSettings() {
	cfg := a.inputConfig()
	a.backlogMirror = cfg.SSPBacklogMirror
	var x struct {
		BalloonTimeoutMS int   `json:"balloon_timeout_ms"`
		BacklogMirror    *bool `json:"ssp_backlog_mirror"`
	}
	b, e := os.ReadFile(profilepath.RuntimeSettings(a.root))
	if e == nil && json.Unmarshal(b, &x) == nil {
		if x.BalloonTimeoutMS >= 0 {
			a.balloonMS = x.BalloonTimeoutMS
		}
		if x.BacklogMirror != nil {
			a.backlogMirror = *x.BacklogMirror
		}
	}
}
func (a *app) saveSettings() {
	a.mu.Lock()
	x := struct {
		BalloonTimeoutMS int  `json:"balloon_timeout_ms"`
		BacklogMirror    bool `json:"ssp_backlog_mirror"`
	}{a.balloonMS, a.backlogMirror}
	a.mu.Unlock()
	b, _ := json.MarshalIndent(x, "", "  ")
	_ = os.MkdirAll(profilepath.Settings(a.root), 0755)
	_ = os.WriteFile(profilepath.RuntimeSettings(a.root), b, 0644)
}

func httpGetJSON(ctx context.Context, url string, out any) error {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return e
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
