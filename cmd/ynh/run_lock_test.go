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

// A vendor that replaces the process keeps the run dir's lock until its
// launch, so what it reads from the run dir then (Copilot's AGENTS.md, which it
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

// A vendor that runs as a child of ynh must not hold the lock for its whole
// session: the runs all reach their launch while the others are still in it.
// It deadlocks (and times out) if the lock is kept past the launch.
func TestCmdRun_ChildVendorReleasesTheLockBeforeTheLaunch(t *testing.T) {
	const runs = 3
	var mu sync.Mutex
	arrived := 0
	allIn := make(chan struct{})
	stubLaunch(t, func(string, runArgs) error {
		mu.Lock()
		arrived++
		if arrived == runs {
			close(allIn)
		}
		mu.Unlock()
		select {
		case <-allIn:
			return nil
		case <-time.After(10 * time.Second):
			return fmt.Errorf("a run is still holding the run dir lock while its vendor runs")
		}
	})
	runInParallel(t, "cursor", runs)
}
