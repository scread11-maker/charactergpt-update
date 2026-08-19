package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"sspgpt/v07/internal/httpjson"
	"sspgpt/v07/internal/model"
	"sspgpt/v07/internal/paths"
	"sspgpt/v07/internal/profilepath"
	"sspgpt/v07/internal/singleinstance"
)

const version = "0.7.1-fix12"

type active struct {
	Target, CharacterID, SessionID    string
	Started, LastMotion, StationaryAt time.Time
	Resting                           bool
	Generation                        int64
	PendingRelease                    bool
	ReleaseToken                      int64
}
type touchEntry struct {
	State       string  `json:"state"`
	Strength    float64 `json:"strength"`
	UpdatedAt   string  `json:"updated_at"`
	LastTouchAt string  `json:"last_touch_at"`
}
type disk struct {
	FormatVersion int                   `json:"format_version"`
	UpdatedAt     string                `json:"updated_at"`
	Targets       map[string]touchEntry `json:"targets"`
}
type service struct {
	mu             sync.Mutex
	root           string
	log            *log.Logger
	active         map[string]*active
	memory         disk
	gen            int64
	sessionImpulse map[string]float64
	shutdownOnce   sync.Once
}

func main() {
	root := paths.GhostRoot()
	if !singleinstance.Acquire("TouchProgress", root) {
		return
	}
	_ = os.MkdirAll(filepath.Join(root, "logs"), 0755)
	lf, _ := os.OpenFile(filepath.Join(root, "logs", "touch_progress.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	s := &service{root: root, log: log.New(lf, "", log.LstdFlags|log.Lmicroseconds), active: map[string]*active{}, memory: disk{FormatVersion: 2, Targets: map[string]touchEntry{}}, sessionImpulse: map[string]float64{}}
	for _, action := range profilepath.MigrateTouch(root) {
		s.log.Printf("PROFILE_LAYOUT %s", action)
	}
	s.loadMemory()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		httpjson.Write(w, 200, map[string]any{"ok": true, "service": "TouchProgress", "version": version})
	})
	mux.HandleFunc("/touch/start", s.start)
	mux.HandleFunc("/touch/motion", s.motion)
	mux.HandleFunc("/touch/stationary", s.stationary)
	mux.HandleFunc("/touch/resting", s.resting)
	mux.HandleFunc("/touch/progress", s.progress)
	mux.HandleFunc("/touch/release", s.release)
	mux.HandleFunc("/touch/memory", s.memoryEvent)
	mux.HandleFunc("/touch/remember", s.rememberJSON)
	mux.HandleFunc("/state/snapshot", s.snapshot)
	mux.HandleFunc("/shutdown", s.shutdown)
	addr := "127.0.0.1:8769"
	s.log.Printf("TouchProgress %s listening %s root=%s", version, addr, root)
	if err := http.ListenAndServe(addr, mux); err != nil {
		s.log.Fatal(err)
	}
}

func canonical(cid, target string) string {
	if cid == "1" && !strings.HasPrefix(target, "Owl.") {
		return "Owl." + target
	}
	return target
}
func key(cid, target string) string { return cid + "|" + canonical(cid, target) }

