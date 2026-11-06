package docs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture is a small parity page and two vendor adapters, written for the
// test, so the real page can change without breaking it. The real page is
// checked by make check-vendor-parity (assertion H).
const parityVendorsJSON = `[{"name":"claude","display_name":"Claude Code"},{"name":"opencode","display_name":"OpenCode"}]`

const parityPage = `# Vendor Feature Parity

## ynh run

| Feature | Claude Code | OpenCode |
|---|---|---|
| Hooks | Y | P [L9](#l9-opencode-has-no-session-end-event) |
| Workspace folders | Y | Y |
| Telemetry | N [L13](#l13-ynh-run-emits-no-telemetry) | X [L27](#l27-opencode-reads-no-packages) |
| Delegates | Y | – |

## Limits

### L9: OpenCode has no session-end event

- **Owner:** the vendor.
- **Evidence:** a live run.
- **Tracking:** none.

### L13: ynh run emits no telemetry

- **Owner:** ynh.
- **Evidence:** the telemetry page.
- **Tracking:** [ynh #673](https://github.com/eyelock/ynh/issues/673), v0.19.0.

### L27: OpenCode reads no packages

- **Owner:** the vendor.
- **Evidence:** the export page.
- **Tracking:** none.
`

const parityClaudeGo = `package vendor

var claudeHookEventMap = map[string]string{
	"before_tool": "PreToolUse",
}

var claudeUnsupportedHookEvents = map[string]string{}

var claudeSessionStartContext = "plain"

var claudeWorkspaceFlag = "--add-dir"
var claudeWorkspaceUnsupported = ""
`

const parityOpenCodeGo = `package vendor

var opencodeHookEventMap = map[string]string{
	"before_tool": "tool.execute.before",
}

var opencodeUnsupportedHookEvents = map[string]string{
	"on_session_end": "no session-end event",
}

var opencodeSessionStartContext = "plain"

var opencodeWorkspaceFlag = ""
var opencodeWorkspaceUnsupported = ""
var opencodeWorkspaceDefault = "an allow rule in each agent file"
`

// parityPageFixture writes the fixture into a temporary repository root and
// returns it with the path to the script under test.
func parityPageFixture(t *testing.T) (root, script string) {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "parity-page.sh"))
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	for rel, body := range map[string]string{
		"vendors.json":                parityVendorsJSON,
		"docs/vendor-parity.md":       parityPage,
		"internal/vendor/claude.go":   parityClaudeGo,
		"internal/vendor/opencode.go": parityOpenCodeGo,
	} {
		writeFixture(t, root, rel, body)
	}
	return root, script
}

func writeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	dst := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceFixture(t *testing.T, root, rel, old, repl string) {
	t.Helper()
	path := filepath.Join(root, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", rel, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, repl, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runParityPage(root, script string) (string, error) {
	out, err := exec.Command("bash", script, root, filepath.Join(root, "vendors.json")).CombinedOutput()
	return string(out), err
}

// TestParityPageScript: assertion H passes on a page that agrees with ynh and
// fails on each way the page can drift from it (ynh #718).
func TestParityPageScript(t *testing.T) {
	root, script := parityPageFixture(t)
	if out, err := runParityPage(root, script); err != nil {
		t.Fatalf("the fixture page must pass: %v\n%s", err, out)
	}

	const page = "docs/vendor-parity.md"
	tests := []struct {
		name    string
		edit    func(root string)
		wantMsg string
	}{
		{"a vendor with no column", func(r string) {
			writeFixture(t, r, "vendors.json", `[{"name":"claude","display_name":"Claude Code"},{"name":"opencode","display_name":"OpenCode"},{"name":"zed","display_name":"Zed"}]`)
		}, "has no column for zed"},
		{"a column for no vendor", func(r string) {
			writeFixture(t, r, "vendors.json", `[{"name":"claude","display_name":"Claude Code"}]`)
		}, `a column "OpenCode" for a vendor ynh vendors does not report`},
		{"columns out of order", func(r string) {
			writeFixture(t, r, "vendors.json", `[{"name":"opencode","display_name":"OpenCode"},{"name":"claude","display_name":"Claude Code"}]`)
		}, "has opencode in column 2"},
		{"a bad cell value", func(r string) {
			replaceFixture(t, r, page, "| Delegates | Y |", "| Delegates | Yes |")
		}, `"Yes" is not a legend mark`},
		{"a non-Y cell with no limit", func(r string) {
			replaceFixture(t, r, page, "X [L27](#l27-opencode-reads-no-packages) |", "X |")
		}, "X names no limit"},
		{"a limit that does not exist", func(r string) {
			replaceFixture(t, r, page, "X [L27](#l27", "X [L99](#l99")
		}, "names L99, which has no ### L99: heading"},
		{"a limit with no owner", func(r string) {
			replaceFixture(t, r, page, "- **Owner:** ynh.\n", "")
		}, "L13 has no Owner"},
		{"a limit with no evidence", func(r string) {
			replaceFixture(t, r, page, "- **Evidence:** a live run.\n", "")
		}, "L9 has no Evidence"},
		{"an N limit with no issue", func(r string) {
			replaceFixture(t, r, page, "- **Tracking:** [ynh #673](https://github.com/eyelock/ynh/issues/673), v0.19.0.", "- **Tracking:** none.")
		}, "is N, and L13 has no Tracking line linking a ynh issue"},
		{"a hook cell the adapter contradicts", func(r string) {
			replaceFixture(t, r, page, "| Hooks | Y | P [L9](#l9-opencode-has-no-session-end-event) |", "| Hooks | Y | Y |")
		}, "Hooks / opencode: Y, but internal/vendor/opencode.go declares it unsupported"},
		{"a workspace cell the adapter contradicts", func(r string) {
			replaceFixture(t, r, page, "| Workspace folders | Y | Y |", "| Workspace folders | X [L27](#l27-opencode-reads-no-packages) | Y |")
		}, "Workspace folders / claude: X, but internal/vendor/claude.go declares it supported"},
		{"a second parity table", func(r string) {
			writeFixture(t, r, "docs/other.md", "# Other\n\n| Feature | Claude Code | OpenCode |\n|---|---|---|\n| Hooks | Y | Y |\n")
		}, "COPY     docs/other.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, s := parityPageFixture(t)
			tc.edit(r)
			out, err := runParityPage(r, s)
			if err == nil {
				t.Fatalf("want failure, got success\n%s", out)
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Errorf("output lacks %q:\n%s", tc.wantMsg, out)
			}
		})
	}
}
