package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const (
	evHookOK  = `{"type":"system","subtype":"hook_response","hook_event":"UserPromptSubmit","outcome":"success","exit_code":0}`
	evHookBad = `{"type":"system","subtype":"hook_response","hook_event":"UserPromptSubmit","outcome":"error","exit_code":2}`
	evInit    = `{"type":"system","subtype":"init","permissionMode":"acceptEdits"}`
	evTool    = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Write","input":{}}]}}`
	evText    = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`
	evResult  = `{"type":"result","subtype":"success","is_error":false,"result":"the answer"}`
)

func runStream(t *testing.T, expected string, events ...string) (out string, killed bool, err error) {
	t.Helper()
	var buf bytes.Buffer
	err = claudeStreamFilter(expected, &buf)(strings.NewReader(strings.Join(events, "\n")+"\n"), func() { killed = true })
	return buf.String(), killed, err
}

// A normal run reads as text: the answer, as `claude -p` prints it.
func TestClaudeStream_NormalRunRendersTheAnswer(t *testing.T) {
	out, killed, err := runStream(t, "acceptEdits", evHookOK, evInit, evText, evTool, evResult)
	if err != nil || killed || out != "the answer\n" {
		t.Errorf("out %q killed %v err %v", out, killed, err)
	}
}

// With no UserPromptSubmit hook response (hooks switched off) the session is
// refused before its first tool runs, and prints no answer.
func TestClaudeStream_NoHookResponseIsRefusedBeforeAnyTool(t *testing.T) {
	out, killed, err := runStream(t, "acceptEdits", evInit, evTool, evResult)
	if !errors.Is(err, errNoHookResponse) || !killed || out != "" {
		t.Errorf("out %q killed %v err %v", out, killed, err)
	}
	// A hook that did not succeed counts as no response.
	if _, killed, err := runStream(t, "acceptEdits", evHookBad, evInit, evTool); !errors.Is(err, errNoHookResponse) || !killed {
		t.Errorf("a failed hook: killed %v err %v", killed, err)
	}
	// A session that finishes without using a tool is refused too: the check never ran.
	out, killed, err = runStream(t, "acceptEdits", evInit, evText, evResult)
	if !errors.Is(err, errNoHookResponse) || out != "" {
		t.Errorf("no tools: out %q killed %v err %v", out, killed, err)
	}
}

// An init that reports another mode is refused and the session killed.
func TestClaudeStream_MismatchedInitIsRefused(t *testing.T) {
	out, killed, err := runStream(t, "plan", evHookOK, evInit, evTool, evResult)
	if err == nil || !strings.Contains(err.Error(), `started in "acceptEdits"`) || !killed || out != "" {
		t.Errorf("out %q killed %v err %v", out, killed, err)
	}
}

func TestWithClaudeStream(t *testing.T) {
	got, err := withClaudeStream([]string{"-p", "hi"})
	if err != nil || strings.Join(got, " ") != "--output-format stream-json --verbose --include-hook-events -p hi" {
		t.Errorf("%q %v", got, err)
	}
	for _, own := range []string{"--output-format", "--output-format=json", "--input-format=stream-json"} {
		if _, err := withClaudeStream([]string{own}); err == nil || !strings.Contains(err.Error(), "refusing to run") {
			t.Errorf("%s accepted", own)
		}
	}
}

// A plain question-and-answer run that had a successful UserPromptSubmit hook
// response but used no tool is not refused: only a run with no successful hook
// response is.
func TestClaudeStream_AnswerWithoutAToolIsRenderedWhenTheHookRan(t *testing.T) {
	out, killed, err := runStream(t, "acceptEdits", evHookOK, evInit, evText, evResult)
	if err != nil || killed || out != "the answer\n" {
		t.Errorf("out %q killed %v err %v", out, killed, err)
	}
}
