package main

import (
	"os"

	"github.com/eyelock/ynh/internal/vendor"
)

// resolveVendorEnv returns the YNH_VENDOR env var value, or empty string.
func resolveVendorEnv() string {
	return os.Getenv("YNH_VENDOR")
}

// resolveVendorDefault returns the vendor from: flag > YNH_VENDOR > vendor.DefaultName.
func resolveVendorDefault(flag string) string {
	if flag != "" {
		return flag
	}
	if v := resolveVendorEnv(); v != "" {
		return v
	}
	return vendor.DefaultName
}

// resolveHarnessEnv returns the YNH_HARNESS env var value, or empty string.
func resolveHarnessEnv() string {
	return os.Getenv("YNH_HARNESS")
}

// skipConfirmEnv returns true if non-interactive mode is requested via env.
// Checks YNH_YES and CI (common convention).
func skipConfirmEnv() bool {
	if os.Getenv("YNH_YES") != "" {
		return true
	}
	if os.Getenv("CI") != "" {
		return true
	}
	return false
}
