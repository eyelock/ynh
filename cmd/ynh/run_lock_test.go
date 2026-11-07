//go:build unix

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/vendor"
)

// stubLaunch replaces the vendor launch for the test, so cmdRun runs for real
// up to the point where the vendor would start.
func stubLaunch(t *testing.T, fn func(runDir string, ra runArgs) error) {
	t.Helper()
	old := launchSessionFn
	t.Cleanup(func() { launchSessionFn = old })
	launchSessionFn = func(_ vendor.Adapter, runDir, _ string, ra runArgs, _, _ string, _ []string, _ io.Writer) error {
		return fn(runDir, ra)
	}
}

// runInParallel calls cmdRun for the same harness n times at once, each with
// its own --instructions, as parallel `ynh run`s of one harness do.
func runInParallel(t *testing.T, vendorName string, n int) {
	t.Helper()
	isolateRun(t)
	work := t.TempDir()
	harnessDir := filepath.Join(work, "h")
	writeSkillHarness(t, harnessDir, "h")
	t.Chdir(work)

	// A vendor that needs project symlinks asks before planting them, and
	// refuses when there is no terminal. Give consent once, as a user would
	// with --install, so the parallel runs do not depend on the prompt.
	if vendorName == "cursor" {
		if err := cmdRun([]string{harnessDir, "-v", vendorName, "--install"}); err != nil {
			t.Fatalf("--install: %v", err)
		}
	}

	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- cmdRun([]string{harnessDir, "-v", vendorName, "--instructions", fmt.Sprintf("instructions-of-run-%02d", i)})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

// The lock is held until the launch, so what a vendor reads from the run dir then (Copilot's AGENTS.md, which it
// projects into the project; OpenCode's workspace grant and AGENTS.md compose)
// is the run's own. It fails if the lock is released before the launch: another
// run then re-assembles the dir and appends its own instructions. ynh #724.
func TestCmdRun_ParallelRunsKeepTheLockUntilTheLaunch(t *testing.T) {
	stubLaunch(t, func(runDir string, ra runArgs) error {
		time.Sleep(25 * time.Millisecond) // wide window for a peer to intrude
		got, err := os.ReadFile(filepath.Join(runDir, "AGENTS.md"))
		if err != nil {
			return err
		}
		if n := strings.Count(string(got), "instructions-of-run-"); n != 1 || !strings.Contains(string(got), ra.Instructions) {
			return fmt.Errorf("%q: AGENTS.md holds %d runtime texts, want only its own:\n%s", ra.Instructions, n, got)
		}
		return nil
	})
	runInParallel(t, "copilot", 6)
}

// Every vendor runs as a child of ynh, so none may hold the run dir's lock for
// its session. Two real `ynh run`s of one harness, each launching a stand-in
// vendor CLI: the first vendor stays running until the second one has started,
// which can only happen if the second run was not blocked by the first's lock.
// It fails (the first vendor times out) if the lock is kept past the child's
// start. Each vendor that ynh launches through the shared supervisor is covered
// by the same release; Claude stands in for them.
func TestCmdRun_SecondRunIsNotBlockedWhileTheFirstVendorRuns(t *testing.T) {
	isolateRun(t)
	work := t.TempDir()
	harnessDir := filepath.Join(work, "h")
	writeSkillHarness(t, harnessDir, "h")
	t.Chdir(work)

	bin := t.TempDir()
	state := t.TempDir()
	script := `#!/bin/sh
# The first vendor to start waits for the second; the second just starts.
if mkdir "$STATE/first" 2>/dev/null; then
  i=0
  while [ ! -e "$STATE/second" ]; do
    i=$((i+1)); [ $i -gt 200 ] && exit 3
    sleep 0.05
  done
  exit 0
fi
touch "$STATE/second"
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STATE", state)

	run := func(label string) chan error {
		ch := make(chan error, 1)
		go func() {
			ch <- cmdRun([]string{harnessDir, "-v", "claude", "--instructions", label})
		}()
		return ch
	}
	first := run("first")
	// Let the first vendor reach its wait before the second run starts.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(state, "first")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first vendor never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	second := run("second")

	for name, ch := range map[string]chan error{"first": first, "second": second} {
		select {
		case err := <-ch:
			if err != nil {
				t.Errorf("%s run: %v", name, err)
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("%s run did not finish: the run dir lock is held while the vendor runs", name)
		}
	}
}