func (s *service) start(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	cid := r.Form.Get("character_id")
	if cid == "" {
		cid = "0"
	}
	target := canonical(cid, r.Form.Get("target"))
	reason := r.Form.Get("reason")
	if target == "" {
		httpjson.Write(w, 400, map[string]string{"error": "missing target"})
		return
	}
	now := time.Now()
	k := key(cid, target)
	freshLifecycle := reason == "first_move" || reason == "reentry" || reason == "implicit_target_change"
	s.mu.Lock()
	if a := s.active[k]; a != nil && a.PendingRelease {
		a.PendingRelease = false
		s.gen++
		a.Generation = s.gen
		a.ReleaseToken = s.gen
		a.LastMotion = now
		a.Resting = false
		a.StationaryAt = time.Time{}
		sid := a.SessionID
		s.mu.Unlock()
		s.log.Printf("RESUMED session=%s target=%s character=%s reason=%s", sid, target, cid, reason)
		httpjson.Write(w, 200, map[string]any{"ok": true, "session_id": sid, "resumed": true})
		return
	}
	if a := s.active[k]; a != nil && !freshLifecycle {
		sid := a.SessionID
		s.mu.Unlock()
		httpjson.Write(w, 200, map[string]any{"ok": true, "session_id": sid, "existing": true})
		return
	}

	// A fresh SHIORI contact start is positive evidence of the current body
	// target. A mouse cursor cannot authoritatively remain on two collision
	// targets of the same character at once. If an older helper lifecycle was
	// left behind because SSP omitted MouseLeave, close it here instead of
	// allowing ghost contact to survive for minutes.
	retired := s.retireConflictingLocked(cid, target, freshLifecycle, now)
	s.gen++
	a := &active{Target: target, CharacterID: cid, SessionID: fmt.Sprintf("t-%d", now.UnixNano()), Started: now, LastMotion: now, Generation: s.gen, ReleaseToken: s.gen}
	s.active[k] = a
	s.mu.Unlock()

	for i := range retired {
		ev := retired[i]
		s.log.Printf("STALE_CONTACT_RETIRED session=%s target=%s character=%s replacement=%s reason=new_contact_start duration_ms=%d", ev.SessionID, ev.Target, ev.CharacterID, target, ev.DurationMS)
		notifyRuntime("/internal/touch/event", &ev)
	}
	s.log.Printf("STARTED session=%s target=%s character=%s reason=%s", a.SessionID, target, cid, reason)
	httpjson.Write(w, 200, map[string]any{"ok": true, "session_id": a.SessionID, "retired": len(retired)})
}

// retireConflictingLocked enforces one authoritative active collision target
// per character. replaceSame is used only for an explicit fresh /touch/start:
// if SHIORI says a new lifecycle began while the backend still has the same
// target active, that backend lifecycle is stale and must be closed. A
// provisional release is handled before this helper and therefore still
// resumes normally within the re-entry guard.
func (s *service) retireConflictingLocked(cid, target string, replaceSame bool, now time.Time) []model.PhysicalEvent {
	retired := []model.PhysicalEvent{}
	for k, a := range s.active {
		if a == nil || a.CharacterID != cid || a.PendingRelease {
			continue
		}
		if a.Target == target && !replaceSame {
			continue
		}
		ev := model.PhysicalEvent{
			Type:          "physical",
			Gesture:       "release",
			Target:        a.Target,
			CharacterID:   a.CharacterID,
			Phase:         "release",
			Contact:       false,
			Released:      true,
			SessionID:     a.SessionID,
			DurationMS:    now.Sub(a.Started).Milliseconds(),
			ObservedAt:    now.Format(time.RFC3339Nano),
			Authoritative: true,
		}
		retired = append(retired, ev)
		delete(s.active, k)
		delete(s.sessionImpulse, a.SessionID)
	}
	return retired
}

