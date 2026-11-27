//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
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

	out, _ := s.mustRunYnh(t, "image", "local/imaged", "--dry-run")

	for _, want := range []string{
		"FROM ghcr.io/eyelock/ynh:latest",
		"COPY --chown=ynh:ynh home/ /home/ynh/.ynh/",
		"COPY --chown=ynh:ynh vendors/claude/",
		"COPY --chown=ynh:ynh vendors/codex/",
		"COPY --chown=ynh:ynh vendors/cursor/",
		"ENV YNH_VENDOR=claude",
		`dev.ynh.harness="imaged"`,
		`ENTRYPOINT ["tini", "-s", "--", "ynh", "run", "local/imaged"]`,
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

	out, _ := s.mustRunYnh(t, "image", "local/imaged-custom", "--base", "myorg/ynh:dev", "--dry-run")
	if !strings.Contains(out, "FROM myorg/ynh:dev") {
		t.Errorf("Dockerfile should honour --base, got:\n%s", out)
	}
}

// TestImage_TrustGate asserts `ynh image` refuses an installed harness whose
// commands are not accepted, and, once accepted, labels the image with the
// digest it carries.
func TestImage_TrustGate(t *testing.T) {
	s := newSandbox(t)
	dir := newSyntheticSkillHarness(t, "gated")
	plugin := `{"name":"gated","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"./hooks/done.sh"}]}}`
	if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "done.sh"), []byte("#!/bin/sh\necho done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", dir)

	_, stderr, err := s.runYnh(t, "image", "local/gated", "--dry-run")
	if err == nil || !strings.Contains(stderr, "ynh trust accept local/gated") {
		t.Fatalf("an unaccepted harness was not refused: err=%v\n%s", err, stderr)
	}

	s.mustRunYnh(t, "trust", "accept", "local/gated")
	out, _ := s.mustRunYnh(t, "image", "local/gated", "--dry-run")
	if !strings.Contains(out, `dev.ynh.harness.trust-digest="sha256:`) {
		t.Errorf("Dockerfile does not label the digest:\n%s", out)
	}
}

// TestImage_FromTrust asserts `ynh image --from` refuses a harness with
// commands at build time, and bakes the acceptance with --trust (ynh #986).
func TestImage_FromTrust(t *testing.T) {
	s := newSandbox(t)
	dir := newSyntheticSkillHarness(t, "fromgated")
	plugin := `{"name":"fromgated","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"./hooks/done.sh"}]}}`
	if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "done.sh"), []byte("#!/bin/sh\necho done\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := s.runYnh(t, "image", "fromgated", "--from", dir, "--dry-run")
	if err == nil || !strings.Contains(stderr, "Fix:") || !strings.Contains(stderr, "--trust") {
		t.Fatalf("an unaccepted --from harness was not refused: err=%v\n%s", err, stderr)
	}

	out, _ := s.mustRunYnh(t, "image", "fromgated", "--from", dir, "--trust", "--dry-run")
	if !strings.Contains(out, `dev.ynh.harness.trust-digest="sha256:`) {
		t.Errorf("Dockerfile does not label the digest:\n%s", out)
	}
}
