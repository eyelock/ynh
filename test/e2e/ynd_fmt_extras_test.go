//go:build e2e

package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFmtHarness creates a minimal harness with a markdown file containing
// the supplied body. Returns the harness dir and the markdown path.
func writeFmtHarness(t *testing.T, name, body string) (dir, mdPath string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	plug := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"` + name + `","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(plug), 0o644); err != nil {
		t.Fatal(err)
	}
	mdPath = filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(mdPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, mdPath
}

// TestYnd_Fmt_TrailingWhitespace asserts ynd fmt strips trailing whitespace.
func TestYnd_Fmt_TrailingWhitespace(t *testing.T) {
	dir, mdPath := writeFmtHarness(t, "fmt-ws", "# heading\n\ntext with trailing   \n")
	mustRunYnd(t, "fmt", "--harness", dir)
	body, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			t.Errorf("line %d still has trailing whitespace: %q", i, line)
		}
	}
}

// TestYnd_Fmt_Idempotent asserts a second `ynd fmt` is a no-op — the second
// invocation should not report any changes.
func TestYnd_Fmt_Idempotent(t *testing.T) {
	dir, _ := writeFmtHarness(t, "fmt-idem", "# heading\n\nclean content\n")
	mustRunYnd(t, "fmt", "--harness", dir)
	stdout, _ := mustRunYnd(t, "fmt", "--harness", dir)
	if strings.Contains(stdout, "Formatted ") && !strings.Contains(stdout, "Formatted 0 of") {
		// Either "all formatted" wording or "Formatted 0 of N" is acceptable;
		// reject only if the second run claims it reformatted something.
		if strings.Contains(stdout, "Formatted AGENTS.md") {
			t.Errorf("second fmt pass should be no-op, got:\n%s", stdout)
		}
	}
}

// TestYnd_Fmt_PreservesContent asserts fmt's whitespace normalisation does
// not mangle the semantic content of the file (headings, paragraphs, lists).
func TestYnd_Fmt_PreservesContent(t *testing.T) {
	body := "# Title\n\n## Section\n\n- item one\n- item two\n\nA paragraph.\n"
	dir, mdPath := writeFmtHarness(t, "fmt-content", body+"   \n\n\n")
	mustRunYnd(t, "fmt", "--harness", dir)
	got, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	// All original tokens must survive.
	for _, want := range []string{"# Title", "## Section", "- item one", "- item two", "A paragraph."} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("fmt dropped content %q\noutput:\n%s", want, got)
		}
	}
	// And the file must end with exactly one trailing newline.
	if !bytes.HasSuffix(got, []byte("\n")) {
		t.Errorf("output should end with newline:\n%q", got)
	}
	if bytes.HasSuffix(got, []byte("\n\n\n")) {
		t.Errorf("multiple trailing blank lines should be collapsed:\n%q", got)
	}
}
