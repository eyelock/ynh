//go:build unix

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var grantVendors = []struct{ name, heading string }{
	{"claude", "Claude Code"},
	{"codex", "Codex"},
	{"copilot", "Copilot"},
	{"cursor", "Cursor"},
	{"opencode", "OpenCode"},
}

// Only decideTrust compares a trust record: no other file reads the store's
// harness records, apart from the trust commands that write and report them
// and the gate internals decideTrust is built from (ynh #1006).
func TestOnlyDecideTrustComparesTrustRecords(t *testing.T) {
	allowed := map[string]string{
		"trust.go":        "the trust commands and trustStatusFor",
		"trust_routes.go": "the routes that write a record: install --trust, update --trust, run --trust",
		"trust_gate.go":   "installedGate and folderGate, decideTrust's internals; acceptFolderIncludes",
		"admission.go":    "decideTrust itself",
	}
	re := regexp.MustCompile(`\.Harnesses\[`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] != "" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if re.MatchString(line) {
				t.Errorf("%s:%d reads a trust record outside decideTrust: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
	for _, name := range []string{"gateTrusted", "workspaceTrusted"} {
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			data, _ := os.ReadFile(f)
			if strings.Contains(string(data), "func "+name+"(") {
				t.Errorf("%s defines %s, which decideTrust replaced", f, name)
			}
		}
	}
}

func TestZeroAdmissionIsARefusalAndGrantsNothing(t *testing.T) {
	var adm Admission
	if adm.err() == nil {
		t.Error("a zero Admission is not a refusal: a path that skipped the decision would run unchecked")
	}
	if g := grantsFor(adm, "claude", true, false); g != (grants{}) {
		t.Errorf("a zero Admission grants %+v", g)
	}
}

func TestGrantsForEveryVendor(t *testing.T) {
	trusted := Admission{minted: true, Trusted: true}
	folder := Admission{minted: true}
	refused := Admission{minted: true, Trusted: true, Refusal: untrustedf("no")}
	for _, v := range grantVendors {
		t.Run(v.name, func(t *testing.T) {
			g := grantsFor(trusted, v.name, true, false)
			wantMCP := v.name == "claude" || v.name == "codex" || v.name == "copilot"
			if g.MCPApprove != wantMCP {
				t.Errorf("one-shot MCPApprove = %v, want %v", g.MCPApprove, wantMCP)
			}
			if g.CopilotOneShot != (v.name == "copilot") || g.CodexHooks != (v.name == "codex") || g.CursorTrust != (v.name == "cursor") {
				t.Errorf("vendor-specific grants wrong: %+v", g)
			}
			if !g.AutoApprove || !g.Workspace {
				t.Errorf("trusted harness lacks a grant that is for every vendor: %+v", g)
			}
			if g := grantsFor(trusted, v.name, false, false); g.MCPApprove || g.CopilotOneShot {
				t.Errorf("an interactive run got a one-shot grant: %+v", g)
			}
			if g := grantsFor(trusted, v.name, true, true); g.MCPApprove {
				t.Errorf("mcp_manual_approval still got MCP approval: %+v", g)
			}
			for name, adm := range map[string]Admission{"folder run": folder, "refused": refused} {
				if g := grantsFor(adm, v.name, true, false); g != (grants{}) {
					t.Errorf("%s granted %+v", name, g)
				}
			}
		})
	}
}

// The per-vendor grants table in docs/trust.md is the table grantsFor
// implements: one row per grant, one column per vendor, "granted" or "asks"
// ("-" where the grant is not a thing on that vendor), so the two cannot drift.
func TestTrustDocsGrantsTableMatchesGrantsFor(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "trust.md"))
	if err != nil {
		t.Fatal(err)
	}
	var header []string
	rows := map[string][]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "| "), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if cells[0] == "Grant" {
			header = cells
			continue
		}
		if header != nil && len(cells) == len(header) && !strings.HasPrefix(cells[0], "---") {
			rows[cells[0]] = cells
		}
	}
	if header == nil {
		t.Fatal("docs/trust.md has no grants table (a header row starting `| Grant |`)")
	}
	trusted := Admission{minted: true, Trusted: true}
	for _, row := range grantRows {
		cells, ok := rows[row.Label]
		if !ok {
			t.Errorf("docs/trust.md has no grants row %q", row.Label)
			continue
		}
		for _, v := range grantVendors {
			col := -1
			for i, h := range header {
				if h == v.heading {
					col = i
				}
			}
			if col < 0 {
				t.Errorf("docs/trust.md grants table has no column %q", v.heading)
				continue
			}
			want := row.Get(grantsFor(trusted, v.name, true, false))
			got := cells[col] == "granted"
			if got != want {
				t.Errorf("docs/trust.md says %q is %q on %s; grantsFor says granted=%v", row.Label, cells[col], v.heading, want)
			}
		}
	}
	if len(rows) != len(grantRows) {
		t.Errorf("docs/trust.md grants table has %d rows, grantsFor has %d", len(rows), len(grantRows))
	}
}