func (s *service) motion(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	target := r.Form.Get("target")
	now := time.Now()
	s.mu.Lock()
	for _, a := range s.active {
		if a.Target == target || strings.TrimPrefix(a.Target, "Owl.") == target {
			a.LastMotion = now
			a.StationaryAt = time.Time{}
			a.Resting = false
		}
	}
	s.mu.Unlock()
	httpjson.Write(w, 200, map[string]any{"ok": true})
}
func (s *service) stationary(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	target := r.Form.Get("target")
	now := time.Now()
	s.mu.Lock()
	for _, a := range s.active {
		if a.Target == target || strings.TrimPrefix(a.Target, "Owl.") == target {
			a.StationaryAt = now
		}
	}
	s.mu.Unlock()
	s.log.Printf("STATE target=%s state=stationary", target)
	httpjson.Write(w, 200, map[string]any{"ok": true})
}
func (s *service) resting(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	target := r.Form.Get("target")
	var ev *model.PhysicalEvent
	s.mu.Lock()
	for _, a := range s.active {
		if (a.Target == target || strings.TrimPrefix(a.Target, "Owl.") == target) && !a.PendingRelease {
			a.Resting = true
			e := model.PhysicalEvent{Type: "physical", Gesture: "resting_touch", Target: a.Target, CharacterID: a.CharacterID, Phase: "resting", Contact: true, Moving: false, Resting: true, SessionID: a.SessionID, DurationMS: time.Since(a.Started).Milliseconds(), ObservedAt: model.Now(), Authoritative: true, Intensity: .25}
			ev = &e
			break
		}
	}
	s.mu.Unlock()
	if ev != nil {
		s.log.Printf("RESTING_DETECTED session=%s target=%s duration_ms=%d", ev.SessionID, ev.Target, ev.DurationMS)
		go notifyRuntime("/internal/touch/event", ev)
		s.remember(*ev)
	}
	httpjson.Write(w, 200, map[string]any{"ok": true, "found": ev != nil})
}
func (s *service) progress(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	target := r.Form.Get("target")
	dur, _ := strconv.ParseInt(r.Form.Get("duration_ms"), 10, 64)
	var ev *model.PhysicalEvent
	s.mu.Lock()
	for _, a := range s.active {
		if (a.Target == target || strings.TrimPrefix(a.Target, "Owl.") == target) && !a.PendingRelease {
			e := model.PhysicalEvent{Type: "physical", Target: a.Target, CharacterID: a.CharacterID, Phase: "progress", Contact: true, Moving: !a.Resting, Resting: a.Resting, SessionID: a.SessionID, DurationMS: dur, ObservedAt: model.Now(), Authoritative: true}
			ev = &e
			break
		}
	}
	s.mu.Unlock()
	if ev != nil {
		go notifyRuntime("/internal/touch/progress", ev)
	}
	httpjson.Write(w, 200, map[string]any{"ok": true, "found": ev != nil})
}

func (s *service) release(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	cid := r.Form.Get("character_id")
	if cid == "" {
		cid = "0"
	}
	target := canonical(cid, r.Form.Get("target"))
	k := key(cid, target)
	now := time.Now()
	s.mu.Lock()
	a := s.active[k]
	if a == nil {
		s.mu.Unlock()
		// A release edge is authoritative even if TouchProgress has already lost
		// (or never observed) its helper-side active record. Runtime may still
		// hold a MOVE-derived provisional session; discarding this release is the
		// exact failure mode that creates multi-minute ghost contact. Forward a
		// target/character release unconditionally so Runtime can clear NOW.
		ev := model.PhysicalEvent{Type: "physical", Gesture: "release", Target: target, CharacterID: cid, Phase: "release", Contact: false, Released: true, ObservedAt: now.Format(time.RFC3339Nano), Authoritative: true}
		s.log.Printf("RELEASE_FORWARDED target=%s character=%s reason=no_active authoritative=true", target, cid)
		go notifyRuntime("/internal/touch/event", &ev)
		httpjson.Write(w, 200, map[string]any{"ok": true, "active": false, "forwarded": true})
		return
	}
	if a.PendingRelease {
		s.mu.Unlock()
		httpjson.Write(w, 200, map[string]any{"ok": true, "active": true, "pending": true, "session_id": a.SessionID})
		return
	}
	s.gen++
	a.PendingRelease = true
	a.ReleaseToken = s.gen
	token := a.ReleaseToken
	ev := model.PhysicalEvent{Type: "physical", Gesture: "release", Target: a.Target, CharacterID: cid, Phase: "release", Contact: false, Released: true, SessionID: a.SessionID, DurationMS: now.Sub(a.Started).Milliseconds(), ObservedAt: now.Format(time.RFC3339Nano), Authoritative: true}
	s.mu.Unlock()
	s.log.Printf("RELEASE_PENDING session=%s target=%s reason=%s", a.SessionID, target, r.Form.Get("reason"))
	go func() {
		time.Sleep(120 * time.Millisecond)
		s.mu.Lock()
		cur := s.active[k]
		if cur == nil || !cur.PendingRelease || cur.ReleaseToken != token {
			s.mu.Unlock()
			s.log.Printf("RELEASE_SUPERSEDED session=%s target=%s", ev.SessionID, target)
			return
		}
		delete(s.active, k)
		delete(s.sessionImpulse, ev.SessionID)
		s.mu.Unlock()
		s.log.Printf("RELEASE_CONFIRMED session=%s target=%s duration_ms=%d", ev.SessionID, target, ev.DurationMS)
		notifyRuntime("/internal/touch/event", &ev)
	}()
	httpjson.Write(w, 200, map[string]any{"ok": true, "active": true, "session_id": a.SessionID})
}

