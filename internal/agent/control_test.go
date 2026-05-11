package agent

import (
	"strings"
	"testing"
	"time"
)

func TestControlReader_DecodesActions(t *testing.T) {
	input := strings.NewReader(
		`{"action":"approve_plan"}` + "\n" +
			`{"action":"replace_feedback","feedback":"try harder"}` + "\n" +
			`{"action":"interrupt"}` + "\n",
	)
	cr := NewControlReader(input)

	timeout := time.After(time.Second)
	want := []ControlAction{ActionApprovePlan, ActionReplaceFeedback, ActionInterrupt}
	for _, expected := range want {
		select {
		case msg, ok := <-cr.C():
			if !ok {
				t.Fatalf("channel closed before receiving %q", expected)
			}
			if msg.Action != expected {
				t.Errorf("expected %q, got %q", expected, msg.Action)
			}
		case <-timeout:
			t.Fatalf("timeout waiting for %q", expected)
		}
	}
}

func TestControlReader_SkipsMalformedLines(t *testing.T) {
	input := strings.NewReader(
		"not json\n" +
			`{"action":"approve_turn"}` + "\n" +
			"also not json\n",
	)
	cr := NewControlReader(input)

	timeout := time.After(time.Second)
	select {
	case msg, ok := <-cr.C():
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		if msg.Action != ActionApproveTurn {
			t.Errorf("expected approve_turn, got %q", msg.Action)
		}
	case <-timeout:
		t.Fatal("timeout")
	}
}

func TestControlReader_SkipsNoActionObjects(t *testing.T) {
	input := strings.NewReader(
		`{}` + "\n" +
			`{"other":"field"}` + "\n" +
			`{"action":"reject_plan"}` + "\n",
	)
	cr := NewControlReader(input)

	timeout := time.After(time.Second)
	select {
	case msg, ok := <-cr.C():
		if !ok {
			t.Fatal("channel closed before receiving reject_plan")
		}
		if msg.Action != ActionRejectPlan {
			t.Errorf("expected reject_plan, got %q", msg.Action)
		}
	case <-timeout:
		t.Fatal("timeout")
	}
}

func TestControlReader_ChannelClosedOnEOF(t *testing.T) {
	input := strings.NewReader(`{"action":"approve_plan"}` + "\n")
	cr := NewControlReader(input)

	// Drain first message.
	<-cr.C()

	// Channel should close after EOF.
	timeout := time.After(time.Second)
	select {
	case _, ok := <-cr.C():
		if ok {
			t.Error("expected channel to be closed")
		}
	case <-timeout:
		t.Error("timeout waiting for channel close")
	}
}

func TestControlReader_ReplaceFeedbackPayload(t *testing.T) {
	input := strings.NewReader(
		`{"action":"replace_feedback","feedback":"updated feedback text"}` + "\n",
	)
	cr := NewControlReader(input)
	msg := <-cr.C()
	if msg.Feedback != "updated feedback text" {
		t.Errorf("expected feedback payload, got %q", msg.Feedback)
	}
}
