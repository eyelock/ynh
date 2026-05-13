package agent

import (
	"strings"
	"testing"
)

func newControlReaderFromString(s string) *ControlReader {
	return NewControlReader(strings.NewReader(s))
}

func TestWaitForApproval_Approve(t *testing.T) {
	cr := newControlReaderFromString(`{"action":"approve_turn"}` + "\n")
	got, fb, aborted := waitForApproval(cr, ActionApproveTurn, ActionRejectPlan)
	if got != ActionApproveTurn {
		t.Errorf("action = %q, want %q", got, ActionApproveTurn)
	}
	if fb != "" {
		t.Errorf("feedback = %q, want empty", fb)
	}
	if aborted {
		t.Error("should not be aborted")
	}
}

func TestWaitForApproval_ReplaceFeedback(t *testing.T) {
	cr := newControlReaderFromString(`{"action":"replace_feedback","feedback":"do x"}` + "\n")
	got, fb, aborted := waitForApproval(cr, ActionApproveTurn, ActionRejectPlan)
	if got != ActionApproveTurn {
		t.Errorf("replace_feedback should yield approve, got %q", got)
	}
	if fb != "do x" {
		t.Errorf("feedback = %q, want do x", fb)
	}
	if aborted {
		t.Error("should not be aborted")
	}
}

func TestWaitForApproval_Reject(t *testing.T) {
	cr := newControlReaderFromString(`{"action":"reject_plan"}` + "\n")
	got, _, aborted := waitForApproval(cr, ActionApprovePlan, ActionRejectPlan)
	if got != ActionRejectPlan {
		t.Errorf("action = %q, want %q", got, ActionRejectPlan)
	}
	if aborted {
		t.Error("reject is not abort")
	}
}

func TestWaitForApproval_Interrupt(t *testing.T) {
	cr := newControlReaderFromString(`{"action":"interrupt"}` + "\n")
	got, _, aborted := waitForApproval(cr, ActionApproveTurn, ActionRejectPlan)
	if got != ActionInterrupt {
		t.Errorf("action = %q, want interrupt", got)
	}
	if !aborted {
		t.Error("interrupt should be aborted=true")
	}
}

func TestWaitForApproval_ChannelClosed(t *testing.T) {
	cr := newControlReaderFromString("") // EOF immediately
	got, _, aborted := waitForApproval(cr, ActionApproveTurn, ActionRejectPlan)
	if got != ActionInterrupt {
		t.Errorf("closed channel should yield interrupt, got %q", got)
	}
	if !aborted {
		t.Error("EOF should be aborted=true")
	}
}

func TestWaitForApproval_IgnoresUnrelated(t *testing.T) {
	// Send an unrelated approve_plan first, then the expected approve_turn.
	// The unrelated action falls through the switch and the loop continues.
	cr := newControlReaderFromString(`{"action":"approve_plan"}` + "\n" + `{"action":"approve_turn"}` + "\n")
	got, _, _ := waitForApproval(cr, ActionApproveTurn, ActionRejectPlan)
	if got != ActionApproveTurn {
		t.Errorf("expected to skip unrelated action, got %q", got)
	}
}
