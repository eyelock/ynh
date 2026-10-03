//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadCommands_RefuseLegacyHarnessJson covers #406: a source tree whose
// only manifest is the legacy `.harness.json` is refused by every read
// command, with the fix, and left byte-identical. Only `ynd migrate` converts
// it, after which the same commands succeed.
func TestReadCommands_RefuseLegacyHarnessJson(t *testing.T) {
	s := newSandbox(t)

	srcDir := filepath.Join(t.TempDir(), "legacy")
	if err := os.MkdirAll(filepath.Join(srcDir, "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"$schema":"https://eyelock.github.io/ynh/schema/harness.schema.json","name":"legacy","version":"0.1.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(srcDir, ".harness.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: hello\ndescription: Say hello.\n---\n\nSay hello.\n"
	if err := os.WriteFile(filepath.Join(srcDir, "skills", "hello", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	before := treeListing(t, srcDir)
	want := srcDir + " uses the legacy .harness.json manifest, which ynh no longer reads; convert it with: ynd migrate " + srcDir

	outDir := filepath.Join(t.TempDir(), "out")
	reads := []struct {
		name string
		run  func() (string, string, error)
	}{
		{"ynd validate", func() (string, string, error) { return runYnd(t, "validate", srcDir) }},
		{"ynd preview", func() (string, string, error) { return runYnd(t, "preview", "-v", "claude", "--harness", srcDir) }},
		{"ynd export", func() (string, string, error) { return runYnd(t, "export", "-o", outDir, srcDir) }},
		{"ynh install", func() (string, string, error) { return s.runYnh(t, "install", srcDir) }},
	}
	for _, r := range reads {
		out, errOut, err := r.run()
		if err == nil {
			t.Errorf("%s must fail on a legacy tree\nstdout:\n%s", r.name, out)
		}
		if !strings.Contains(out+errOut, want) {
			t.Errorf("%s should name the fix %q\nstdout:\n%s\nstderr:\n%s", r.name, want, out, errOut)
		}
		if got := treeListing(t, srcDir); got != before {
			t.Fatalf("%s changed the source tree\nbefore:\n%s\nafter:\n%s", r.name, before, got)
		}
	}

	out, _ := mustRunYnd(t, "migrate", "--dry-run", srcDir)
	if !strings.Contains(out, srcDir) || treeListing(t, srcDir) != before {
		t.Fatalf("migrate --dry-run should list %s and change nothing:\n%s", srcDir, out)
	}
	mustRunYnd(t, "migrate", "-y", srcDir)
	if _, err := os.Stat(filepath.Join(srcDir, ".harness.json")); !os.IsNotExist(err) {
		t.Errorf(".harness.json should be gone after ynd migrate, err=%v", err)
	}
	assertFileExists(t, filepath.Join(srcDir, ".agents/harness", "plugin.json"))

	mustRunYnd(t, "validate", srcDir)
	mustRunYnd(t, "preview", "-v", "claude", "--harness", srcDir)
	mustRunYnd(t, "export", "-o", outDir, srcDir)
	s.mustRunYnh(t, "install", srcDir)

	out, _ = s.mustRunYnh(t, "ls", "--format", "json")
	var got envelopeLs
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing ls JSON: %v\n%s", err, out)
	}
	if len(got.Harnesses) != 1 || got.Harnesses[0].Name != "legacy" {
		t.Fatalf("expected one harness named 'legacy', got %+v", got.Harnesses)
	}
}

// treeListing returns every path under root with its contents, so two calls
// compare equal only if nothing in the tree was added, removed or changed.
func treeListing(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		b.WriteString(rel)
		if !d.IsDir() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			b.WriteString(" = ")
			b.Write(data)
		}
		b.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestInstall_BareAgentsMd asserts that a directory containing only an
// AGENTS.md file (no manifest) is installable — install synthesizes a
// minimal plugin.json named after the directory. Locks the documented
// "drop AGENTS.md in any folder and ynh install ./" entry point.
func TestInstall_BareAgentsMd(t *testing.T) {
	s := newSandbox(t)

	srcDir := filepath.Join(t.TempDir(), "bare-agents")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "AGENTS.md"),
		[]byte("# Bare\n\nInstructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.mustRunYnh(t, "install", srcDir)

	// Schema 3: synthesised plugin.json lands in the user's source tree
	// (which IS the install), no copy under HarnessesDir.
	assertFileExists(t, filepath.Join(srcDir, ".agents/harness", "plugin.json"))
	assertFileExists(t, filepath.Join(srcDir, "AGENTS.md"))

	out, _ := s.mustRunYnh(t, "ls", "--format", "json")
	var got envelopeLs
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing ls JSON: %v\n%s", err, out)
	}
	if len(got.Harnesses) != 1 || got.Harnesses[0].Name != "bare-agents" {
		t.Fatalf("expected one harness named 'bare-agents', got %+v", got.Harnesses)
	}
}

// TestInstall_RejectsNonHarnessDir asserts that installing a directory
// with no manifest, no AGENTS.md, and no instructions.md fails with a
// clear error message rather than producing a malformed install.
func TestInstall_RejectsNonHarnessDir(t *testing.T) {
	s := newSandbox(t)

	srcDir := filepath.Join(t.TempDir(), "not-a-harness")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Drop in a random file unrelated to the harness format.
	if err := os.WriteFile(filepath.Join(srcDir, "README.md"),
		[]byte("# Not a harness\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errOut, err := s.runYnh(t, "install", srcDir)
	if err == nil {
		t.Fatalf("expected install of non-harness dir to fail, got success")
	}
	if !strings.Contains(errOut, "manifest") && !strings.Contains(errOut, "AGENTS.md") {
		t.Errorf("expected error to mention manifest or AGENTS.md, got: %s", errOut)
	}
}

// TestInstall_ReinstallReplaces verifies that re-installing a local harness
// of the same name overwrites in place — no duplicates appear in `ynh ls`,
// and the install dir reflects the second source's content.
//
// Behavioural backstop for the alreadyInstalled / RemoveAll branch in the
// install flow.
func TestInstall_ReinstallReplaces(t *testing.T) {
	s := newSandbox(t)

	// First install — minimal harness.
	srcA := filepath.Join(t.TempDir(), "first")
	if err := os.MkdirAll(filepath.Join(srcA, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginA := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"twin","version":"0.1.0","description":"first"}`
	if err := os.WriteFile(filepath.Join(srcA, ".agents/harness", "plugin.json"), []byte(pluginA), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", srcA)

	// Second install — same name, different description, different source dir.
	srcB := filepath.Join(t.TempDir(), "second")
	if err := os.MkdirAll(filepath.Join(srcB, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginB := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"twin","version":"0.2.0","description":"second"}`
	if err := os.WriteFile(filepath.Join(srcB, ".agents/harness", "plugin.json"), []byte(pluginB), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", srcB)

	// ls must show exactly one entry — the second one.
	out, _ := s.mustRunYnh(t, "ls", "--format", "json")
	var got envelopeLs
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing ls JSON: %v\n%s", err, out)
	}
	if len(got.Harnesses) != 1 {
		t.Fatalf("expected 1 harness after reinstall, got %d: %+v", len(got.Harnesses), got.Harnesses)
	}
	h := got.Harnesses[0]
	assertEqual(t, "name", h.Name, "twin")
	assertEqual(t, "version_installed", h.VersionInstalled, "0.2.0")
	assertEqual(t, "description", h.Description, "second")
}
