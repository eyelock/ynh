package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// readSrtSettings reads back the settings file srtCommand wrote for a run.
func readSrtSettings(t *testing.T, path string) srtSettings {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("settings file not written: %v", err)
	}
	var s srtSettings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// ynh agent run --sandbox srt on codex and cursor wraps each turn's process
// in srt with that vendor's hosts and state, and nothing of Claude Code's.
func TestWorkerCommand_SrtPerVendor(t *testing.T) {
	fakeProgram(t, "srt", "", "", 0)
	home := t.TempDir()
	cases := []struct {
		name   string
		policy func([]string) srtPolicy
		hosts  []string
		state  string
		weak   bool
	}{
		{"codex", codexSrtPolicy, []string{"api.openai.com", "auth.openai.com", "chatgpt.com"}, filepath.Join(home, ".codex"), true},
		{"cursor", cursorSrtPolicy, []string{"*.cursor.sh", "cursor.com", "*.cursor.com"}, filepath.Join(home, ".cursor"), true},
		{"opencode", opencodeSrtPolicy, []string{"opencode.ai", "*.opencode.ai", "models.dev"}, filepath.Join(home, ".local/share/opencode"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			session := t.TempDir()
			opts := StartOptions{Sandbox: "srt", WorktreeDir: t.TempDir(), SessionDir: session, Env: []string{"HOME=" + home}}
			cmd, cleanup, err := workerCommand(context.Background(), opts, c.policy, "/bin/vendor", []string{"exec", "--settings", "x"})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			want := []string{"--settings", filepath.Join(session, srtSettingsFile), "--", "/bin/vendor", "exec", "--settings", "x"}
			if !slices.Equal(cmd.Args[1:], want) {
				t.Fatalf("srt args = %q, want %q", cmd.Args[1:], want)
			}
			s := readSrtSettings(t, filepath.Join(session, srtSettingsFile))
			if !slices.Equal(s.Network.AllowedDomains, c.hosts) {
				t.Errorf("allowed domains = %q, want %q", s.Network.AllowedDomains, c.hosts)
			}
			if !slices.Contains(s.Filesystem.AllowWrite, realPath(c.state)) {
				t.Errorf("writes = %q, want %q among them", s.Filesystem.AllowWrite, realPath(c.state))
			}
			if s.EnableWeakerNetworkIsolation != c.weak {
				t.Errorf("enableWeakerNetworkIsolation = %v, want %v", s.EnableWeakerNetworkIsolation, c.weak)
			}
		})
	}
}

// Claude Code never gets the weaker network isolation; the key is left out.
func TestSrtSettings_ClaudeLeavesWeakerIsolationOut(t *testing.T) {
	s := buildSrtSettings(t.TempDir(), "/x/srt-settings.json", claudeSrtPolicy([]string{"HOME=" + t.TempDir()}))
	data, _ := json.Marshal(s)
	if contains(string(data), "enableWeakerNetworkIsolation") {
		t.Errorf("claude's settings carry enableWeakerNetworkIsolation: %s", data)
	}
}

func TestWorkerCommand_NoSandboxIsTheBinary(t *testing.T) {
	cmd, cleanup, err := workerCommand(context.Background(), StartOptions{}, codexSrtPolicy, "/bin/codex", []string{"exec"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !slices.Equal(cmd.Args, []string{"/bin/codex", "exec"}) {
		t.Errorf("args = %q", cmd.Args)
	}
}

// Without srt on PATH the run stops; it never falls back to running bare.
func TestWorkerCommand_SrtMissingIsAnError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := workerCommand(context.Background(), StartOptions{Sandbox: "srt", WorktreeDir: t.TempDir()}, cursorSrtPolicy, "/bin/cursor", nil)
	if err == nil {
		t.Fatal("want an error when srt is not on PATH")
	}
}

func TestCodexSrtPolicy_CodexHome(t *testing.T) {
	p := codexSrtPolicy([]string{"HOME=/h", "CODEX_HOME=/elsewhere"})
	if !slices.Equal(p.state, []string{"/elsewhere"}) {
		t.Errorf("state = %q", p.state)
	}
}
