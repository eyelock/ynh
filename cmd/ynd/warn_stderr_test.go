package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStreams runs f and returns what it wrote to stdout and stderr.
func captureStreams(t *testing.T, f func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	re, we, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wo, we
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	f()
	_ = wo.Close()
	_ = we.Close()
	o, _ := io.ReadAll(ro)
	e, _ := io.ReadAll(re)
	return string(o), string(e)
}

const cursorMatcherManifest = `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"w","version":"0.1.0","default_vendor":"cursor",` +
	`"hooks":{"before_tool":[{"matcher":"Bash","command":"x"}]}}`

// The fix of a hook warning reaches the caller, not only its message.
func TestHookVendorWarnings_CarryTheFix(t *testing.T) {
	var hj map[string]any
	if err := json.Unmarshal([]byte(cursorMatcherManifest), &hj); err != nil {
		t.Fatal(err)
	}
	got := hookVendorWarnings(hj)
	if len(got) != 1 || got[0].Fix == "" || strings.Contains(got[0].Message, got[0].Fix) {
		t.Fatalf("warnings = %+v, want one with its fix in the fix field", got)
	}
}

// ynd validate prints its warnings on stderr through the shared printer, with
// the fix on its own line, and nothing of them on stdout.
func TestValidateWarningsGoToStderr(t *testing.T) {
	dir := t.TempDir()
	if err := writePluginJSONFile(dir, []byte(cursorMatcherManifest)); err != nil {
		t.Fatal(err)
	}
	var verr error
	stdout, stderr := captureStreams(t, func() { verr = cmdValidate([]string{dir}) })
	if verr != nil {
		t.Fatalf("validate: %v\n%s\n%s", verr, stdout, stderr)
	}
	if strings.Contains(stdout, "warning") {
		t.Errorf("a warning on stdout:\n%s", stdout)
	}
	if !strings.HasPrefix(stderr, "warning: hook matcher") || !strings.Contains(stderr, "\n  Fix: ") {
		t.Errorf("stderr = %q, want a warning line then a Fix line", stderr)
	}
}

// ynd lint names the manifest in the warning and prints it on stderr.
func TestLintHookWarningGoesToStderrWithItsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugin.json")
	if err := os.WriteFile(path, []byte(cursorMatcherManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := captureStreams(t, func() { printHookWarnings(path) })
	if stdout != "" || !strings.HasPrefix(stderr, "warning: "+path+": hook matcher") || !strings.Contains(stderr, "\n  Fix: ") {
		t.Errorf("stdout %q stderr %q", stdout, stderr)
	}
}
