//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestImage_DryRun asserts `ynh image <name> --dry-run` renders a Dockerfile
// to stdout containing the harness's identity (FROM, COPY, ENV, LABEL lines
// referencing the harness name). Pure file-generation — no docker build,
// so the test runs without docker installed.
func TestImage_DryRun(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "imaged")
	s.mustRunYnh(t, "install", harness)

	out, _ := s.mustRunYnh(t, "image", "imaged", "--dry-run")

	for _, want := range []string{
		"FROM ghcr.io/eyelock/ynh:latest",
		"COPY --link --chown=ynh:ynh vendors/claude/",
		"COPY --link --chown=ynh:ynh vendors/codex/",
		"COPY --link --chown=ynh:ynh vendors/cursor/",
		"ENV YNH_VENDOR=claude",
		`dev.ynh.harness="imaged"`,
		`ENTRYPOINT ["tini", "-s", "--", "ynh", "run", "imaged"]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Dockerfile missing line %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestImage_DryRun_CustomBase asserts --base overrides the FROM line.
func TestImage_DryRun_CustomBase(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "imaged-custom")
	s.mustRunYnh(t, "install", harness)

	out, _ := s.mustRunYnh(t, "image", "imaged-custom", "--base", "myorg/ynh:dev", "--dry-run")
	if !strings.Contains(out, "FROM myorg/ynh:dev") {
		t.Errorf("Dockerfile should honour --base, got:\n%s", out)
	}
}
