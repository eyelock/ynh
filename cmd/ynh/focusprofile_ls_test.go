package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const lsHarnessJSON = `{
  "name": "lsh",
  "version": "0.1.0",
  "default_vendor": "claude",
  "focuses": {
    "review":  {"prompt": "Review the diff.\nSecond line is not shown in text.", "profile": "strict"},
    "release": {"prompt": "Draft a changelog."}
  },
  "profiles": {
    "strict": {
      "hooks": {"on_stop": [{"command": "a"}, {"command": "b"}]},
      "env_passthrough": ["HOME", "PATH"]
    },
    "quiet": {}
  }
}`

func lsHarness(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "lsh", lsHarnessJSON)
}

func TestFocusLs_Text(t *testing.T) {
	lsHarness(t)
	var out bytes.Buffer
	if err := cmdFocusTo([]string{"ls", "local/lsh"}, &out); err != nil {
		t.Fatalf("focus ls: %v", err)
	}
	got := out.String()
	for _, want := range []string{"NAME", "review", "release", "strict"} {
		if !strings.Contains(got, want) {
			t.Errorf("focus ls text missing %q:\n%s", want, got)
		}
	}
	// The text view is for reading: a multi-line prompt collapses to one line.
	if strings.Contains(got, "Second line") {
		t.Errorf("text view should show only the first line of a prompt:\n%s", got)
	}
}

// JSON carries the prompt whole. A loop driver choosing a focus needs the text
// it will actually send, so truncating here would force a second call.
func TestFocusLs_JSONCarriesTheWholePrompt(t *testing.T) {
	lsHarness(t)
	var out bytes.Buffer
	if err := cmdFocusTo([]string{"ls", "local/lsh", "--format", "json"}, &out); err != nil {
		t.Fatalf("focus ls --format json: %v", err)
	}
	var entries []focusListEntry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatalf("decoding: %v\n%s", err, out.String())
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 focuses, got %d", len(entries))
	}
	// Sorted by name: release, review.
	if entries[0].Name != "release" || entries[1].Name != "review" {
		t.Errorf("focuses are not sorted by name: %+v", entries)
	}
	if !strings.Contains(entries[1].Prompt, "Second line") {
		t.Errorf("JSON truncated the prompt: %q", entries[1].Prompt)
	}
	if entries[1].Profile != "strict" {
		t.Errorf("profile lost: %+v", entries[1])
	}
	if entries[0].Profile != "" {
		t.Errorf("a focus with no profile should omit it: %+v", entries[0])
	}
}

func TestProfileLs_CountsWhatEachTouches(t *testing.T) {
	lsHarness(t)
	var out bytes.Buffer
	if err := cmdProfileTo([]string{"ls", "local/lsh", "--format", "json"}, &out); err != nil {
		t.Fatalf("profile ls: %v", err)
	}
	var entries []profileListEntry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatalf("decoding: %v\n%s", err, out.String())
	}
	byName := map[string]profileListEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	strict, ok := byName["strict"]
	if !ok {
		t.Fatalf("strict profile missing: %+v", entries)
	}
	// Hooks are counted across events, not per event.
	if strict.Hooks != 2 {
		t.Errorf("hooks = %d, want 2 (both on_stop entries)", strict.Hooks)
	}
	if strict.EnvPassthrough != 2 {
		t.Errorf("env_passthrough = %d, want 2", strict.EnvPassthrough)
	}
	if quiet := byName["quiet"]; quiet.Hooks != 0 || quiet.MCPServers != 0 {
		t.Errorf("an empty profile should count zero: %+v", quiet)
	}
}

// A harness with none of either says so rather than printing a bare header.
func TestFocusProfileLs_EmptyIsExplicit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "bare",
		`{"name":"bare","version":"0.1.0","default_vendor":"claude"}`)

	var f, p bytes.Buffer
	if err := cmdFocusTo([]string{"ls", "local/bare"}, &f); err != nil {
		t.Fatal(err)
	}
	if err := cmdProfileTo([]string{"ls", "local/bare"}, &p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.String(), "no focuses") {
		t.Errorf("expected an explicit empty message, got %q", f.String())
	}
	if !strings.Contains(p.String(), "no profiles") {
		t.Errorf("expected an explicit empty message, got %q", p.String())
	}
}

// The usage line must advertise the verb that now exists.
func TestFocusProfile_UsageMentionsLs(t *testing.T) {
	var out bytes.Buffer
	fErr := cmdFocusTo(nil, &out)
	pErr := cmdProfileTo(nil, &out)
	if fErr == nil || !strings.Contains(fErr.Error(), "ls") {
		t.Errorf("focus usage does not mention ls: %v", fErr)
	}
	if pErr == nil || !strings.Contains(pErr.Error(), "ls") {
		t.Errorf("profile usage does not mention ls: %v", pErr)
	}
}
