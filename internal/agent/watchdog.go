package agent

import (
	"crypto/sha256"
	"fmt"
)

// Watchdog detects stuckness signals in the agent loop.
// Phase 1 implements two signals: edit-loop (same content hash N times in a
// row) and no-progress window (no sensor delta for K consecutive turns).
type Watchdog struct {
	// EditLoopThreshold triggers when the same response appears this many times
	// consecutively. Zero disables this check.
	EditLoopThreshold int
	// NoProgressThreshold triggers when sensor state is unchanged for this
	// many consecutive turns. Zero disables this check.
	NoProgressThreshold int

	contentHashes   []string
	lastSensorHash  string
	noProgressTurns int
}

// NewWatchdog returns a Watchdog with production defaults.
func NewWatchdog() *Watchdog {
	return &Watchdog{
		EditLoopThreshold:   3,
		NoProgressThreshold: 5,
	}
}

// RecordTurn updates the watchdog with content from the latest assistant turn
// and the current sensor hash. Returns a non-empty reason string if stuckness
// is detected, or empty string if the session appears to be making progress.
func (w *Watchdog) RecordTurn(content, sensorHash string) string {
	h := contentHash(content)
	w.contentHashes = append(w.contentHashes, h)

	if w.EditLoopThreshold > 0 && len(w.contentHashes) >= w.EditLoopThreshold {
		tail := w.contentHashes[len(w.contentHashes)-w.EditLoopThreshold:]
		if allEqual(tail) {
			return fmt.Sprintf("edit-loop: same response for %d consecutive turns", w.EditLoopThreshold)
		}
	}

	if sensorHash != "" {
		if sensorHash == w.lastSensorHash {
			w.noProgressTurns++
		} else {
			w.noProgressTurns = 0
			w.lastSensorHash = sensorHash
		}
		if w.NoProgressThreshold > 0 && w.noProgressTurns >= w.NoProgressThreshold {
			return fmt.Sprintf("no sensor progress for %d consecutive turns", w.noProgressTurns)
		}
	}

	return ""
}

func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}

func allEqual(ss []string) bool {
	for i := 1; i < len(ss); i++ {
		if ss[i] != ss[0] {
			return false
		}
	}
	return true
}
