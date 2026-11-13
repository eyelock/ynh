//go:build !unix

package delegation

// lockFile is a no-op on non-unix platforms; ynh ships only darwin and linux
// builds and this keeps cross-compilation working.
func lockFile(_ string) (func(), error) { return func() {}, nil }
