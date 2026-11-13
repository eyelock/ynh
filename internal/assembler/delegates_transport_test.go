package assembler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

// agentFilesOf lists the files of the vendor's agents directory under workDir.
func agentFilesOf(t *testing.T, workDir string, adapter vendor.Adapter) []string {
	t.Helper()
	dir := filepath.Join(workDir, adapter.ConfigDir(), adapter.ArtifactDirs()["agents"])
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A native delegate is assembled as a subagent and an a2a one is assembled as
// none, on every vendor with subagents. a2a is the default: an entry that says
// nothing, with no harness default, gets no agent file and no launch file.
func TestAssembleDelegates_TransportDecidesTheSubagent(t *testing.T) {
	for _, name := range []string{"claude", "codex", "copilot", "opencode"} {
		adapter, err := vendor.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			label         string
			entry         string // the entry's transport
			rootTransport string
			wantAgent     bool
		}{
			{"entry native", harness.TransportNative, "", true},
			{"entry a2a", harness.TransportA2A, "", false},
			{"neither says: a2a", "", "", false},
			{"harness default native", "", harness.TransportNative, true},
			{"entry a2a beats a native default", harness.TransportA2A, harness.TransportNative, false},
			{"entry native beats an a2a default", harness.TransportNative, harness.TransportA2A, true},
		}
		for _, tc := range cases {
			t.Run(name+"/"+tc.label, func(t *testing.T) {
				t.Setenv("YNH_HOME", t.TempDir())
				repo := delegateRepo(t, t.TempDir(), map[string]any{
					"name": "probe", "version": "0.1.0", "description": "Probes things.",
					"mcp_servers": map[string]any{"b": map[string]any{"command": "/bin/b"}},
				}, "lint")
				workDir := t.TempDir()
				var warn strings.Builder
				declared, err := AssembleDelegates(workDir, adapter,
					[]harness.Delegate{{Transport: tc.entry, GitSource: harness.GitSource{Git: repo}}}, "",
					DelegateOptions{Warn: &warn, Launch: true, RootTransport: tc.rootTransport})
				if err != nil {
					t.Fatal(err)
				}
				files := agentFilesOf(t, workDir, adapter)
				if tc.wantAgent {
					if len(files) != 1 {
						t.Fatalf("a native delegate must be one subagent, got %v", files)
					}
					return
				}
				if len(files) != 0 || len(declared) != 0 {
					t.Errorf("an a2a delegate must be assembled as no subagent: files %v, declared %v", files, declared)
				}
				if _, err := os.Stat(filepath.Join(workDir, vendor.ClaudeDelegateAgentsFile)); err == nil {
					t.Error("an a2a delegate must get no launch entry")
				}
				if warn.Len() != 0 {
					t.Errorf("an a2a delegate has nothing to warn about: %q", warn.String())
				}
			})
		}
	}
}

// An a2a delegate's own delegates are its session's business: they are not
// reached from the parent, whatever their transport. A native delegate's own
// entries follow their own transport.
func TestAssembleDelegates_TransportGovernsTheWalk(t *testing.T) {
	adapter, _ := vendor.Get("claude")
	graphWith := func(t *testing.T, midTransport, leafTransport string) map[string]string {
		t.Helper()
		leaf, mid, root := t.TempDir(), t.TempDir(), t.TempDir()
		delegateRepo(t, leaf, map[string]any{"name": "leaf", "version": "0.1.0", "description": "leaf"})
		delegateRepo(t, mid, map[string]any{"name": "mid", "version": "0.1.0", "description": "mid",
			"delegates_to": []any{map[string]any{"git": leaf, "transport": leafTransport}}})
		delegateRepo(t, root, map[string]any{"name": "root", "version": "0.1.0"})
		return map[string]string{"leaf": leaf, "mid": mid, "root": root}
	}
	assemble := func(t *testing.T, dirs map[string]string, midTransport string) []string {
		t.Helper()
		t.Setenv("YNH_HOME", t.TempDir())
		workDir := t.TempDir()
		_, err := AssembleDelegates(workDir, adapter,
			[]harness.Delegate{{Transport: midTransport, GitSource: harness.GitSource{Git: dirs["mid"]}}},
			dirs["root"], DelegateOptions{RootName: "root"})
		if err != nil {
			t.Fatal(err)
		}
		return agentFilesOf(t, workDir, adapter)
	}
	t.Run("native then native: both", func(t *testing.T) {
		if got := assemble(t, graphWith(t, "native", "native"), "native"); len(got) != 2 {
			t.Errorf("want mid and leaf, got %v", got)
		}
	})
	t.Run("native then a2a: only the native one", func(t *testing.T) {
		if got := assemble(t, graphWith(t, "native", "a2a"), "native"); len(got) != 1 || got[0] != "mid.md" {
			t.Errorf("want mid alone, got %v", got)
		}
	})
	t.Run("a2a then native: none, the walk stops at the a2a delegate", func(t *testing.T) {
		if got := assemble(t, graphWith(t, "a2a", "native"), "a2a"); len(got) != 0 {
			t.Errorf("want none, got %v", got)
		}
	})
}