func (s *service) memoryEvent(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	state := r.Form.Get("state")
	if state == "double" {
		state = "heavy_tap"
	}
	cid := r.Form.Get("character_id")
	ev := model.PhysicalEvent{Type: "physical", Gesture: state, Target: canonical(cid, r.Form.Get("target")), CharacterID: cid, Phase: "instant", ObservedAt: model.Now(), Authoritative: true}
	switch state {
	case "light_touch":
		ev.Intensity = .2
	case "heavy_tap":
		ev.Intensity = .55
	case "grab":
		ev.Intensity = .65
	}
	s.remember(ev)
	httpjson.Write(w, 200, map[string]any{"ok": true})
}
func (s *service) rememberJSON(w http.ResponseWriter, r *http.Request) {
	var ev model.PhysicalEvent
	if err := httpjson.Decode(r, &ev); err != nil {
		httpjson.Write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	ev.Target = canonical(ev.CharacterID, ev.Target)
	// Runtime and TouchProgress intentionally have separate local session IDs.
	// For body salience accounting, map Runtime's normalized event back to the
	// authoritative TouchProgress contact session so stroke -> resting/escalation
	// remains one logical contact and cannot double-count salience.
	s.mu.Lock()
	if a := s.active[key(ev.CharacterID, ev.Target)]; a != nil && !a.PendingRelease {
		ev.SessionID = a.SessionID
	}
	s.mu.Unlock()
	s.remember(ev)
	httpjson.Write(w, 200, map[string]any{"ok": true})
}

func (s *service) remember(ev model.PhysicalEvent) {
	if ev.Target == "" || ev.Gesture == "" || ev.Gesture == "release" {
		return
	}
	rules := s.rules()
	imp := rules.Impulses[ev.Gesture]
	if imp <= 0 {
		imp = 200
	}
	now := time.Now()
	s.mu.Lock()
	old := s.memory.Targets[ev.Target]
	old.Strength = decay(old.Strength, old.UpdatedAt, rules.HalfLifeSeconds, now)
	incoming := math.Min(100, imp/5)
	if ev.SessionID != "" {
		previous := s.sessionImpulse[ev.SessionID]
		if ev.Gesture == "resting_touch" || imp <= previous {
			incoming = 0
		} else if previous > 0 {
			incoming = math.Min(100, (imp-previous)/5)
		}
		if imp > previous {
			s.sessionImpulse[ev.SessionID] = imp
		}
	}
	if incoming > 0 {
		gain := incoming * (1 - math.Min(old.Strength, 130)/160)
		if gain < incoming*.2 {
			gain = incoming * .2
		}
		old.Strength = math.Min(140, old.Strength+gain)
	}
	old.State = ev.Gesture
	old.UpdatedAt = now.Format(time.RFC3339Nano)
	if incoming > 0 {
		old.LastTouchAt = old.UpdatedAt
	}
	s.memory.Targets[ev.Target] = old
	s.memory.UpdatedAt = old.UpdatedAt
	s.persistLocked()
	s.mu.Unlock()
	s.log.Printf("TOUCH_MEMORY_SET session=%s target=%s state=%s strength=%.1f delta_input=%.1f", ev.SessionID, ev.Target, ev.Gesture, old.Strength, incoming)
}

func (s *service) snapshot(w http.ResponseWriter, r *http.Request) {
	rules := s.rules()
	now := time.Now()
	s.mu.Lock()
	targets := map[string]touchEntry{}
	for k, v := range s.memory.Targets {
		v.Strength = decay(v.Strength, v.UpdatedAt, rules.HalfLifeSeconds, now)
		if v.Strength >= rules.ForgetThreshold {
			targets[k] = v
		}
	}
	activeEvents := []model.PhysicalEvent{}
	for _, a := range s.active {
		if a.PendingRelease {
			continue
		}
		g := "contact"
		phase := "contact"
		if a.Resting {
			g = "resting_touch"
			phase = "resting"
		}
		activeEvents = append(activeEvents, model.PhysicalEvent{Type: "physical", Gesture: g, Target: a.Target, CharacterID: a.CharacterID, Phase: phase, Contact: true, Moving: !a.Resting, Resting: a.Resting, SessionID: a.SessionID, DurationMS: now.Sub(a.Started).Milliseconds(), ObservedAt: now.Format(time.RFC3339Nano), Authoritative: true})
	}
	s.mu.Unlock()
	httpjson.Write(w, 200, map[string]any{"updated_at": now.Format(time.RFC3339Nano), "active": activeEvents, "targets": targets})
}

func (s *service) rules() model.TouchMemoryRules {
	c := model.TouchMemoryRules{Enabled: true, HalfLifeSeconds: 300, ForgetThreshold: 20, DiminishingScale: 900, Impulses: map[string]float64{"resting_touch": 120, "light_touch": 220, "gentle_stroke": 260, "stroke": 300, "heavy_tap": 340, "rough_rub": 450, "grab": 500}}
	b, e := os.ReadFile(filepath.Join(s.root, "config", "touch_memory_rules.json"))
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.HalfLifeSeconds <= 0 {
		c.HalfLifeSeconds = 300
	}
	return c
}
func decay(v float64, ts string, half float64, now time.Time) float64 {
	if v <= 0 || ts == "" {
		return v
	}
	t, e := time.Parse(time.RFC3339Nano, ts)
	if e != nil {
		return v
	}
	return v * math.Pow(.5, now.Sub(t).Seconds()/half)
}
func (s *service) persistLocked() {
	_ = os.MkdirAll(profilepath.State(s.root), 0755)
	b, _ := json.MarshalIndent(s.memory, "", "  ")
	_ = os.WriteFile(profilepath.Touch(s.root), b, 0644)
}
func (s *service) loadMemory() {
	b, e := os.ReadFile(profilepath.Touch(s.root))
	if e == nil {
		var x disk
		if json.Unmarshal(b, &x) == nil && x.Targets != nil {
			s.memory = x
		}
	}
}
func notifyRuntime(path string, ev *model.PhysicalEvent) {
	ctx, c := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer c()
	_ = httpjson.Post(ctx, "http://127.0.0.1:8770"+path, ev, nil)
}
func (s *service) shutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	started := false
	s.shutdownOnce.Do(func() {
		started = true
		s.mu.Lock()
		s.persistLocked()
		activeCount := len(s.active)
		s.mu.Unlock()
		s.log.Printf("SHUTDOWN_BEGIN persisted=true active_contacts=%d", activeCount)
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.log.Printf("SHUTDOWN_COMPLETE")
			os.Exit(0)
		}()
	})
	httpjson.Write(w, http.StatusAccepted, map[string]any{"ok": true, "started": started, "service": "TouchProgress", "version": version})
}
