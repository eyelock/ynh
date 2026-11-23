//go:build unix

package assembler

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
)

// TestLockRunDir_ParallelAssemblyDoesNotRace launches several assemblies of
// one run directory at once, as parallel `ynh run`s of one harness do. Each
// holds the run lock for its assembly and then checks that every skill file
// is present: without the lock one run's clean removes files another has
// just written ("directory not empty", a missing SKILL.md). ynh #724.
func TestLockRunDir_ParallelAssemblyDoesNotRace(t *testing.T) {
	const skills = 40
	repoDir := t.TempDir()
	for i := range skills {
		d := filepath.Join(repoDir, "skills", fmt.Sprintf("skill-%02d", i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("body"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	content := []resolver.ResolvedContent{{BasePath: repoDir}}
	adapter := &mockAdapter{}
	runDir := filepath.Join(t.TempDir(), "run", "harness")

	const runs = 12
	errs := make(chan error, runs)
	var wg sync.WaitGroup
	for range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := LockRunDir(runDir + ".lock")
			if err != nil {
				errs <- err
				return
			}
			defer unlock()
			if err := AssembleTo(runDir, adapter, content); err != nil {
				errs <- err
				return
			}
			for i := range skills {
				p := filepath.Join(runDir, ".mock", "skills", fmt.Sprintf("skill-%02d", i), "SKILL.md")
				if _, err := os.Stat(p); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestLockRunDir_ReleaseIsIdempotentAndReacquirable(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run", "h")
	unlock, err := LockRunDir(runDir + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	unlock()
	again, err := LockRunDir(runDir + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	again()
}
