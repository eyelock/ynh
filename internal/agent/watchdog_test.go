package agent

import (
	"testing"
)

func TestWatchdog_NoStuck(t *testing.T) {
	w := NewWatchdog()
	for i := 0; i < 4; i++ {
		reason := w.RecordTurn("different content "+string(rune('A'+i)), "hash"+string(rune('A'+i)))
		if reason != "" {
			t.Errorf("turn %d: unexpected stuck signal: %s", i, reason)
		}
	}
}

func TestWatchdog_EditLoop(t *testing.T) {
	w := &Watchdog{EditLoopThreshold: 3, NoProgressThreshold: 0}
	w.RecordTurn("same content", "h1")
	w.RecordTurn("same content", "h2")
	reason := w.RecordTurn("same content", "h3")
	if reason == "" {
		t.Error("expected edit-loop detection on 3rd identical turn")
	}
}

func TestWatchdog_EditLoopBreaks(t *testing.T) {
	w := &Watchdog{EditLoopThreshold: 3, NoProgressThreshold: 0}
	w.RecordTurn("same", "h1")
	w.RecordTurn("same", "h2")
	w.RecordTurn("different", "h3") // breaks the streak
	reason := w.RecordTurn("same", "h4")
	if reason != "" {
		t.Errorf("streak broken by different content; should not trigger: %s", reason)
	}
}

func TestWatchdog_NoProgress(t *testing.T) {
	// First call with a new hash resets the counter (hash was "").
	// Subsequent calls with the same hash increment it.
	// Threshold=3 triggers when noProgressTurns reaches 3.
	w := &Watchdog{EditLoopThreshold: 0, NoProgressThreshold: 3}
	sensorHash := "unchangedHash"
	w.RecordTurn("a", sensorHash)           // first: resets counter (hash changed from ""), count=0
	w.RecordTurn("b", sensorHash)           // count=1
	w.RecordTurn("c", sensorHash)           // count=2
	reason := w.RecordTurn("d", sensorHash) // count=3 → trigger
	if reason == "" {
		t.Error("expected no-progress detection after 4 turns with same sensor hash")
	}
}

func TestWatchdog_NoProgressResets(t *testing.T) {
	w := &Watchdog{EditLoopThreshold: 0, NoProgressThreshold: 3}
	w.RecordTurn("a", "hash1")
	w.RecordTurn("b", "hash1")
	w.RecordTurn("c", "hash2") // sensor changed — reset counter
	reason := w.RecordTurn("d", "hash2")
	if reason != "" {
		t.Errorf("sensor changed at turn 3; counter should have reset: %s", reason)
	}
}

func TestWatchdog_DisabledChecks(t *testing.T) {
	w := &Watchdog{EditLoopThreshold: 0, NoProgressThreshold: 0}
	for i := 0; i < 20; i++ {
		reason := w.RecordTurn("same", "same")
		if reason != "" {
			t.Errorf("disabled watchdog should never trigger, got: %s", reason)
		}
	}
}

func TestWatchdog_EmptySensorHash(t *testing.T) {
	// If no sensors are declared, sensorHash is ""; no-progress check must skip.
	w := &Watchdog{EditLoopThreshold: 0, NoProgressThreshold: 3}
	for i := 0; i < 10; i++ {
		reason := w.RecordTurn("content", "")
		if reason != "" {
			t.Errorf("empty sensor hash should skip no-progress check: %s", reason)
		}
	}
}
