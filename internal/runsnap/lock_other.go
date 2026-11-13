//go:build !unix

package runsnap

// LockShared is a no-op on non-unix platforms; ynh ships only darwin and
// linux builds and this keeps cross-compilation working.
func LockShared(_ string) (func(), error) { return func() {}, nil }

// tryLockExclusive reports false on non-unix platforms, so nothing is deleted.
func tryLockExclusive(_ string) (func(), bool) { return nil, false }
