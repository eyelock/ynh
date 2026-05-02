//go:build e2e

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// productionSmokeHarnesses are real harnesses on the HEAD of
// eyelock/assistants:develop. The smoke layer asserts only that they
// install successfully and produce a well-formed installed.json — no
// byte-exact assertions. The point is to catch "we changed something
// subtle that breaks real harnesses but happens to still pass against
// the synthetic fixtures."
var productionSmokeHarnesses = []string{
	"ynh/ynh-dev",
}

// TestSmoke_LiveAssistants installs each production harness against the
// current HEAD of eyelock/assistants:develop. Runs at release-promotion
// time; if upstream is broken the gate blocks, surfacing the issue
// before goreleaser publishes.
func TestSmoke_LiveAssistants(t *testing.T) {
	for _, path := range productionSmokeHarnesses {
		t.Run(path, func(t *testing.T) {
			s := newSandbox(t)
			out, _ := s.mustRunYnh(t,
				"install", "https://github.com/eyelock/assistants",
				"--path", path,
			)
			name := lastSegment(path)
			if !strings.Contains(out, `Installed harness "`+name+`"`) {
				t.Errorf("install stdout missing success line for %q:\n%s", name, out)
			}

			got := readInstalledJSON(t, filepath.Join(s.home, "harnesses", name))
			assertEqual(t, "source_type", got.SourceType, "git")
			assertEqual(t, "path", got.Path, path)
			if !sha40.MatchString(got.SHA) {
				t.Errorf("smoke install: sha %q is not 40-char hex", got.SHA)
			}
		})
	}
}

func lastSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
