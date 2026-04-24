package pathutil_test

import (
	"testing"

	"github.com/eyelock/ynh/internal/pathutil"
)

func TestCheckSubpath(t *testing.T) {
	valid := []string{
		"skills/dev",
		"harnesses/reviewer",
		"ynh/david",
		"a",
		"a/b/c",
		".",
		"some.dir/with-dashes_and.dots",
	}
	for _, p := range valid {
		t.Run("valid:"+p, func(t *testing.T) {
			if err := pathutil.CheckSubpath(p); err != nil {
				t.Errorf("CheckSubpath(%q) unexpected error: %v", p, err)
			}
		})
	}

	invalid := []string{
		"..",
		"../etc/passwd",
		"../",
		"a/../../etc",
		"/absolute/path",
		"/etc/passwd",
	}
	for _, p := range invalid {
		t.Run("invalid:"+p, func(t *testing.T) {
			if err := pathutil.CheckSubpath(p); err == nil {
				t.Errorf("CheckSubpath(%q) expected error, got nil", p)
			}
		})
	}
}
