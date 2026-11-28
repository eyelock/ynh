package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// ynw-q0lml: auto_approve on a native delegates_to entry is refused wherever a
// harness is loaded, with the one message: agent run, trust accept and show,
// check, update, info and image, not only install and run.
func TestNativeEntryAutoApproveIsRefusedOnEveryPath(t *testing.T) {
	fakeHome(t)
	dir := t.TempDir()
	body := `{"name":"nat","version":"0.1.0","default_vendor":"claude","delegates_to":[{"git":"github.com/o/a","ref":"` + sha1 + `","transport":"native","auto_approve":"edits"}]}`
	if err := writePluginJSONFile(dir, []byte(body)); err != nil {
		t.Fatal(err)
	}
	id := installLocalHarness(t, dir, "nat")

	const want = "auto_approve is not accepted on a native delegate"
	// agent run reports a refusal as a structured error on stdout.
	agentRun := func(harnessRef string, extra ...string) func() error {
		return func() error {
			var out, errb bytes.Buffer
			args := append([]string{"--format", "json", "--worktree", t.TempDir(), "--task", "t", "--no-plan", "--harness", harnessRef}, extra...)
			err := cmdAgentRun(args, &out, &errb, strings.NewReader(""))
			if err == nil {
				return nil
			}
			return errors.New(err.Error() + out.String() + errb.String())
		}
	}
	paths := map[string]func() error{
		"agent run folder":    agentRun(dir, "--trust"),
		"agent run installed": agentRun(id),
		"trust accept":        func() error { return cmdTrustAccept([]string{id}, io.Discard, io.Discard) },
		"trust show":          func() error { return cmdTrustShow([]string{id}, io.Discard, io.Discard) },
		"check":               func() error { return cmdCheck([]string{dir}, io.Discard, io.Discard) },
		"info":                func() error { return cmdInfoTo([]string{id}, io.Discard, io.Discard) },
		"update":              func() error { return cmdUpdate([]string{id}) },
		"image --from": func() error {
			var out bytes.Buffer
			return cmdImageTo([]string{"nat", "--from", dir, "--trust", "--dry-run"}, &out, io.Discard)
		},
		"image installed": func() error {
			var out bytes.Buffer
			return cmdImageTo([]string{id, "--dry-run"}, &out, io.Discard)
		},
	}
	for name, run := range paths {
		t.Run(name, func(t *testing.T) {
			err := run()
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("want a refusal containing %q, got %v", want, err)
			}
		})
	}
	if store, _ := loadTrustStore(); len(store.Harnesses) != 0 {
		t.Errorf("nothing may be accepted: %v", store.Harnesses)
	}
}
