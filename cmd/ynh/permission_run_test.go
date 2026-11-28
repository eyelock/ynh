package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/permission"
)

func noEnv(string) string { return "" }

func envOf(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }

// A golden per vendor and level: what a one-shot run of a harness that
// declares that level is passed. Provisional cells follow ynh #1059's final
// results; the table is permission.For.
func TestResolveRunPermission_GoldenPerVendor(t *testing.T) {
	for _, v := range []string{"claude", "codex", "cursor", "opencode", "copilot"} {
		for _, l := range []permission.Level{permission.None, permission.Edits, permission.All} {
			t.Run(v+"/"+string(l), func(t *testing.T) {
				p := &harness.Harness{Name: "h", AutoApprove: string(l)}
				rp, err := resolveRunPermission(p, v, "", t.TempDir(), false, noEnv, 1000)
				want, werr := permission.For(v, l)
				if werr != nil {
					if err == nil || !strings.Contains(err.Error(), "cannot hold") {
						t.Fatalf("want a refusal, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(rp.Args, want.Args) || !slices.Equal(rp.Env, want.Env) || rp.Level != l {
					t.Errorf("got %+v, want %+v", rp, want)
				}
			})
		}
	}
}

func TestResolveRunPermission_UndeclaredOpensAtAll(t *testing.T) {
	rp, err := resolveRunPermission(&harness.Harness{Name: "h"}, "claude", "", t.TempDir(), false, noEnv, 1000)
	if err != nil || rp.Level != permission.All || rp.From != permission.FromDefault {
		t.Fatalf("%+v %v", rp, err)
	}
	if !slices.Equal(rp.Args, []string{"--permission-mode", "bypassPermissions"}) {
		t.Errorf("args = %q", rp.Args)
	}
}

func TestResolveRunPermission_Narrowing(t *testing.T) {
	p := &harness.Harness{Name: "h", AutoApprove: "edits"}
	// The flag narrows, with its own source.
	rp, err := resolveRunPermission(p, "claude", "none", t.TempDir(), false, noEnv, 1000)
	if err != nil || rp.Level != permission.None || rp.From != permission.FromFlag {
		t.Fatalf("%+v %v", rp, err)
	}
	// A wider flag is refused.
	if _, err := resolveRunPermission(p, "claude", "all", t.TempDir(), false, noEnv, 1000); err == nil || !strings.Contains(err.Error(), "wider than the ceiling") {
		t.Errorf("wider flag must be refused: %v", err)
	}
	// An unknown word is refused.
	if _, err := resolveRunPermission(p, "claude", "most", t.TempDir(), false, noEnv, 1000); err == nil {
		t.Error("unknown level must be refused")
	}
	// The caller's cap narrows (D6), and says so.
	rp, err = resolveRunPermission(&harness.Harness{Name: "h"}, "codex", "", t.TempDir(), true, envOf(map[string]string{permission.CapEnv: "none"}), 1000)
	if err != nil || rp.Level != permission.None || rp.From != permission.FromCallerCap {
		t.Fatalf("%+v %v", rp, err)
	}
	// A delegate's own declaration is delegate_default; a request source comes from the entry or call.
	rp, _ = resolveRunPermission(p, "claude", "", t.TempDir(), true, noEnv, 1000)
	if rp.From != permission.FromDelegateDefault {
		t.Errorf("from = %s", rp.From)
	}
	rp, _ = resolveRunPermission(p, "claude", "none", t.TempDir(), true, envOf(map[string]string{permission.SourceEnv: "entry"}), 1000)
	if rp.From != "entry" {
		t.Errorf("from = %s", rp.From)
	}
}

func TestResolveRunPermission_ProjectModeAndRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(`{"permissions":{"defaultMode":"plan"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rp, err := resolveRunPermission(&harness.Harness{Name: "h"}, "claude", "", dir, false, noEnv, 1000)
	if err != nil || rp.Level != permission.None || rp.From != permission.FromProject || len(rp.Args) != 0 {
		t.Fatalf("%+v %v", rp, err)
	}
	if !strings.Contains(strings.Join(rp.Lines, "\n"), "settings.json") {
		t.Errorf("no project line: %q", rp.Lines)
	}
	if _, err := resolveRunPermission(&harness.Harness{Name: "h"}, "claude", "", t.TempDir(), false, noEnv, 0); err == nil || !strings.Contains(err.Error(), "as root") {
		t.Errorf("root at all must be refused: %v", err)
	}
	if _, err := resolveRunPermission(&harness.Harness{Name: "h"}, "claude", "edits", t.TempDir(), false, noEnv, 0); err != nil {
		t.Errorf("root at edits is allowed: %v", err)
	}
}

func TestMergeJSONObject(t *testing.T) {
	got, err := mergeJSONObject(`{"mcp":{"a":1},"permission":{"x":1}}`, `{"permission":{"edit":"deny"}}`)
	if err != nil || got != `{"mcp":{"a":1},"permission":{"edit":"deny"}}` {
		t.Errorf("%s %v", got, err)
	}
	if _, err := mergeJSONObject(`[`, `{}`); err == nil {
		t.Error("a non-object must fail")
	}
}

// An interactive run with --auto-approve passes the vendor nothing and caps
// the delegates it starts (S4).
func TestRun_InteractiveFlagCapsDelegates(t *testing.T) {
	isolateRun(t)
	t.Chdir(t.TempDir())
	t.Setenv(permission.CapEnv, "")
	dir := filepath.Join(t.TempDir(), "cop")
	writeSkillHarness(t, dir, "cop")
	var got []string
	var capSeen string
	stubLaunchArgs(t, func(a []string) error { got = a; capSeen = os.Getenv(permission.CapEnv); return nil })
	if err := cmdRun([]string{dir, "-v", "claude", "--auto-approve", "edits"}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got, "--permission-mode") {
		t.Errorf("an interactive run is unchanged: %q", got)
	}
	if capSeen != "edits" {
		t.Errorf("cap = %q, want edits", capSeen)
	}
}

// ynh run reads --auto-approve itself and never passes it on to the vendor.
func TestParseRunArgs_AutoApproveIsNotForwarded(t *testing.T) {
	for _, args := range [][]string{
		{"h", "--auto-approve", "edits", "--", "task"},
		{"h", "--auto-approve=edits", "--", "task"},
		{"h", "--auto-approve", "edits", "task"},
	} {
		ra := parseRunArgs(args)
		if ra.AutoApprove != "edits" || ra.HarnessName != "h" || ra.Prompt != "task" || len(ra.VendorArgs) != 0 {
			t.Errorf("%q: %+v", args, ra)
		}
	}
}

func TestDelegateRunArgs_CarryTheLevel(t *testing.T) {
	rec := delegation.Record{Delegate: "local/rev", Task: "go", AutoApprove: "none", AutoApproveFrom: "call"}
	got := delegateRunArgs(&rec)
	want := []string{"run", "local/rev", "--auto-approve", "none", "--", "go"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
	if ra := parseRunArgs(got[1:]); ra.AutoApprove != "none" || len(ra.VendorArgs) != 0 {
		t.Errorf("ynh run reads %+v", ra)
	}
	cmd := delegateLauncher(&rec)
	if !slices.Contains(cmd.Env, permission.SourceEnv+"=call") {
		t.Errorf("the launcher does not carry the level's source")
	}
}

// The ladder of a delegate's level (D2, D6): its own, then the entry, then the
// calling session's cap; a wider entry is refused.
func TestDelegateStep(t *testing.T) {
	declared := &harness.Harness{Name: "d", AutoApprove: "edits"}
	undeclared := &harness.Harness{Name: "d"}
	step, err := delegateStep(undeclared, harness.Delegate{}, noEnv)
	if err != nil || step.Level != permission.All || step.From != permission.FromDefault {
		t.Errorf("%+v %v", step, err)
	}
	step, _ = delegateStep(declared, harness.Delegate{}, noEnv)
	if step.Level != permission.Edits || step.From != permission.FromDelegateDefault {
		t.Errorf("%+v", step)
	}
	step, _ = delegateStep(declared, harness.Delegate{AutoApprove: "none"}, noEnv)
	if step.Level != permission.None || step.From != permission.FromEntry {
		t.Errorf("%+v", step)
	}
	step, _ = delegateStep(undeclared, harness.Delegate{AutoApprove: "edits"}, envOf(map[string]string{permission.CapEnv: "none"}))
	if step.Level != permission.None || step.From != permission.FromCallerCap {
		t.Errorf("%+v", step)
	}
	if _, err := delegateStep(declared, harness.Delegate{AutoApprove: "all"}, noEnv); err == nil || !strings.Contains(err.Error(), "wider than the ceiling") {
		t.Errorf("a wider entry must be refused: %v", err)
	}
}
