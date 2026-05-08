//go:build unix

package resolver

import (
	"fmt"
	"os"
	"syscall"
)

// withRepoLock acquires an exclusive advisory file lock at lockPath for the
// duration of fn. The lock file is created if absent. Concurrent ynh
// processes operating on the same cache entry block on the lock instead of
// racing each other (and tripping over partial clones, in-flight RemoveAll,
// or git's own .git/config writes).
//
// The lock file itself is left in place; flock state is per-fd and released
// when the file is closed. Removing the file would race with peers that
// already hold an open fd on it.
func withRepoLock(lockPath string, fn func() error) error {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening cache lock %s: %w", lockPath, err)
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking cache %s: %w", lockPath, err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return fn()
}
