//go:build unix

package runsnap

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockShared takes a shared advisory lock on the file at path, creating it,
// and returns the function that releases it. Many sessions share one snapshot;
// a later deleter takes the lock exclusively (without blocking) to learn that
// no session holds it. The lock is dropped when the process exits.
func LockShared(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating snapshot lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening snapshot lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking snapshot %s: %w", path, err)
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

// tryLockExclusive takes an exclusive lock on the lock file at path without
// waiting. It reports false when a session holds the snapshot (or the file
// cannot be opened). A snapshot with no lock file was never locked, so it has
// no session and the lock is granted on a file it creates.
func tryLockExclusive(path string) (func(), bool) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, false
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true
}
