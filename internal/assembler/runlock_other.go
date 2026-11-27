//go:build !unix

package assembler

// LockRunDir is a no-op on non-unix platforms. ynh ships only darwin and
// linux builds; this stub keeps cross-compilation working.
func LockRunDir(_ string) (func(), error) {
	return func() {}, nil
}
