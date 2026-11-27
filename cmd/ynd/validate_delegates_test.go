package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// delegateHarnessRepo makes a git repo holding a delegate harness at
// harnesses/probe, reached from the repo's root as {"git": ".", "path":
// "harnesses/probe"}. It has a profile "ci" and focuses "triage" (on ci) and
// "orphan" (on a profile it does not define). It returns the repo's root.
func delegateHarnessRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "root")
	mkdirAll(t, repo)
	writeFile(t, filepath.Join(repo, "harnesses", "probe", ".agents/harness", "plugin.json"), []byte(`{"name":"probe","version":"0.1.0",
		"profiles":{"ci":{}},
		"focuses":{"triage":{"profile":"ci","prompt":"Triage."},"orphan":{"profile":"gone","prompt":"x"}}}`))
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}, {"add", "."}, {"commit", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

// A delegates_to entry may name one of the delegate's own profiles or
// focuses, not both, and not one it does not define. A delegate that cannot
// be read is a note, not a failure.
func TestValidateHarnessDelegates(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	repo := delegateHarnessRepo(t)
	const src = `"git":".","path":"harnesses/probe"`
	tests := []struct {
		name      string
		entry     string
		wantIssue string // "" for none
		wantNote  string // "" for none
	}{
		{name: "profile", entry: `{` + src + `,"profile":"ci"}`},
		{name: "focus", entry: `{` + src + `,"focus":"triage"}`},
		{name: "neither", entry: `{` + src + `}`},
		{name: "both", entry: `{` + src + `,"profile":"ci","focus":"triage"}`, wantIssue: `names both profile "ci" and focus "triage"; a focus already carries its profile`},
		{name: "unknown profile", entry: `{` + src + `,"profile":"nope"}`, wantIssue: `references profile "nope", which delegate probe does not define (it declares: ci)`},
		{name: "unknown focus", entry: `{` + src + `,"focus":"nope"}`, wantIssue: `references focus "nope", which delegate probe does not define (it declares: orphan, triage)`},
		{name: "focus on an undefined profile", entry: `{` + src + `,"focus":"orphan"}`, wantIssue: `focus "orphan" of delegate probe names profile "gone", which it does not define`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(`{"name":"root","version":"0.1.0","delegates_to":[` + tt.entry + `]}`)
			issues, notes := validateHarnessDelegates(repo, data)
			checkOne(t, "issue", issues, tt.wantIssue)
			checkOne(t, "note", notes, tt.wantNote)
		})
	}

	// Not cached and not local: a note, never an error.
	data := []byte(`{"name":"root","version":"0.1.0","delegates_to":[{"git":"github.com/example/absent","focus":"triage"}]}`)
	issues, notes := validateHarnessDelegates(t.TempDir(), data)
	checkOne(t, "issue", issues, "")
	checkOne(t, "note", notes, "delegates_to[0] (github.com/example/absent): selection not checked")
}

func checkOne(t *testing.T, kind string, got []string, want string) {
	t.Helper()
	if want == "" {
		if len(got) != 0 {
			t.Errorf("%ss = %q, want none", kind, got)
		}
		return
	}
	if len(got) != 1 || !strings.Contains(got[0], want) {
		t.Errorf("%ss = %q, want one containing %q", kind, got, want)
	}
}

// The schema refuses an entry naming both, and an unknown key still.
func TestValidateHarness_DelegateBothFailsValidation(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	hr := delegateHarnessRepo(t)
	t.Chdir(filepath.Dir(hr))
	const head = `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"root","version":"0.1.0","delegates_to":[{"git":".","path":"harnesses/probe",`
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"), []byte(head+`"profile":"ci","focus":"triage"}]}`))
	if err := validateHarness(hr); err == nil {
		t.Fatal("naming both a profile and a focus must fail validation")
	}
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"), []byte(head+`"focus":"triage"}]}`))
	if err := validateHarness(hr); err != nil {
		t.Fatalf("a valid focus must pass: %v", err)
	}
}

// A delegate set to transport: native on a vendor with no native subagents
// fails validation with the fix; the schema takes the setting on an entry and
// on the harness, and refuses any other value.
func TestValidateHarnessTransport(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	tests := []struct {
		name, manifest, wantIssue string
	}{
		{"neither: a2a", `{"name":"h","version":"0.1.0","default_vendor":"cursor","delegates_to":[{"git":"github.com/o/a"}]}`, ""},
		{"native on claude", `{"name":"h","version":"0.1.0","default_vendor":"claude","delegates_to":[{"git":"github.com/o/a","transport":"native"}]}`, ""},
		{"entry native on cursor", `{"name":"h","version":"0.1.0","default_vendor":"cursor","delegates_to":[{"git":"github.com/o/a","transport":"native"}]}`,
			"Fix: set transport: a2a on this delegate, or run on a vendor with subagents"},
		{"harness default native on cursor", `{"name":"h","version":"0.1.0","default_vendor":"cursor","transport":"native","delegates_to":[{"git":"github.com/o/a"}]}`,
			"Fix: set transport: a2a on this delegate"},
		{"mix on cursor, the a2a entry alone is fine", `{"name":"h","version":"0.1.0","default_vendor":"cursor","transport":"native","delegates_to":[{"git":"github.com/o/a","transport":"a2a"}]}`, ""},
		{"unknown value", `{"name":"h","version":"0.1.0","default_vendor":"claude","delegates_to":[{"git":"github.com/o/a","transport":"pigeon"}]}`, "transport"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(tc.manifest))
			var err error
			var buf bytes.Buffer
			withStdout(t, &buf, func() { err = validateHarness(dir) })
			got := buf.String()
			if err != nil {
				got += err.Error()
			}
			if tc.wantIssue == "" {
				if strings.Contains(got, "transport") {
					t.Errorf("no transport issue expected:\n%s", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantIssue) {
				t.Errorf("want %q in:\n%s", tc.wantIssue, got)
			}
		})
	}
}
