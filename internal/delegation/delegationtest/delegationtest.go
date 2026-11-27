// Package delegationtest holds the test helper every package that starts
// delegations shares, so none of them leaves a supervisor writing a record
// while t.TempDir's cleanup removes its folder.
package delegationtest

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
)

// StopAll cancels every delegation the test left running under the current
// YNH_HOME, and waits for their runs and supervisors to finish, so the test's
// directories can be removed. Register it with t.Cleanup after the test's
// temp folders are created, so it runs before they are removed.
func StopAll(t *testing.T) {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(delegation.Dir(), "*", "*", "dlg-*.json"))
	for _, p := range paths {
		_, _ = delegation.Cancel(p)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		for _, p := range paths {
			// The supervisor outlives the run it supervises: after the run's
			// group is gone it still writes the final record, so wait for
			// the supervisor process too, or the temp folder's removal races
			// that write.
			r, err := delegation.Load(p)
			if err != nil {
				continue
			}
			if r.Pgid > 1 && syscall.Kill(-r.Pgid, 0) == nil {
				busy = true
			}
			if r.Pid > 1 && syscall.Kill(r.Pid, 0) == nil {
				busy = true
			}
		}
		if !busy {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("delegations were still running after 5s; the temp folder's removal may race them")
}
