package vendor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// TestCodexHookHashMatchesCodex pins the hash against values Codex itself
// reported as currentHash (hooks/list, codex-cli 0.166.0, 2026-11-04). If this
// fails after a Codex upgrade, Codex changed its algorithm: re-derive it.
func TestCodexHookHashMatchesCodex(t *testing.T) {
	cases := []struct {
		name string
		id   codexHookIdentity
		want string
	}{
		{"user hook, no matcher", codexHookIdentity{"session_start", "", "/Users/david/.superset/hooks/notify.sh", 600},
			"sha256:11620ec140653f63491d7488ffd8a816eb7954c24a971d8578808d3d2a6082bd"},
		{"matcher", codexHookIdentity{"pre_tool_use", "Bash", "echo bt", 600},
			"sha256:fbcd7a37252bc9417b2bcfefeb8a892fc7dfee379e4c151a32451110e5fe38f1"},
		{"regex matcher", codexHookIdentity{"post_tool_use", "Bash|Edit", "echo at", 600},
			"sha256:28ac0fb7b94d549a41740b652465b7643a545d351ab8aa23c5cfb027cb9b368a"},
		{"html and non-ascii in the command", codexHookIdentity{"session_start", "", `echo A <&> "q" é`, 600},
			"sha256:7a519379fb5a6805c1ae70fb77e55f75a89b27f169c0473bb6594e308afb8d9f"},
		{"explicit timeout", codexHookIdentity{"user_prompt_submit", "", "echo p", 7},
			"sha256:6ede7a6c7a29ecc6abea63085d9fb5690adaf280fb025c95aad87a91bc7e2281"},
		{"session_end", codexHookIdentity{"session_end", "", "echo end", 1},
			"sha256:45850818ac37352c413a1b5998816007fb7be904214afb35e02f4b66d14c787a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.hash(); got != tc.want {
				t.Errorf("hash = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCodexHookTrustArgs(t *testing.T) {
	runDir := t.TempDir()
	real, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := (&Codex{}).GenerateHookConfig(map[string][]plugin.HookEntry{
		"before_tool":      {{Command: "echo bt", Matcher: "Bash"}, {Command: "echo bt2", Matcher: "Bash"}, {Command: "echo bt3"}},
		"on_session_end":   {{Command: "echo end"}},
		"before_prompt":    {{Command: "echo p", Timeout: 7}},
		"on_session_start": {{Command: "echo s"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(runDir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runDir, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	args := CodexHookTrustArgs(runDir)
	if len(args) != 4 || args[0] != "-c" || args[2] != "-c" {
		t.Fatalf("args = %q, want two -c overrides", args)
	}
	if want := `projects={"` + real + `"={trust_level="trusted"}}`; args[1] != want {
		t.Errorf("project override = %q, want %q", args[1], want)
	}
	state := args[3]
	if !strings.HasPrefix(state, "hooks.state={") {
		t.Fatalf("state override = %q", state)
	}
	for _, want := range []string{
		`.codex/hooks.json:pre_tool_use:0:0"={trusted_hash="` + codexHookIdentity{"pre_tool_use", "Bash", "echo bt", 600}.hash(),
		`.codex/hooks.json:pre_tool_use:0:1"={trusted_hash="` + codexHookIdentity{"pre_tool_use", "Bash", "echo bt2", 600}.hash(),
		`.codex/hooks.json:pre_tool_use:1:0"={trusted_hash="` + codexHookIdentity{"pre_tool_use", "", "echo bt3", 600}.hash(),
		`.codex/hooks.json:session_end:0:0"={trusted_hash="` + codexHookIdentity{"session_end", "", "echo end", 1}.hash(),
		`.codex/hooks.json:user_prompt_submit:0:0"={trusted_hash="` + codexHookIdentity{"user_prompt_submit", "", "echo p", 7}.hash(),
		`.codex/hooks.json:session_start:0:0"={trusted_hash="` + codexHookIdentity{"session_start", "", "echo s", 600}.hash(),
	} {
		if !strings.Contains(state, want) {
			t.Errorf("state override lacks %q\n%s", want, state)
		}
	}
}

func TestCodexHookTrustArgsNoHooks(t *testing.T) {
	runDir := t.TempDir()
	if args := CodexHookTrustArgs(runDir); args != nil {
		t.Errorf("no hook file: args = %q, want none", args)
	}
	if err := os.MkdirAll(filepath.Join(runDir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, ".codex", "hooks.json"), []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if args := CodexHookTrustArgs(runDir); args != nil {
		t.Errorf("empty hook file: args = %q, want none", args)
	}
}
