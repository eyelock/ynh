package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeYnh writes an executable stub that prints body on stdout, err on stderr,
// and exits with code. It also records the arguments it was called with.
func fakeYnh(t *testing.T, body, errBody string, code int) (path, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	dir := t.TempDir()
	path = filepath.Join(dir, "ynh")
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s' \"$*\" > " + argsFile + "\n"
	if body != "" {
		script += "cat <<'JSONEOF'\n" + body + "\nJSONEOF\n"
	}
	if errBody != "" {
		script += "echo '" + errBody + "' >&2\n"
	}
	script += "exit " + itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	return path, argsFile
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

const blockedEnvelope = `{
  "capabilities": "0.8.0",
  "harness": "demo",
  "verdict": "blocked",
  "summary": {"total": 1, "failed": 1, "blocking": 1},
  "sensors": [{"name":"lint","kind":"command","tolerance":"blocking","status":"fail","exit_code":1}]
}`

// Exit 1 means blocking sensors failed and the report is on stdout — the
// ordinary outcome of a mid-loop turn. Treating it as an error would leave
// the loop unable to tell a failing test from a broken harness, and it would
// end every run that had work left to do.
func TestRunCheck_BlockedIsNotAnError(t *testing.T) {
	bin, _ := fakeYnh(t, blockedEnvelope, "", 1)

	env, err := defaultRunCheck(bin, "demo", "", nil, nil)
	if err != nil {
		t.Fatalf("exit 1 is a verdict, not a failure: %v", err)
	}
	if env.Verdict != "blocked" {
		t.Errorf("verdict = %q, want blocked", env.Verdict)
	}
	if len(env.Sensors) != 1 || env.Sensors[0].Name != "lint" {
		t.Errorf("sensors not parsed: %+v", env.Sensors)
	}
}

// Exit 2 means the gate itself could not run. It must surface as an error
// carrying what ynh said, or an operator sees an unproductive run rather than
// a broken harness.
func TestRunCheck_ExecFailureIsAnError(t *testing.T) {
	bin, _ := fakeYnh(t, "", `harness "demo" not installed`, 2)

	_, err := defaultRunCheck(bin, "demo", "", nil, nil)
	if err == nil {
		t.Fatal("exit 2 means the gate is broken and must not be silently absorbed")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error must carry what ynh reported, got %q", err)
	}
}

func TestRunCheck_PassParses(t *testing.T) {
	bin, _ := fakeYnh(t, `{"verdict":"pass","harness":"demo","sensors":[]}`, "", 0)

	env, err := defaultRunCheck(bin, "demo", "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Verdict != "pass" {
		t.Errorf("verdict = %q, want pass", env.Verdict)
	}
}

// The loop names the sensors it wants. Without --only, `ynh check` would also
// run the convergence-verifier and stuck-recovery sensors the loop
// deliberately excludes — and the verifier's verdict would then gate the very
// turn that is meant to consult it separately.
func TestRunCheck_PassesOnlyAndOverlay(t *testing.T) {
	bin, argsFile := fakeYnh(t, `{"verdict":"pass","sensors":[]}`, "", 0)

	_, err := defaultRunCheck(bin, "demo", "/work", []string{"lint", "test"},
		map[string]json.RawMessage{"lint": json.RawMessage(`{"source":{"command":"make fast"}}`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatalf("reading recorded args: %v", readErr)
	}
	args := string(got)
	for _, want := range []string{"check demo", "--format json", "--cwd /work", "--only lint,test", "make fast"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}
}
