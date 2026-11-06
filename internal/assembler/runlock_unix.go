//go:build unix

package assembler

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockRunDir takes an exclusive advisory lock on the file lockPath and returns
// the function that releases it. Callers keep one lock file per run directory,
// outside that directory: assembly empties the directory, and a lock file
// among the run directories would be taken for a harness's.
//
// Assembly of a run directory is several steps (clean, copy artifacts, write
// delegates, hooks, MCP config). Two ynh processes assembling the same
// directory interleave those steps and break each other: one removes files
// the other is about to read. Holding this lock across the whole assembly
// makes them take turns instead. The returned release is safe to call more
// than once.
//
// The lock file is left in place: removing it would race with peers that
// already hold it open.
func LockRunDir(lockPath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating run lock dir: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening run lock %s: %w", lockPath, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking run dir %s: %w", lockPath, err)
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
