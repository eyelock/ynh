//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// loopSessionCap is the test's own cap on delegate sessions: the fake vendor
// refuses to delegate once this many exist, so a ynh that never stops the loop
// cannot run away. ynh must stop the chain well before it.
const loopSessionCap = 12

// A circular delegates_to (loopa delegates to loopb, which delegates back to
// loopa) must be stopped by ynh at runtime (ynh #892). The vendor is a shell
// stand-in on PATH, the home a scratch YNH_HOME: no real vendor runs. Each fake
// session delegates to the other harness exactly as a model would call the
// delegate tool, through `ynh delegate-run start`, and writes what ynh answered.
func TestDelegateLoopIsStoppedAtRuntime(t *testing.T) {
	s := newSandbox(t)
	root := t.TempDir()
	for _, p := range [][2]string{{"loopa", "loopb"}, {"loopb", "loopa"}} {
		dir := filepath.Join(root, p[0])
		if err := os.MkdirAll(filepath.Join(dir, ".agents/harness"), 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := `{"name":"` + p[0] + `","version":"0.1.0","default_vendor":"claude",` +
			`"delegates_to":[{"git":"` + root + `","path":"` + p[1] + `"}]}`
		if err := os.WriteFile(filepath.Join(dir, ".agents/harness/plugin.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(t, root, "init", "-q")
	mustGit(t, root, "add", ".")
	mustGit(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "loop")
	for _, n := range []string{"loopa", "loopb"} {
		s.mustRunYnh(t, "install", filepath.Join(root, n))
	}

	work := t.TempDir()
	shims := filepath.Join(work, "shims")
	if err := os.MkdirAll(shims, 0o755); err != nil {
		t.Fatal(err)
	}
	answers := filepath.Join(work, "answers.log")
	runaway := filepath.Join(work, "runaway")
	fake := `#!/bin/sh
me=""
for a in "$@"; do
  case "$a" in *"I am "*) me=${a##*I am } ;; esac
done
me=$(echo "$me" | tr -d ' ')
[ -n "$me" ] || exit 0
n=$(ls "$YNH_HOME"/delegations/*/*/dlg-*.json 2>/dev/null | wc -l)
if [ "$n" -ge "$CAP" ]; then echo "$n" >> "$RUNAWAY"; exit 0; fi
case "$me" in loopa) other=loopb ;; *) other=loopa ;; esac
"$YNH_BIN" delegate-run start --parent "local/$me" --entry 0 --task "I am $other" >> "$ANSWERS" 2>&1
echo "--- $me exit $?" >> "$ANSWERS"
`
	if err := os.WriteFile(filepath.Join(shims, "claude"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"PATH="+shims+string(os.PathListSeparator)+os.Getenv("PATH"),
		"YNH_HOME="+s.home, "YNH_BIN="+ynhBinary(t), "CAP="+itoa(loopSessionCap),
		"ANSWERS="+answers, "RUNAWAY="+runaway, "YNH_FOCUS=", "YNH_PROFILE=")

	cmd := exec.Command(ynhBinary(t), "run", "local/loopa", "--", "I am loopa")
	cmd.Dir = work
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ynh run: %v\n%s", err, out)
	}

	// Wait for the chain to go quiet, then stop whatever is left.
	records := func() []string {
		m, _ := filepath.Glob(filepath.Join(s.home, "delegations", "*", "*", "dlg-*.json"))
		return m
	}
	t.Cleanup(func() {
		for _, r := range records() {
			c := exec.Command(ynhBinary(t), "delegate-run", "cancel", "--record", r)
			c.Env = env
			_ = c.Run()
		}
		time.Sleep(300 * time.Millisecond)
	})
	quiet := func() bool {
		for _, r := range records() {
			data, _ := os.ReadFile(r)
			var rec struct {
				Status string `json:"status"`
			}
			_ = json.Unmarshal(data, &rec)
			if rec.Status == "started" || rec.Status == "running" {
				return false
			}
		}
		return true
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if quiet() {
			time.Sleep(1500 * time.Millisecond)
			if quiet() {
				break
			}
		}
	}

	if _, err := os.Stat(runaway); err == nil {
		t.Errorf("the loop ran to the test's cap of %d sessions: ynh never stopped it (%d records)", loopSessionCap, len(records()))
	}
	if n := len(records()); n >= loopSessionCap {
		t.Errorf("%d delegate sessions were started, want well under the cap of %d", n, loopSessionCap)
	}
	got, _ := os.ReadFile(answers)
	for _, want := range []string{"loop", "local/loopa", "Fix:"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("ynh's refusal does not contain %q; the delegate tool's results were:\n%s", want, got)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
