package agent

import (
	"bufio"
	"encoding/json"
	"io"
)

// ControlAction identifies an action sent on the control channel.
type ControlAction string

const (
	ActionApprovePlan     ControlAction = "approve_plan"
	ActionRejectPlan      ControlAction = "reject_plan"
	ActionInterrupt       ControlAction = "interrupt"
	ActionApproveTurn     ControlAction = "approve_turn"
	ActionReplaceFeedback ControlAction = "replace_feedback"
)

// ControlMessage is a single NDJSON message from the control channel.
type ControlMessage struct {
	Action   ControlAction `json:"action"`
	Feedback string        `json:"feedback,omitempty"` // used with replace_feedback
}

// ControlReader reads NDJSON control messages from a stream.
// Unknown actions are silently ignored for forward-compatibility.
type ControlReader struct {
	ch chan ControlMessage
}

// NewControlReader starts a background goroutine reading NDJSON from r
// and returns a ControlReader whose channel receives decoded messages.
// The channel is closed when r reaches EOF or an unrecoverable read error.
func NewControlReader(r io.Reader) *ControlReader {
	cr := &ControlReader{ch: make(chan ControlMessage, 16)}
	go cr.read(r)
	return cr
}

// C returns the channel of incoming control messages.
func (cr *ControlReader) C() <-chan ControlMessage {
	return cr.ch
}

func (cr *ControlReader) read(r io.Reader) {
	defer close(cr.ch)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg ControlMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // ignore malformed lines
		}
		if msg.Action == "" {
			continue // ignore objects with no action
		}
		cr.ch <- msg
	}
}
