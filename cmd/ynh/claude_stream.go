package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/permission"
)

// A one-shot Claude Code run is read as a stream (--output-format stream-json
// --verbose --include-hook-events) so that ynh itself can see that the
// opened-mode check ran: with hooks switched off (disableAllHooks, or by
// policy) a hook never runs and a hook-only check would pass silently. ynh
// requires a successful UserPromptSubmit hook response before the first tool
// use, checks the mode init reports, and renders the answer itself, so normal
// output still reads as text (ynh #1058, ynw-yy4id).

// claudeStreamArgs are the flags that put a one-shot run on the stream.
var claudeStreamArgs = []string{"--output-format", "stream-json", "--verbose", "--include-hook-events"}

// withClaudeStream returns vendorArgs with the stream flags added. A caller
// who passed its own --output-format is refused: ynh needs the stream.
func withClaudeStream(vendorArgs []string) ([]string, error) {
	for _, a := range vendorArgs {
		if a == "--output-format" || strings.HasPrefix(a, "--output-format=") || a == "--input-format" || strings.HasPrefix(a, "--input-format=") {
			return nil, fmt.Errorf("refusing to run: %s is passed, but ynh reads a one-shot Claude Code run as a stream to check the mode it opened in and renders the answer itself; remove it", a)
		}
	}
	return append(slices.Clone(claudeStreamArgs), vendorArgs...), nil
}

// errNoHookResponse is why a session was refused: the opened-mode hook did not
// run, so nothing checked the mode it opened in.
var errNoHookResponse = errors.New("the opened-mode check did not run: no successful UserPromptSubmit hook response was seen before the session used a tool or finished (hooks may be switched off by disableAllHooks or by policy), so the session was stopped")

// claudeStreamEvent is the part of a stream event ynh reads.
type claudeStreamEvent struct {
	Type           string `json:"type"`
	Subtype        string `json:"subtype"`
	HookEvent      string `json:"hook_event"`
	Outcome        string `json:"outcome"`
	PermissionMode string `json:"permissionMode"`
	IsError        bool   `json:"is_error"`
	Result         string `json:"result"`
	Message        struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// usesTool reports whether an assistant event asks to run a tool.
func (e claudeStreamEvent) usesTool() bool {
	if e.Type != "assistant" {
		return false
	}
	var blocks []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

// claudeStreamFilter reads a one-shot Claude Code session's stream and writes
// its answer to out as text, as `claude -p` does. It stops the session (kill)
// and returns an error when the session's init reports a mode other than
// expected, or when a tool is about to be used, or the session ends, without a
// successful UserPromptSubmit hook response. The answer is written only once
// the session has been verified, so a refused session prints none.
func claudeStreamFilter(expected string, out io.Writer) func(io.Reader, func()) error {
	return func(r io.Reader, kill func()) error {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		hookOK := false
		var answer *claudeStreamEvent
		for sc.Scan() {
			line := sc.Bytes()
			var ev claudeStreamEvent
			if json.Unmarshal(line, &ev) != nil {
				// Not an event: pass it through, so nothing is lost.
				_, _ = fmt.Fprintln(out, string(line))
				continue
			}
			switch {
			case ev.Type == "system" && ev.Subtype == "hook_response" && ev.HookEvent == "UserPromptSubmit" && ev.Outcome == "success":
				hookOK = true
			case ev.Type == "system" && ev.Subtype == "init":
				if err := permission.OpenedMismatch(expected, ev.PermissionMode); err != nil {
					kill()
					return err
				}
			case ev.usesTool():
				if !hookOK {
					kill()
					return errNoHookResponse
				}
			case ev.Type == "result":
				e := ev
				answer = &e
			}
		}
		if err := sc.Err(); err != nil {
			return err
		}
		if answer == nil {
			return nil // no answer to render; the child's own exit status stands
		}
		if !hookOK {
			return errNoHookResponse
		}
		_, err := fmt.Fprintln(out, answer.Result)
		return err
	}
}
