//go:build unix

package assembler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// TestRuntimeInstructions_ParallelRunsDoNotCross follows the order `ynh run`
// uses: lock the run dir, assemble, append the run's --instructions, release.
// Copilot appends to the shared AGENTS.md, so without the lock held through
// the append, one run's assembly empties the file between another's assembly
// and append, and the texts cross. Each run must end up with exactly its own
// text appended. ynh #764 review.
func TestRuntimeInstructions_ParallelRunsDoNotCross(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "AGENTS.md"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := []resolver.ResolvedContent{{BasePath: repoDir}}
	c := &vendor.Copilot{}
	runDir := filepath.Join(t.TempDir(), "run", "harness")

	const runs = 12
	errs := make(chan error, runs)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := LockRunDir(runDir + ".lock")
			if err != nil {
				errs <- err
				return
			}
			defer unlock()
			if err := AssembleTo(runDir, c, content); err != nil {
				errs <- err
				return
			}
			mine := fmt.Sprintf("instructions-of-run-%02d", i)
			if _, err := c.ApplyRuntimeInstructions(runDir, mine); err != nil {
				errs <- err
				return
			}
			got, err := os.ReadFile(filepath.Join(runDir, "AGENTS.md"))
			if err != nil {
				errs <- err
				return
			}
			if n := strings.Count(string(got), "instructions-of-run-"); n != 1 || !strings.Contains(string(got), mine) {
				errs <- fmt.Errorf("run %d: AGENTS.md holds %d runtime texts, want only its own:\n%s", i, n, got)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
