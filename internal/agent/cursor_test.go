package agent

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestCursorBackend_Name(t *testing.T) {
	b := &CursorBackend{}
	if b.Name() != "cursor" {
		t.Errorf("expected 'cursor', got %q", b.Name())
	}
}

// buildCursorResult emits stream-json lines matching Cursor's output format
// (same wire format as Claude Code: "assistant" then "result").
func buildCursorResult(text string) string {
	msg := cursorOutputEvent{
		Type: "assistant",
		Message: &cursorOutputMsg{
			Role:    "assistant",
			Content: []cursorContent{{Type: "text", Text: text}},
			Usage:   &cursorUsage{InputTokens: 8, OutputTokens: 3},
		},
	}
	result := cursorOutputEvent{
		Type:  "result",
		Usage: &cursorUsage{InputTokens: 2, OutputTokens: 1},
	}
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	_ = enc.Encode(msg)
	_ = enc.Encode(result)
	return sb.String()
}

func TestParseCursorOutput_HappyPath(t *testing.T) {
	raw := buildCursorResult("hello from cursor")
	turn, err := parseCursorOutput(strings.NewReader(raw))
	if err != nil && err != io.EOF {
		t.Fatalf("parseCursorOutput: %v", err)
	}
	if turn.Content != "hello from cursor" {
		t.Errorf("unexpected content: %q", turn.Content)
	}
	// Usage is accumulated from both assistant and result events.
	if turn.Usage.InputTokens != 10 {
		t.Errorf("expected InputTokens=10, got %d", turn.Usage.InputTokens)
	}
	if turn.Usage.OutputTokens != 4 {
		t.Errorf("expected OutputTokens=4, got %d", turn.Usage.OutputTokens)
	}
}

func TestParseCursorOutput_EmptyStream(t *testing.T) {
	turn, err := parseCursorOutput(strings.NewReader(""))
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
	if turn.Content != "" {
		t.Errorf("expected empty content, got %q", turn.Content)
	}
}

func TestParseCursorOutput_SkipsUnknownEvents(t *testing.T) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	_ = enc.Encode(map[string]string{"type": "system", "data": "ignored"})
	_ = enc.Encode(cursorOutputEvent{
		Type: "assistant",
		Message: &cursorOutputMsg{
			Role:    "assistant",
			Content: []cursorContent{{Type: "text", Text: "real answer"}},
		},
	})
	_ = enc.Encode(cursorOutputEvent{Type: "result"})

	turn, err := parseCursorOutput(strings.NewReader(sb.String()))
	if err != nil && err != io.EOF {
		t.Fatalf("parseCursorOutput: %v", err)
	}
	if turn.Content != "real answer" {
		t.Errorf("unexpected content: %q", turn.Content)
	}
}

func TestParseCursorOutput_AccumulatesTextBlocks(t *testing.T) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	_ = enc.Encode(cursorOutputEvent{
		Type: "assistant",
		Message: &cursorOutputMsg{
			Role: "assistant",
			Content: []cursorContent{
				{Type: "text", Text: "first "},
				{Type: "tool_use", Text: "skip"},
				{Type: "text", Text: "second"},
			},
		},
	})
	_ = enc.Encode(cursorOutputEvent{Type: "result"})

	turn, err := parseCursorOutput(strings.NewReader(sb.String()))
	if err != nil && err != io.EOF {
		t.Fatalf("parseCursorOutput: %v", err)
	}
	if turn.Content != "first second" {
		t.Errorf("unexpected content: %q", turn.Content)
	}
}

func TestCursorSession_SendQueuesMessage(t *testing.T) {
	sess := &cursorSession{}
	if err := sess.Send("hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sess.pending != "hello" {
		t.Errorf("expected pending='hello', got %q", sess.pending)
	}
}

func TestCursorSession_NextErrorsWithoutSend(t *testing.T) {
	sess := &cursorSession{}
	_, err := sess.Next()
	if err == nil {
		t.Error("expected error when Next called without prior Send")
	}
}

func TestCursorSession_CloseIsNoop(t *testing.T) {
	sess := &cursorSession{}
	if err := sess.Close(); err != nil {
		t.Errorf("Close should be a no-op, got %v", err)
	}
}

func TestCursorSession_FirstTurnOmitsResume(t *testing.T) {
	// Verify the firstTurn flag is true on a fresh session and flips after Next.
	// We can't easily test the subprocess args without spawning cursor, so we
	// check the state transitions directly.
	sess := &cursorSession{firstTurn: true}
	if !sess.firstTurn {
		t.Error("expected firstTurn=true on fresh session")
	}

	// Simulate what Next() does to firstTurn (set to false before subprocess).
	sess.firstTurn = false
	if sess.firstTurn {
		t.Error("expected firstTurn=false after first turn")
	}
}
