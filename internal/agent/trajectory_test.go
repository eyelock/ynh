package agent

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestTrajectoryWriter_EmitsValidNDJSON(t *testing.T) {
	var buf bytes.Buffer
	tw := NewTrajectoryWriter(&buf)

	if err := tw.Emit(KindSessionStart, 0, SessionStartData{
		SessionID: "abc123",
		Harness:   "myharness",
		Backend:   "claude",
		Task:      "fix the bug",
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if err := tw.Emit(KindTurnStart, 1, nil); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
		if ev.Time.IsZero() {
			t.Errorf("line %d: time is zero", i)
		}
	}
}

func TestTrajectoryWriter_TurnField(t *testing.T) {
	var buf bytes.Buffer
	tw := NewTrajectoryWriter(&buf)
	_ = tw.Emit(KindTurnStart, 7, nil)

	var ev Event
	if err := json.Unmarshal(buf.Bytes(), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.Turn != 7 {
		t.Errorf("expected turn=7, got %d", ev.Turn)
	}
}

func TestTrajectoryWriter_NoHTMLEscape(t *testing.T) {
	var buf bytes.Buffer
	tw := NewTrajectoryWriter(&buf)
	_ = tw.Emit(KindFeedbackSent, 1, "<sensor-results>&</sensor-results>")

	// json.Encoder with SetEscapeHTML(true) encodes < as < (6 bytes).
	// With escaping disabled it should appear as the literal < byte.
	jsonEscaped := []byte("\\u003c") // the 6-char sequence: \, u, 0, 0, 3, c
	if bytes.Contains(buf.Bytes(), jsonEscaped) {
		t.Error("HTML escaping should be disabled: found \\u003c in output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("<")) {
		t.Error("expected literal < in output when HTML escaping is disabled")
	}
}

func TestEventKindConstants(t *testing.T) {
	kinds := []EventKind{
		KindSessionStart, KindPlan, KindTurnStart, KindAssistantMessage,
		KindSensorRun, KindSensorResult, KindFeedbackSent,
		KindTurnApprovalRequired, KindStuckDetected, KindBudgetExceeded,
		KindConverged, KindSessionEnd,
	}
	seen := make(map[EventKind]bool)
	for _, k := range kinds {
		if k == "" {
			t.Error("empty EventKind constant")
		}
		if seen[k] {
			t.Errorf("duplicate EventKind: %q", k)
		}
		seen[k] = true
	}
}
