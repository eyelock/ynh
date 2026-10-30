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
	if err := os.MkdirAll(filepath.Join(harness, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "evil",
  "version": "0.1.0",
  "includes": [{"local": "../../../etc"}]
}
`
	if err := os.WriteFile(filepath.Join(harness, ".agents/harness", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	_, errOut, err := runYnhInDirRaw(t, s, project, "run", "local/evil", "-v", "cursor", "--install")
	if err == nil {
		t.Fatalf("expected run with .. in local include to fail, got success")
	}
	if !strings.Contains(errOut, "traverse") && !strings.Contains(errOut, "..") {
		t.Errorf("expected error to mention traversal, got: %s", errOut)
	}
}

// TestInstall_TransitiveAgentsMd verifies the instructions composition rule
// for AGENTS.md / instructions.md: the harness's own instructions come first,
// byte for byte, and each include that has instructions follows as a section
// headed "## Instructions from <label>". An include never replaces or reorders
// the harness's own text, and "instructions": false on the include drops its
// section. The label never carries an absolute path: an
// include without a manifest is labelled by its directory name.
func TestInstall_TransitiveAgentsMd(t *testing.T) {
	for _, tc := range []struct {
		name         string
		instructions string // JSON fragment added to the include entry
		wantInclude  bool
	}{
		{"composed", "", true},
		{"opted out", `, "instructions": false`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testTransitiveAgentsMd(t, tc.instructions, tc.wantInclude)
		})
	}
}

func testTransitiveAgentsMd(t *testing.T, instructions string, wantInclude bool) {
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
	if err := os.MkdirAll(filepath.Join(harness, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "transitive",
  "version": "0.1.0",
  "includes": [{"local": %q%s}]
}
`, include, instructions)
	if err := os.WriteFile(filepath.Join(harness, ".agents/harness", "plugin.json"), []byte(manifest), 0o644); err != nil {
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
	mustRunYnhInDir(t, s, project, "run", "local/transitive", "-v", "cursor", "--install")

	body, err := os.ReadFile(filepath.Join(s.home, "run", "transitive", ".cursorrules"))
	if err != nil {
		t.Fatalf("reading .cursorrules: %v", err)
	}
	got := string(body)
	if !strings.HasPrefix(got, "HARNESS_INSTRUCTIONS\n") {
		t.Errorf(".cursorrules should start with the harness's own instructions, got:\n%s", got)
	}
	if strings.Contains(got, include) || strings.Contains(got, filepath.Dir(include)) {
		t.Errorf(".cursorrules leaks the include's absolute path:\n%s", got)
	}
	if !wantInclude {
		if strings.Contains(got, "INCLUDE_INSTRUCTIONS") || strings.Contains(got, "## Instructions from") {
			t.Errorf(`.cursorrules has include instructions despite "instructions": false:\n%s`, got)
		}
		return
	}
	want := "HARNESS_INSTRUCTIONS\n\n## Instructions from include-source\n\nINCLUDE_INSTRUCTIONS\n"
	if !strings.HasPrefix(got, want) {
		t.Errorf(".cursorrules should start with %q, got:\n%s", want, got)
	}
}
