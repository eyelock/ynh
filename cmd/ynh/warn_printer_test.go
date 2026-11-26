package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// handPrintedWarningAllowed names the files that still print "warning: " by
// hand, each for a stated reason. Everything else in cmd/ynh goes through
// internal/warn, so a new warning gets the box, NO_COLOR and plain forms free.
var handPrintedWarningAllowed = map[string]string{
	// The trust-gate pull requests rewrite these trust warnings (ynw-wj4j left them alone to avoid conflicts).
	"trust.go": "warnIfTrustChanged and warnRunIfTrustChanged, being reworked by the trust gate",
}

func TestNoHandPrintedWarnings(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || handPrintedWarningAllowed[f] != "" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(line, `"warning: `) || strings.Contains(line, `"  warning: `) {
				t.Errorf("%s:%d prints a warning by hand; use warn.Print with a warn.Warning:\n%s", f, i+1, line)
			}
		}
	}
}
