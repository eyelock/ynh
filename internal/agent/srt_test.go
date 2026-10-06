package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// startSandboxedClaude starts the claude backend under --sandbox srt with
// stub claude and srt binaries on PATH, closes it, and returns the arguments
// srt was given.
func startSandboxedClaude(t *testing.T, opts StartOptions) []string {
	t.Helper()
	fakeVendor(t, "claude", "", "", 0)
	srtArgs := fakeVendor(t, "srt", "", "", 0)
	opts.Sandbox = "srt"
	sess, err := (&ClaudeBackend{}).Start(context.Background(), opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = sess.Close()
	data, err := os.ReadFile(srtArgs)
	if err != nil {
		t.Fatalf("srt was not run: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// #528: ynh ran `srt --profile workspace --network-allow ... -- claude`.
// srt's CLI has no such options and ignores unknown ones, so the worker ran
// under srt's defaults and ynh's allowlist never applied. srt reads its
// rules from a settings file named with --settings, and only from there.
func TestClaudeBackend_SrtGetsSettingsFile(t *testing.T) {
	session := t.TempDir()
	got := startSandboxedClaude(t, StartOptions{WorktreeDir: t.TempDir(), SessionDir: session})

	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--settings", filepath.Join(session, srtSettingsFile), "--", claudeBin}
	if len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
		t.Fatalf("srt args = %q, want them to start %q", got, want)
	}
	for _, flag := range []string{"--profile", "--network-allow"} {
		if slices.Contains(got[:slices.Index(got, "--")], flag) {
			t.Errorf("srt was given %s, an option it does not have: %q", flag, got)
		}
	}
	// claude's own --settings must reach claude, not srt: srt's option
	// parser reads its options anywhere on the line until a "--".
	if !slices.Contains(got[len(want):], "--input-format") {
		t.Errorf("claude's arguments did not follow the --: %q", got)
	}
}

// The file is the run's own: written in the session directory, readable by
// the operator alone.
func TestClaudeBackend_SrtSettingsFileMode(t *testing.T) {
	session := t.TempDir()
	startSandboxedClaude(t, StartOptions{WorktreeDir: t.TempDir(), SessionDir: session})
	info, err := os.Stat(filepath.Join(session, srtSettingsFile))
	if err != nil {
		t.Fatalf("settings file not written: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("settings file mode = %o, want 600", mode)
	}
}

// A run with no session directory (no --emit-jsonl file) still gets a
// settings file, in a private directory under YNH_HOME, removed with the
// session.
func TestClaudeBackend_SrtSettingsWithoutSessionDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	got := startSandboxedClaude(t, StartOptions{WorktreeDir: t.TempDir()})
	if got[0] != "--settings" {
		t.Fatalf("srt args = %q, want --settings first", got)
	}
	path := got[1]
	if !strings.HasPrefix(path, filepath.Join(home, "run")+string(filepath.Separator)) {
		t.Errorf("settings file %s is not under YNH_HOME/run", path)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("settings file %s outlived the session (stat err %v)", path, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("settings directory %s outlived the session (stat err %v)", filepath.Dir(path), err)
	}
}

// Fail closed: a settings file that cannot be written means no run, never a
// run under srt's defaults or no sandbox at all.
func TestClaudeBackend_SrtRefusesWhenSettingsUnwritable(t *testing.T) {
	fakeVendor(t, "claude", "", "", 0)
	srtArgs := fakeVendor(t, "srt", "", "", 0)
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (&ClaudeBackend{}).Start(context.Background(), StartOptions{
		WorktreeDir: t.TempDir(),
		SessionDir:  notADir,
		Sandbox:     "srt",
	})
	if err == nil || !strings.Contains(err.Error(), "srt settings") {
		t.Fatalf("Start err = %v, want a refusal naming the srt settings", err)
	}
	if _, statErr := os.Stat(srtArgs); !os.IsNotExist(statErr) {
		t.Errorf("srt ran although its settings could not be written")
	}
}

// A link planted at the settings file's name is replaced, not written
// through: the session directory may sit inside the worktree, which the
// worker can write.
func TestWriteSrtSettings_ReplacesLink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, srtSettingsFile)); err != nil {
		t.Fatal(err)
	}
	path, err := writeSrtSettings(dir, srtSettings{})
	if err != nil {
		t.Fatalf("writeSrtSettings: %v", err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "keep" {
		t.Errorf("the link's target was written: %q", data)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("settings path is still a link (err %v)", err)
	}
}

// srtSchemaCheck validates a settings file against the shape srt 0.0.78
// accepts (SandboxRuntimeConfigSchema in sandbox-runtime's
// src/sandbox/sandbox-config.ts): network.allowedDomains,
// network.deniedDomains, filesystem.denyRead, filesystem.allowWrite and
// filesystem.denyWrite are required arrays; a domain entry is a host or
// "*.domain" pattern with an optional ":port", no scheme or path.
func srtSchemaCheck(t *testing.T, data []byte) map[string]map[string][]string {
	t.Helper()
	var doc map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("settings are not a JSON object of objects: %v\n%s", err, data)
	}
	required := map[string][]string{
		"network":    {"allowedDomains", "deniedDomains"},
		"filesystem": {"denyRead", "allowWrite", "denyWrite"},
	}
	out := map[string]map[string][]string{}
	for section, keys := range required {
		fields, ok := doc[section]
		if !ok {
			t.Fatalf("settings have no %q section:\n%s", section, data)
		}
		out[section] = map[string][]string{}
		for _, key := range keys {
			raw, ok := fields[key]
			if !ok {
				t.Fatalf("%s.%s is required by srt and missing:\n%s", section, key, data)
			}
			var list []string
			if err := json.Unmarshal(raw, &list); err != nil || list == nil {
				t.Fatalf("%s.%s = %s, want an array of strings", section, key, raw)
			}
			out[section][key] = list
		}
		for key := range fields {
			if !slices.Contains(keys, key) {
				t.Errorf("%s.%s is written but not checked here", section, key)
			}
		}
	}
	for section := range doc {
		if _, ok := required[section]; !ok {
			t.Errorf("unexpected top-level key %q", section)
		}
	}
	for _, d := range out["network"]["allowedDomains"] {
		host := d
		if h, port, ok := strings.Cut(d, ":"); ok {
			host = h
			if port == "" || strings.Trim(port, "0123456789") != "" {
				t.Errorf("allowedDomains entry %q has a bad port", d)
			}
		}
		if strings.Contains(host, "/") || !strings.Contains(host, ".") ||
			strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") ||
			(strings.Contains(host, "*") && !strings.HasPrefix(host, "*.")) {
			t.Errorf("allowedDomains entry %q is not a pattern srt accepts", d)
		}
	}
	for _, list := range out["filesystem"] {
		for _, p := range list {
			if !filepath.IsAbs(p) {
				t.Errorf("filesystem path %q is not absolute", p)
			}
		}
	}
	return out
}

// The file's content, per backend that implements srt: the vendor's own
// hosts, the worktree, the vendor's state, and nothing else.
func TestSrtSettings_PerVendor(t *testing.T) {
	home := resolvedTempDir(t)
	tests := []struct {
		name       string
		policy     func(env []string) srtPolicy
		env        []string
		wantDomain []string
		wantWrite  []string
	}{
		{
			name:       "claude",
			policy:     claudeSrtPolicy,
			env:        []string{"HOME=" + home},
			wantDomain: []string{"api.anthropic.com", "claude.ai", "platform.claude.com"},
			wantWrite: []string{
				filepath.Join(home, ".claude"),
				filepath.Join(home, ".claude.json"),
				filepath.Join(home, ".claude.json.backup"),
				filepath.Join(home, ".claude.json.lock"),
			},
		},
		{
			name:       "claude with CLAUDE_CONFIG_DIR passed through",
			policy:     claudeSrtPolicy,
			env:        []string{"HOME=" + home, "CLAUDE_CONFIG_DIR=/srv/claude-state"},
			wantDomain: []string{"api.anthropic.com", "claude.ai", "platform.claude.com"},
			wantWrite:  []string{"/srv/claude-state"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worktree := resolvedTempDir(t)
			settingsPath := filepath.Join(resolvedTempDir(t), srtSettingsFile)
			s := buildSrtSettings(worktree, settingsPath, tt.policy(tt.env))
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			got := srtSchemaCheck(t, data)
			if d := got["network"]["allowedDomains"]; !slices.Equal(d, tt.wantDomain) {
				t.Errorf("allowedDomains = %q, want %q", d, tt.wantDomain)
			}
			if d := got["network"]["deniedDomains"]; len(d) != 0 {
				t.Errorf("deniedDomains = %q, want none", d)
			}
			wantWrite := append([]string{worktree}, tt.wantWrite...)
			if w := got["filesystem"]["allowWrite"]; !slices.Equal(w, wantWrite) {
				t.Errorf("allowWrite = %q, want %q", w, wantWrite)
			}
			if r := got["filesystem"]["denyRead"]; len(r) != 0 {
				t.Errorf("denyRead = %q, want srt's default reads", r)
			}
			if w := got["filesystem"]["denyWrite"]; !slices.Equal(w, []string{settingsPath}) {
				t.Errorf("denyWrite = %q, want the settings file itself", w)
			}
		})
	}
}

// resolvedTempDir is t.TempDir() with its links resolved (on macOS it lies
// under /var, a link to /private/var), the spelling srt's rules need.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// srt resolves the links in a rule only when the whole path exists, and
// judges a write by where it lands. A rule for ~/.claude before Claude
// Code's first run, beneath a linked directory, must be written with the
// link resolved or it matches nothing (found running srt 0.0.78 on macOS,
// where /tmp is a link to /private/tmp).
func TestRealPath(t *testing.T) {
	real := resolvedTempDir(t)
	link := filepath.Join(resolvedTempDir(t), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, in, want string }{
		{"existing path through a link", link, real},
		{"absent path beneath a link", filepath.Join(link, ".claude", "x"), filepath.Join(real, ".claude", "x")},
		{"no links", filepath.Join(real, "absent"), filepath.Join(real, "absent")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := realPath(tt.in); got != tt.want {
				t.Errorf("realPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