const mcpClaudeHarness = `{"name":"cl","version":"0.1.0","default_vendor":"claude",` +
	`"mcp_servers":{"web":{"url":"https://x/mcp"}}}`

const mcpClaudeManualHarness = `{"name":"cl","version":"0.1.0","default_vendor":"claude","mcp_manual_approval":true,` +
	`"mcp_servers":{"web":{"url":"https://x/mcp"}}}`

const wantClaudeRule = "--allowedTools=mcp__plugin_cl_web"

// A direct one-shot Claude Code run of a trusted harness is allowed its own
// MCP servers' tools; one that sets mcp_manual_approval, an interactive one
// and a folder run get none (ynh #1006, T7).
func TestRunOneShotClaudeOwnMCPRuleFollowsGrants(t *testing.T) {
	run := func(t *testing.T, manifest string, argv ...string) []string {
		isolateRun(t)
		t.Chdir(t.TempDir())
		id := installTrustedTestHarness(t, os.Getenv("YNH_HOME"), "cl", manifest)
		args := captureLaunch(t)
		if err := cmdRun(append([]string{id}, argv...)); err != nil {
			t.Fatal(err)
		}
		return *args
	}
	has := func(args []string) bool {
		for _, a := range args {
			if a == wantClaudeRule {
				return true
			}
		}
		return false
	}
	if args := run(t, mcpClaudeHarness, "--", "hi"); !has(args) {
		t.Errorf("a trusted one-shot Claude run got no rule for its own servers: %q", args)
	}
	if args := run(t, mcpClaudeManualHarness, "--", "hi"); has(args) {
		t.Errorf("mcp_manual_approval still got the rule: %q", args)
	}
	if args := run(t, mcpClaudeHarness, "--interactive", "--", "hi"); has(args) {
		t.Errorf("an interactive run got the rule: %q", args)
	}

	t.Run("folder run", func(t *testing.T) {
		isolateRun(t)
		t.Chdir(t.TempDir())
		dir := filepath.Join(t.TempDir(), "cl")
		writeSkillHarness(t, dir, "cl")
		if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(mcpClaudeHarness), 0o644); err != nil {
			t.Fatal(err)
		}
		args := captureLaunch(t)
		if err := cmdRun([]string{dir, "--", "hi"}); err != nil {
			t.Fatal(err)
		}
		if has(*args) {
			t.Errorf("a folder run got the rule: %q", *args)
		}
	})
}

// A purpose changes only the words of a refusal, never the decision.
func TestDecideTrustPurposeChangesOnlyTheWords(t *testing.T) {
	isolateRun(t)
	id := installListTestHarness(t, os.Getenv("YNH_HOME"), "mcp", mcpCodexHarness)
	p := loadInstalled(t, id)
	run := decideTrust(id, p, purposeRun, nil)
	image := decideTrust(id, p, purposeImage, nil)
	if run.Trusted || image.Trusted || run.err() == nil || image.err() == nil {
		t.Fatalf("an unaccepted harness was admitted: run=%v image=%v", run.err(), image.err())
	}
	if !strings.Contains(run.err().Error(), "refusing to run "+id) || !strings.Contains(image.err().Error(), "refusing to build an image of "+id) {
		t.Errorf("wording: %q / %q", run.err(), image.err())
	}
	if strings.Replace(image.err().Error(), "build an image of", "run", 1) != run.err().Error() {
		t.Errorf("the purposes differ by more than the words:\n%q\n%q", run.err(), image.err())
	}
}

// ynw-3fsvf: the grants follow the backend the loop resolved, so a resumed
// Codex worker of a trusted harness keeps its MCP approval even though the
// CLI was given no --backend.
func TestAgentAdmissionFollowsResolvedBackend(t *testing.T) {
	trusted := Admission{minted: true, Trusted: true}
	if a := agentAdmission(trusted, "codex"); a.Refusal != nil || !a.MCPApprove || !a.AutoApprove {
		t.Errorf("codex: %+v, want MCP and auto-approve granted", a)
	}
	if a := agentAdmission(trusted, "cursor"); a.MCPApprove {
		t.Errorf("cursor: MCP approved: %+v", a)
	}
	if a := agentAdmission(Admission{}, "codex"); a.Refusal == nil || a.MCPApprove {
		t.Errorf("zero Admission: %+v, want a refusal", a)
	}
}
