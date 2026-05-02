//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_RejectsLocalPathTraversal verifies the security guard documented
// in pathutil.CheckSubpath: a relative `local:` path containing `..` must
// be rejected when the include is resolved (during `ynh run`). Without
// this, a malicious harness manifest could escape its directory.
//
// Note: install accepts the manifest (it only validates structure, not
// resolution). The check fires when the include is actually resolved.
func TestRun_RejectsLocalPathTraversal(t *testing.T) {
	s := newSandbox(t)

	harness := filepath.Join(t.TempDir(), "evil")
	if err := os.MkdirAll(filepath.Join(harness, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "evil",
  "version": "0.1.0",
  "includes": [{"local": "../../../etc"}]
}
`
	if err := os.WriteFile(filepath.Join(harness, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	_, errOut, err := runYnhInDirRaw(t, s, project, "run", "evil", "-v", "cursor", "--install")
	if err == nil {
		t.Fatalf("expected run with .. in local include to fail, got success")
	}
	if !strings.Contains(errOut, "traverse") && !strings.Contains(errOut, "..") {
		t.Errorf("expected error to mention traversal, got: %s", errOut)
	}
}

// TestInstall_TransitiveAgentsMd verifies the documented "later sources
// override earlier" rule for AGENTS.md / instructions.md: when both an
// include and the harness itself carry instructions, the harness wins.
//
// This is critical correctness — silently swapping the order would mean
// users' own AGENTS.md gets clobbered by an include's.
func TestInstall_TransitiveAgentsMd(t *testing.T) {
	s := newSandbox(t)

	// Include source carries its own AGENTS.md (and a skill so the include is non-empty).
	include := filepath.Join(t.TempDir(), "include-source")
	if err := os.MkdirAll(filepath.Join(include, "skills", "from-include"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(include, "skills", "from-include", "SKILL.md"),
		[]byte("---\nname: from-include\ndescription: stub.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(include, "AGENTS.md"),
		[]byte("INCLUDE_INSTRUCTIONS\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Harness has its own AGENTS.md plus the include.
	harness := filepath.Join(t.TempDir(), "transitive")
	if err := os.MkdirAll(filepath.Join(harness, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "transitive",
  "version": "0.1.0",
  "includes": [{"local": %q}]
}
`, include)
	if err := os.WriteFile(filepath.Join(harness, ".ynh-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harness, "AGENTS.md"),
		[]byte("HARNESS_INSTRUCTIONS\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.mustRunYnh(t, "install", harness)
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", "transitive", "-v", "cursor", "--install")

	body, err := os.ReadFile(filepath.Join(s.home, "run", "transitive", ".cursorrules"))
	if err != nil {
		t.Fatalf("reading .cursorrules: %v", err)
	}
	if !strings.Contains(string(body), "HARNESS_INSTRUCTIONS") {
		t.Errorf(".cursorrules should contain harness's own instructions, got:\n%s", body)
	}
	if strings.Contains(string(body), "INCLUDE_INSTRUCTIONS") {
		t.Errorf(".cursorrules contains include's instructions — harness should override:\n%s", body)
	}
}
