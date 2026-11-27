package delegation

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/presence"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestGuardRefusesAReturnToAnAncestor(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	_, err := Guard("local/b", "local/a", []string{"local/a"}, envOf(nil))
	if err == nil {
		t.Fatal("a delegation back to a harness already in the chain must be refused")
	}
	for _, want := range []string{"loop", "local/a -> local/b -> local/a", "Fix:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal lacks %q:\n%v", want, err)
		}
	}
	// A harness delegating to itself is the shortest loop.
	if _, err := Guard("local/a", "local/a", nil, envOf(nil)); err == nil {
		t.Error("a harness delegating to itself must be refused")
	}
}

func TestGuardAllowsADiamondAndReturnsTheAncestry(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	anc, err := Guard("local/b", "local/d", []string{"local/a"}, envOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(anc, ",") != "local/a,local/b" {
		t.Errorf("ancestry = %v", anc)
	}
}

func TestGuardDepthLimit(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	chain := []string{"local/a", "local/b", "local/c"}
	if _, err := Guard("local/d", "local/e", chain, envOf(nil)); err != nil {
		t.Fatalf("depth %d is within the default of %d: %v", len(chain)+1, DefaultMaxDepth, err)
	}
	chain = append(chain, "local/d")
	_, err := Guard("local/e", "local/f", chain, envOf(nil))
	if err == nil || !strings.Contains(err.Error(), "too deep") || !strings.Contains(err.Error(), "Fix:") || !strings.Contains(err.Error(), MaxDepthEnv) {
		t.Fatalf("a chain past the default must be refused with a Fix naming %s, got %v", MaxDepthEnv, err)
	}
	if _, err := Guard("local/e", "local/f", chain, envOf(map[string]string{MaxDepthEnv: "6"})); err != nil {
		t.Errorf("the limit can be raised: %v", err)
	}
	if _, err := Guard("local/b", "local/c", []string{"local/a"}, envOf(map[string]string{MaxDepthEnv: "1"})); err == nil {
		t.Error("the limit can be lowered")
	}
}

func TestGuardRefusesABadLimit(t *testing.T) {
	for _, v := range []string{"0", "-1", "many"} {
		for _, name := range []string{MaxDepthEnv, MaxLiveEnv} {
			_, err := Guard("local/a", "local/b", nil, envOf(map[string]string{name: v}))
			if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "Fix:") {
				t.Errorf("%s=%s: want an error naming the variable with a Fix, got %v", name, v, err)
			}
		}
	}
}

func TestGuardCapsLiveSessions(t *testing.T) {
	folder := setup(t)
	for i := 0; i < 2; i++ {
		start(t, folder, "sleep 30")
	}
	t.Cleanup(func() { stopAll(t) })
	if n := Live(); n != 2 {
		t.Fatalf("Live = %d, want 2", n)
	}
	_, err := Guard("local/a", "local/b", nil, envOf(map[string]string{MaxLiveEnv: "2"}))
	if err == nil || !strings.Contains(err.Error(), "already live") || !strings.Contains(err.Error(), "Fix:") || !strings.Contains(err.Error(), MaxLiveEnv) {
		t.Fatalf("a full cap must refuse with a Fix naming %s, got %v", MaxLiveEnv, err)
	}
	if _, err := Guard("local/a", "local/b", nil, envOf(map[string]string{MaxLiveEnv: "3"})); err != nil {
		t.Errorf("below the cap: %v", err)
	}
}

func TestStartRecordsAndPassesTheAncestry(t *testing.T) {
	folder := setup(t)
	t.Cleanup(func() { stopAll(t) })
	t.Setenv(ChainEnv, "local/root")
	rec, path := start(t, folder, `echo "$`+ChainEnv+`"`)
	if strings.Join(rec.Chain, ",") != "local/root,local/parent" {
		t.Errorf("record chain = %v", rec.Chain)
	}
	done := waitFor(t, path, func(r *Record) bool { return r.Status.Final() })
	if got := strings.TrimSpace(done.Result); got != "local/root,local/parent" {
		t.Errorf("the session's %s = %q, want the ancestry", ChainEnv, got)
	}
}

func TestStartRefusesALoopAndLeavesNoRecord(t *testing.T) {
	folder := setup(t)
	t.Setenv(ChainEnv, "local/delegate")
	_, _, err := Start(StartOptions{
		Parent: "local/parent", Delegate: "local/delegate", Task: "x", Folder: folder,
		Supervisor: []string{os.Args[0]},
	})
	if err == nil || !strings.Contains(err.Error(), "loop") {
		t.Fatalf("Start must refuse a loop, got %v", err)
	}
	if n := Live(); n != 0 {
		t.Errorf("a refused start left %d live sessions", n)
	}
}

// stopAll cancels every session the test left and waits for the supervisors to
// finish writing, so the temp folders can be removed.
func stopAll(t *testing.T) {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(Dir(), "*", "*", "dlg-*.json"))
	for _, p := range paths {
		_, _ = Cancel(p)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		for _, p := range paths {
			if r, err := Load(p); err == nil && r.Pid > 1 && syscall.Kill(r.Pid, 0) == nil {
				busy = true
			}
		}
		if !busy {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The cap counts every live session under the home that ynh ps lists, not only
// delegations: a delegation is refused while the home holds that many live
// sessions of any launch mode, and the refusal points at ynh ps, ynh stop and
// ynh reap.
func TestGuardCapCountsEveryLiveSession(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	var ends []func()
	for _, id := range []string{"a", "b"} {
		s, err := presence.Start(presence.Record{SessionID: id, Harness: "local--h", Vendor: "claude", Cwd: "/x", Mode: presence.ModeInteractive})
		if err != nil {
			t.Fatal(err)
		}
		ends = append(ends, s.End)
	}
	defer func() {
		for _, e := range ends {
			e()
		}
	}()
	if n := LiveSessions(); n != 2 {
		t.Fatalf("LiveSessions = %d, want 2", n)
	}
	_, err := Guard("local/a", "local/b", nil, envOf(map[string]string{MaxLiveEnv: "2"}))
	if err == nil || !strings.Contains(err.Error(), "already live") || !strings.Contains(err.Error(), "ynh stop") || !strings.Contains(err.Error(), "Fix:") {
		t.Fatalf("a full home must refuse with a Fix, got %v", err)
	}
	if _, err := Guard("local/a", "local/b", nil, envOf(map[string]string{MaxLiveEnv: "3"})); err != nil {
		t.Errorf("under the cap: %v", err)
	}
}
