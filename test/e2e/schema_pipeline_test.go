//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestSchemaPipeline_LsValidatesAgainstSchema is the end-to-end check that
// real ynh binary output validates against the embedded schema via the real
// ynd binary. Exercises the full chain: ynh ls --format json → ynd
// validate-output --schema list → exit 0. Catches any drift the unit-level
// round-trip tests would miss (e.g. ldflag-injected version strings,
// real-FS-driven envelope contents, embedded-vs-source schema mismatches).
func TestSchemaPipeline_LsValidatesAgainstSchema(t *testing.T) {
	s := newSandbox(t)
	lsOut, _ := s.mustRunYnh(t, "ls", "--format", "json")

	cmd := exec.Command(yndBinary(t), "validate", "--schema", "list")
	cmd.Stdin = strings.NewReader(lsOut)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("ynd validate-output failed: %v\nstdout: %s\nstderr: %s\ninput: %s", err, out.String(), errOut.String(), lsOut)
	}
	if !strings.Contains(out.String(), "ok") {
		t.Errorf("expected 'ok' on success, got: %s", out.String())
	}
}

// TestSchemaPipeline_ManifestRoundTrip exercises `ynh schema --all
// --format json` and confirms (a) every embedded CLI schema is present and
// (b) the version schema's $id is exactly what consumers see.
func TestSchemaPipeline_ManifestRoundTrip(t *testing.T) {
	out, _ := mustRunYnh(t, "schema", "--all", "--format", "json")
	var manifest struct {
		Capabilities string                     `json:"capabilities"`
		YnhVersion   string                     `json:"ynh_version"`
		Schemas      map[string]json.RawMessage `json:"schemas"`
	}
	if err := json.Unmarshal([]byte(out), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v\nraw: %s", err, out)
	}
	if manifest.Capabilities == "" {
		t.Error("manifest missing capabilities")
	}
	want := []string{
		"cli/version", "cli/list", "cli/info", "cli/fork", "cli/installed",
		"cli/error", "cli/search", "cli/sources", "cli/registry",
		"cli/paths", "cli/status", "cli/vendors",
		"shared/envelope", "shared/enums", "shared/harness",
	}
	for _, n := range want {
		if _, ok := manifest.Schemas[n]; !ok {
			t.Errorf("manifest missing %q", n)
		}
	}

	// Spot-check the version schema's $id is the published URL.
	versionRaw := manifest.Schemas["cli/version"]
	var versionSchema struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(versionRaw, &versionSchema); err != nil {
		t.Fatalf("unmarshal version schema: %v", err)
	}
	const wantID = "https://eyelock.github.io/ynh/schema/cli/version.schema.json"
	if versionSchema.ID != wantID {
		t.Errorf("version $id = %q, want %q", versionSchema.ID, wantID)
	}
}

// TestSchemaPipeline_ValidateOutputDetectsBadInput confirms ynd
// validate-output actually rejects malformed input, not just rubber-stamps
// everything. Without this, a misconfigured validator could silently let
// drift through.
func TestSchemaPipeline_ValidateOutputDetectsBadInput(t *testing.T) {
	// Missing the required "capabilities" field — must fail.
	bad := `{"version": "0.3.1"}`
	cmd := exec.Command(yndBinary(t), "validate", "--schema", "version")
	cmd.Stdin = strings.NewReader(bad)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err == nil {
		t.Errorf("expected non-zero exit for invalid input; stdout: %s\nstderr: %s", out.String(), errOut.String())
	}
}

// mustRunYnh is a thin wrapper for ynh runs that don't need a sandbox
// (e.g. `ynh schema --all` is stateless). The sandbox version is for
// stateful commands that touch ~/.ynh.
func mustRunYnh(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(ynhBinary(t), args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("ynh %s failed: %v\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), err, outBuf.String(), errBuf.String())
	}
	return outBuf.String(), errBuf.String()
}
