//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestYnd_Compose_JSON asserts `ynd compose --format json` emits the
// documented envelope shape — name/version/default_vendor/counts/artifacts —
// suitable for tooling that needs to introspect a harness without
// reaching into the manifest itself.
func TestYnd_Compose_JSON(t *testing.T) {
	harness := newSyntheticSkillHarness(t, "composed")

	out, _ := mustRunYnd(t, "compose", harness, "--format", "json")

	var got struct {
		Name          string `json:"name"`
		Version       string `json:"version"`
		DefaultVendor string `json:"default_vendor"`
		Artifacts     struct {
			Skills []struct {
				Name string `json:"name"`
			} `json:"skills"`
		} `json:"artifacts"`
		Counts map[string]any `json:"counts"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing compose JSON: %v\n%s", err, out)
	}
	assertEqual(t, "name", got.Name, "composed")
	assertEqual(t, "version", got.Version, "0.1.0")
	assertEqual(t, "default_vendor", got.DefaultVendor, "claude")
	if len(got.Artifacts.Skills) == 0 {
		t.Errorf("expected at least one skill in artifacts")
	}
	if got.Counts == nil {
		t.Errorf("expected counts to be present")
	}
}

// TestYnd_Diff_OutputDifference asserts `ynd diff <harness> claude cursor`
// surfaces the structural difference between two vendor layouts. Doesn't
// pin exact content (vendor adapters evolve) — checks that the diff
// header and "Only in" / "Identical" sections are emitted.
func TestYnd_Diff_OutputDifference(t *testing.T) {
	harness := newSyntheticSkillHarness(t, "diffed")

	out, _ := mustRunYnd(t, "diff", harness, "claude", "cursor")

	for _, want := range []string{"=== claude vs cursor ==="} {
		if !strings.Contains(out, want) {
			t.Errorf("diff output missing %q\n%s", want, out)
		}
	}
	// Either there are differences ("Only in" / "Different content") or
	// they're identical — at least one of these section headers must appear.
	if !strings.Contains(out, "Only in") && !strings.Contains(out, "Identical") && !strings.Contains(out, "Different") {
		t.Errorf("diff output should report at least one Only in / Different / Identical section:\n%s", out)
	}
}
