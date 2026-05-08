//go:build !unix

package resolver

// withRepoLock is a no-op on non-unix platforms. ynh ships only darwin and
// linux builds; this stub exists so cross-compilation to other platforms
// (e.g. GOOS=windows go build for sanity-checking) still succeeds. There is
// no inter-process locking on these platforms — concurrent ynh invocations
// on the same cache entry can still race.
func withRepoLock(_ string, fn func() error) error {
	return fn()
}
