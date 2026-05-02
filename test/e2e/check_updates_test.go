//go:build e2e

package e2e

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestLs_CheckUpdates_JSON_Shape installs a harness with a floating include
// pointing at a local file:// upstream, then runs `ynh ls --check-updates
// --format json` after the upstream HEAD has moved. Asserts the JSON output
// carries ref_installed (the resolved SHA at install) and ref_available (the
// new upstream SHA) at the include level, and that they differ.
//
// This is the JSON shape the suite was built for — the #115 regression
// surfaced through this exact field pair on includes.
func TestLs_CheckUpdates_JSON_Shape(t *testing.T) {
	s := newSandbox(t)
	upstream := newLocalUpstream(t, "include-target", "first content")
	harness := newLocalFloatingHarness(t, "check-updates-harness", upstream)

	s.mustRunYnh(t, "install", harness)

	commitToUpstream(t, upstream, "include-target/SKILL.md", "second content")

	out, _ := s.mustRunYnh(t, "ls", "--check-updates", "--format", "json")

	var env struct {
		Schema    string `json:"$schema"`
		Harnesses []struct {
			Name     string `json:"name"`
			Includes []struct {
				Git          string `json:"git"`
				RefInstalled string `json:"ref_installed,omitempty"`
				RefAvailable string `json:"ref_available,omitempty"`
				IsPinned     bool   `json:"is_pinned"`
			} `json:"includes"`
		} `json:"harnesses"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parsing ls --check-updates JSON: %v\n%s", err, out)
	}

	if len(env.Harnesses) != 1 {
		t.Fatalf("expected 1 harness, got %d: %+v", len(env.Harnesses), env.Harnesses)
	}
	h := env.Harnesses[0]
	assertEqual(t, "harness name", h.Name, "check-updates-harness")
	if len(h.Includes) != 1 {
		t.Fatalf("expected 1 include, got %d: %+v", len(h.Includes), h.Includes)
	}
	inc := h.Includes[0]

	if !sha40.MatchString(inc.RefInstalled) {
		t.Errorf("includes[0].ref_installed %q is not a 40-char SHA", inc.RefInstalled)
	}
	if !sha40.MatchString(inc.RefAvailable) {
		t.Errorf("includes[0].ref_available %q is not a 40-char SHA — probe may have failed", inc.RefAvailable)
	}
	if inc.RefInstalled == inc.RefAvailable {
		t.Errorf("expected ref_installed != ref_available after upstream HEAD moved; both are %s", inc.RefInstalled)
	}

	// Also sanity-check we can still read installed.json and it agrees with the JSON.
	inst := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "check-updates-harness"))
	if len(inst.Resolved) != 1 {
		t.Fatalf("installed.json: expected 1 resolved entry, got %d", len(inst.Resolved))
	}
	assertEqual(t, "ref_installed agrees with installed.json", inc.RefInstalled, inst.Resolved[0].SHA)
}
