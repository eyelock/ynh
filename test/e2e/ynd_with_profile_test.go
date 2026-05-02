//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const profileTwoHooksHarness = `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "withp",
  "version": "0.1.0",
  "hooks": {
    "before_tool": [{"command": "echo BASE"}]
  },
  "profiles": {
    "p": {
      "hooks": {
        "before_tool": [{"command": "echo OVERRIDE"}]
      }
    }
  }
}
`

// TestYnd_Compose_WithProfile asserts that `ynd compose --profile p` returns
// the merged view of the harness — the profile-overridden hook command
// must surface in the composed JSON, not the base.
func TestYnd_Compose_WithProfile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "withp")
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"),
		[]byte(profileTwoHooksHarness), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := mustRunYnd(t, "compose", dir, "--profile", "p", "--format", "json")

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing compose JSON: %v\n%s", err, out)
	}
	if !bytes.Contains([]byte(out), []byte("OVERRIDE")) {
		t.Errorf("compose with profile p should reflect override hook, got:\n%s", out)
	}
}

// TestYnd_Preview_WithProfile asserts that `ynd preview --profile p`
// assembles the merged view to disk — the profile-overridden hook must
// appear in the rendered hooks file.
func TestYnd_Preview_WithProfile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "withp-preview")
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"),
		[]byte(profileTwoHooksHarness), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "preview-out")
	mustRunYnd(t, "preview", "--harness", dir, "-v", "cursor", "-o", outDir, "--profile", "p")

	body, err := os.ReadFile(filepath.Join(outDir, ".cursor", "hooks.json"))
	if err != nil {
		t.Fatalf("reading hooks file: %v", err)
	}
	if !bytes.Contains(body, []byte("OVERRIDE")) {
		t.Errorf("preview with profile p should produce profile hooks, got:\n%s", body)
	}
	if bytes.Contains(body, []byte("BASE")) {
		t.Errorf("preview with profile p leaked base hook BASE:\n%s", body)
	}
}
