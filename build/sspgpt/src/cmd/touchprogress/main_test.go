package main

import (
	"testing"
	"time"
)

func TestCanonicalOwlTarget(t *testing.T) {
	if got := canonical("1", "Head"); got != "Owl.Head" {
		t.Fatalf("got %q", got)
	}
	if got := canonical("0", "Head"); got != "Head" {
		t.Fatalf("got %q", got)
	}
}

func TestRetireConflictingLockedEnforcesSingleContactPerCharacter(t *testing.T) {
	now := time.Now()
	s := &service{
		active:         map[string]*active{},
		sessionImpulse: map[string]float64{},
	}
	oldBust := &active{Target: "Owl.Bust", CharacterID: "1", SessionID: "old-bust", Started: now.Add(-5 * time.Minute)}
	otherCharacter := &active{Target: "Head", CharacterID: "0", SessionID: "main-head", Started: now.Add(-time.Second)}
	s.active[key("1", "Owl.Bust")] = oldBust
	s.active[key("0", "Head")] = otherCharacter
	s.sessionImpulse[oldBust.SessionID] = 300

	retired := s.retireConflictingLocked("1", "Owl.Wing", true, now)
	if len(retired) != 1 {
		t.Fatalf("retired=%d want=1", len(retired))
	}
	if retired[0].Target != "Owl.Bust" || !retired[0].Released || retired[0].Contact {
		t.Fatalf("unexpected retired event: %#v", retired[0])
	}
	if retired[0].DurationMS < int64((4*time.Minute)/time.Millisecond) {
		t.Fatalf("duration not preserved: %d", retired[0].DurationMS)
	}
	if _, ok := s.active[key("1", "Owl.Bust")]; ok {
		t.Fatal("stale Owl.Bust contact remained active")
	}
	if _, ok := s.active[key("0", "Head")]; !ok {
		t.Fatal("another character contact must not be retired")
	}
	if _, ok := s.sessionImpulse[oldBust.SessionID]; ok {
		t.Fatal("retired session impulse must be cleared")
	}
}

func TestRetireConflictingLockedCanReplaceSameTargetFreshLifecycle(t *testing.T) {
	now := time.Now()
	s := &service{active: map[string]*active{}, sessionImpulse: map[string]float64{}}
	s.active[key("1", "Owl.Bust")] = &active{Target: "Owl.Bust", CharacterID: "1", SessionID: "stale", Started: now.Add(-time.Minute)}

	retired := s.retireConflictingLocked("1", "Owl.Bust", true, now)
	if len(retired) != 1 || retired[0].SessionID != "stale" {
		t.Fatalf("fresh lifecycle did not replace stale same-target contact: %#v", retired)
	}
}
