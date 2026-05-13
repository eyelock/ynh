package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestCodexBackend_Name(t *testing.T) {
	b := &CodexBackend{}
	if b.Name() != "codex" {
		t.Errorf("expected 'codex', got %q", b.Name())
	}
}

// buildCodexResult emits a sequence of NDJSON lines mimicking the codex
// exec --json wire format: one "message" event followed by a "result" event.
func buildCodexResult(text string) string {
	msg := codexOutputEvent{
		Type: "message",
		Message: &codexMsg{
			Role:    "assistant",
			Content: []codexContent{{Type: "text", Text: text}},
		},
	}
	result := codexOutputEvent{
		Type:  "result",
		Usage: &codexUsage{InputTokens: 10, OutputTokens: 5},
	}
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	_ = enc.Encode(msg)
	_ = enc.Encode(result)
	return sb.String()
}

func TestCodexSession_NextReturnsContent(t *testing.T) {
	raw := buildCodexResult("hello from codex")
	scanner := bufio.NewScanner(strings.NewReader(raw))

	sess := &codexSession{
		scanner: scanner,
		stdin:   &nopWriteCloser{new(bytes.Buffer)},
	}
	turn, err := sess.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if turn.Content != "hello from codex" {
		t.Errorf("unexpected content: %q", turn.Content)
	}
	if turn.Usage.InputTokens != 10 || turn.Usage.OutputTokens != 5 {
		t.Errorf("unexpected usage: %+v", turn.Usage)
	}
}

func TestCodexSession_NextEOFOnEmptyStream(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	sess := &codexSession{
		scanner: scanner,
		stdin:   &nopWriteCloser{new(bytes.Buffer)},
	}
	_, err := sess.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestCodexSession_NextSkipsUnknownEvents(t *testing.T) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	_ = enc.Encode(map[string]string{"type": "thinking", "content": "pondering"})
	_ = enc.Encode(codexOutputEvent{
		Type: "message",
		Message: &codexMsg{
			Role:    "assistant",
			Content: []codexContent{{Type: "text", Text: "answer"}},
		},
	})
	_ = enc.Encode(codexOutputEvent{Type: "result"})

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))
	sess := &codexSession{
		scanner: scanner,
		stdin:   &nopWriteCloser{new(bytes.Buffer)},
	}
	turn, err := sess.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if turn.Content != "answer" {
		t.Errorf("unexpected content: %q", turn.Content)
	}
}

func TestCodexSession_SendWritesJSON(t *testing.T) {
	var buf bytes.Buffer
	sess := &codexSession{
		stdin: &nopWriteCloser{&buf},
	}
	if err := sess.Send("do the thing"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var msg codexUserMsg
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Type != "user" {
		t.Errorf("expected type 'user', got %q", msg.Type)
	}
	if msg.Content != "do the thing" {
		t.Errorf("unexpected content: %q", msg.Content)
	}
}

func TestCodexSession_NextAccumulatesMultipleMessageBlocks(t *testing.T) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	_ = enc.Encode(codexOutputEvent{
		Type: "message",
		Message: &codexMsg{
			Role: "assistant",
			Content: []codexContent{
				{Type: "text", Text: "part one "},
				{Type: "tool_use", Text: "ignored"},
				{Type: "text", Text: "part two"},
			},
		},
	})
	_ = enc.Encode(codexOutputEvent{Type: "result"})

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))
	sess := &codexSession{
		scanner: scanner,
		stdin:   &nopWriteCloser{new(bytes.Buffer)},
	}
	turn, err := sess.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if turn.Content != "part one part two" {
		t.Errorf("unexpected content: %q", turn.Content)
	}
}

// nopWriteCloser wraps a bytes.Buffer so it satisfies io.WriteCloser.
type nopWriteCloser struct{ *bytes.Buffer }

func (n *nopWriteCloser) Close() error { return nil }
