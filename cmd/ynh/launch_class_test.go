package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every command main.go dispatches is classified as launching or not, and every classified command is
// dispatched: a command added without a row fails here (ynh #1006).
func TestEveryDispatchedCommandIsClassified(t *testing.T) {
	cmds := dispatchedCommands(t)
	if len(cmds) < 30 {
		t.Fatalf("read only %d commands from main.go's switch", len(cmds))
	}
	for _, c := range cmds {
		cl, ok := commandClasses[c]
		if !ok {
			t.Errorf("main.go dispatches %q but commandClasses has no row for it: say whether it launches, and why", c)
			continue
		}
		if cl.Why == "" {
			t.Errorf("%q: a row names its reason", c)
		}
	}
	for c := range commandClasses {
		if !slices.Contains(cmds, c) {
			t.Errorf("commandClasses has a row for %q, which main.go does not dispatch", c)
		}
	}
}

// ynhRun runs ynh itself (this test binary re-executed as main) with the isolated home the test
// set, and reports its exit code and standard error.
func ynhRun(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), execMainEnv+"=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, stdout.String() + stderr.String()
}

// Each launching command, run against a harness nobody accepted whose sensor, hook and MCP server
// each touch a marker file, exits 1 with the untrusted refusal and touches none (ynh #1006).
func TestEveryLaunchingCommandRefusesAnUnacceptedHarness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", filepath.Join(home, ".ynh"))
	markers := t.TempDir()
	touch := func(name string) string { return "touch " + filepath.Join(markers, name) }
	fields := `,"default_vendor":"claude",` +
		`"sensors":{"probe":{"category":"maintainability","source":{"command":` + strconv.Quote(touch("sensor")) + `},"output":{"format":"text"}}},` +
		`"hooks":{"on_stop":[{"command":` + strconv.Quote(touch("hook")) + `}]},` +
		`"mcp_servers":{"tool":{"command":"sh","args":["-c",` + strconv.Quote(touch("mcp")) + `]}}`
	hj := `{"name":"fx","version":"0.1.0"` + fields + `}`
	id := installGitHarness(t, "github.com/o/fx", "", "0123456789abcdef0123456789abcdef01234567", "fx", fields)
	folder := t.TempDir()
	if err := writePluginJSONFile(folder, []byte(hj)); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	// A checkpoint a resume can read, so --resume reaches the gate rather than failing on a missing one.
	resume := t.TempDir()
	writeAgentCheckpoint(t, resume, map[string]any{"backend": "claude", "phase": "plan"})

	// Every launching command in commandClasses has a case here: a new one fails below.
	cases := map[string][][]string{
		"run":      {{"run", id, "--", "hi"}},
		"agent":    {{"agent", "run", "--harness", id, "--task", "t", "--format", "json"}, {"agent", "run", "--harness", id, "--resume", resume, "--task", "t", "--format", "json"}},
		"check":    {{"check", id, "--cwd", work, "--format", "json"}},
		"sensors":  {{"sensors", "run", id, "probe", "--cwd", work, "--format", "json"}},
		"baseline": {{"baseline", id, "--explain", "--cwd", work, "--format", "json"}},
		"image":    {{"image", id, "--dry-run"}, {"image", "fxfrom", "--from", folder, "--dry-run"}},
	}
	for name, cl := range commandClasses {
		if cl.Launches && name != "delegate-run" && cases[name] == nil {
			t.Errorf("%q is classified as launching, but this test has no case that runs it against an unaccepted harness", name)
		}
	}
	for name, runs := range cases {
		if !commandClasses[name].Launches {
			t.Errorf("%q has a case here but is not classified as launching", name)
		}
		for _, args := range runs {
			code, out := ynhRun(t, args...)
			if code != 1 {
				t.Errorf("ynh %s: exit %d, want 1 (untrusted)\n%s", strings.Join(args, " "), code, out)
			}
			if !strings.Contains(out, "trust accept") && !strings.Contains(out, "--trust") {
				t.Errorf("ynh %s: not the untrusted refusal:\n%s", strings.Join(args, " "), out)
			}
		}
	}
	entries, _ := os.ReadDir(markers)
	if len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("an unaccepted harness ran something: %v", names)
	}
}

// A delegation's session is a ynh run of an installed delegate, so an unaccepted delegate stops the
// parent's run before its endpoint opens, and nothing the delegate declares runs.
func TestAnUnacceptedDelegateIsRefusedBeforeItRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", filepath.Join(home, ".ynh"))
	markers := t.TempDir()
	delegate := t.TempDir()
	dj := `{"name":"dg","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":` + strconv.Quote("touch "+filepath.Join(markers, "delegate")) + `}]}}`
	if err := writePluginJSONFile(delegate, []byte(dj)); err != nil {
		t.Fatal(err)
	}
	installLocalHarness(t, delegate, "dg")
	pid := installGitHarness(t, "github.com/o/pr", "", "0123456789abcdef0123456789abcdef01234567", "pr", `,"default_vendor":"claude","delegates_to":[{"git":`+strconv.Quote(delegate)+`}]`)
	acceptForTest(t, pid)

	code, out := ynhRun(t, "run", pid, "--", "hi")
	if code != 1 || !strings.Contains(out, "trust accept") {
		t.Errorf("exit %d, want 1 naming the acceptance:\n%s", code, out)
	}
	if entries, _ := os.ReadDir(markers); len(entries) > 0 {
		t.Error("the unaccepted delegate ran something")
	}
}
