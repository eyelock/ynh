package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
)

// srt is Anthropic's sandbox runtime (github.com/anthropic-experimental/
// sandbox-runtime, npm @anthropic-ai/sandbox-runtime). Its CLI reads its
// rules from a settings file named with --settings and from nowhere else:
// its only options are --debug, --settings, -c and --control-fd, and it
// accepts any other option without reading it. A --settings file that is
// missing, empty or invalid makes srt refuse to run, so a file ynh writes is
// either applied or the run stops.

// srtSettingsFile is the name of the settings file ynh writes for srt.
const srtSettingsFile = "srt-settings.json"

// srtSettings is the settings file, in the shape srt validates
// (SandboxRuntimeConfigSchema in sandbox-runtime's
// src/sandbox/sandbox-config.ts, v0.0.78). Every field here is one srt
// requires; the optional ones are left to srt's defaults.
type srtSettings struct {
	Network    srtNetwork    `json:"network"`
	Filesystem srtFilesystem `json:"filesystem"`
}

// srtNetwork is allow-only: srt's proxy refuses every host not listed.
type srtNetwork struct {
	AllowedDomains []string `json:"allowedDomains"`
	DeniedDomains  []string `json:"deniedDomains"`
}

// srtFilesystem leaves reads as srt allows them (everywhere, an empty
// denyRead) and writes allow-only: srt denies every write outside allowWrite
// and, inside it, the paths it always protects (shell rc files, .git/hooks,
// .git/config, .claude/commands, .claude/agents and others).
type srtFilesystem struct {
	DenyRead   []string `json:"denyRead"`
	AllowWrite []string `json:"allowWrite"`
	DenyWrite  []string `json:"denyWrite"`
}

// srtPolicy is what one vendor CLI needs from the sandbox to work at all.
type srtPolicy struct {
	// domains are the hosts the CLI must reach to authenticate and to talk
	// to its model.
	domains []string
	// state are the absolute paths the CLI writes its own state to.
	state []string
}

// claudeSrtPolicy is what Claude Code needs: its API, and the two hosts
// that sign in and refresh a claude.ai or Console login (Claude Code's
// "Network access requirements", code.claude.com/docs/en/network-config).
// The optional hosts in that list, operational telemetry, error reports,
// updates, plugin downloads and claude.ai connectors, stay blocked.
// State is its config directory and the config file it keeps beside it,
// both relocated by CLAUDE_CONFIG_DIR when the worker is given one.
func claudeSrtPolicy(env []string) srtPolicy {
	p := srtPolicy{domains: []string{"api.anthropic.com", "claude.ai", "platform.claude.com"}}
	if dir := lastEnv(env, "CLAUDE_CONFIG_DIR"); dir != "" {
		p.state = []string{dir}
		return p
	}
	home := lastEnv(env, "HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	p.state = []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".claude.json.backup"),
		filepath.Join(home, ".claude.json.lock"),
	}
	return p
}

// lastEnv returns the value name has in env, the last one when it is set
// more than once (a declared passthrough comes after the process minimum),
// or "".
func lastEnv(env []string, name string) string {
	v := ""
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && k == name {
			v = val
		}
	}
	return v
}

// buildSrtSettings returns the settings for one run: network only to the
// vendor's hosts, writes only to the worktree and the vendor's state, and
// the settings file itself never writable from inside, even when the
// session directory holding it lies in the worktree.
func buildSrtSettings(worktree, settingsPath string, p srtPolicy) srtSettings {
	allow := []string{realPath(worktree)}
	for _, path := range p.state {
		allow = append(allow, realPath(path))
	}
	return srtSettings{
		Network: srtNetwork{
			AllowedDomains: append([]string{}, p.domains...),
			DeniedDomains:  []string{},
		},
		Filesystem: srtFilesystem{
			DenyRead:   []string{},
			AllowWrite: allow,
			DenyWrite:  []string{realPath(settingsPath)},
		},
	}
}

// realPath returns path with the links in its deepest existing ancestor
// resolved. srt resolves the links in a rule only when the whole path
// exists, and the sandbox judges a write by where it lands, so a rule for a
// directory not created yet beneath a link (/tmp is one on macOS, and
// ~/.claude does not exist before Claude Code's first run) would match
// nothing.
func realPath(path string) string {
	rest := ""
	for dir := filepath.Clean(path); ; dir = filepath.Dir(dir) {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
		if filepath.Dir(dir) == dir {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// writeSrtSettings writes s to dir/srt-settings.json with mode 0600 and
// returns its path. The file is written beside its final name and renamed
// over it, so whatever is at that name already, a link included, is
// replaced rather than written through.
func writeSrtSettings(dir string, s srtSettings) (string, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding srt settings: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".srt-settings-*.json")
	if err != nil {
		return "", fmt.Errorf("writing srt settings: %w", err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		// CreateTemp makes the file 0600 already; the umask cannot widen it,
		// but say so rather than rely on it.
		werr = os.Chmod(tmp.Name(), 0o600)
	}
	path := filepath.Join(dir, srtSettingsFile)
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("writing srt settings: %w", werr)
	}
	return path, nil
}

// srtSettingsDir returns the directory the run's settings file goes in: the
// session directory, or when the run has none (no --emit-jsonl file), a
// fresh private directory under $YNH_HOME/run that cleanup removes once the
// worker has exited. cleanup is never nil.
func srtSettingsDir(sessionDir string) (dir string, cleanup func(), err error) {
	if sessionDir != "" {
		// Absolute: srt runs in the worktree, and resolves a relative
		// --settings path from there.
		dir, err = filepath.Abs(sessionDir)
		if err != nil {
			return "", nil, fmt.Errorf("resolving the session directory for srt settings: %w", err)
		}
		return dir, func() {}, nil
	}
	parent := filepath.Join(config.HomeDir(), "run")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", nil, fmt.Errorf("creating a directory for the srt settings: %w", err)
	}
	dir, err = os.MkdirTemp(parent, "srt-")
	if err != nil {
		return "", nil, fmt.Errorf("creating a directory for the srt settings: %w", err)
	}
	// Exactly the file this run writes and the directory MkdirTemp made for
	// it. os.Remove, not RemoveAll: a directory that holds anything else is
	// left as it is.
	return dir, func() {
		_ = os.Remove(filepath.Join(dir, srtSettingsFile))
		_ = os.Remove(dir)
	}, nil
}

// srtCommand writes the run's settings and returns srt wrapping bin. A
// settings file that cannot be written is an error, never a run under srt's
// defaults or outside srt. The "--" is load-bearing: srt reads its options
// anywhere on the line until one, and the vendor's own --settings would
// otherwise be taken for srt's. cleanup is never nil and runs once the
// command has exited.
func srtCommand(ctx context.Context, opts StartOptions, p srtPolicy, bin string, args []string) (cmd *exec.Cmd, cleanup func(), err error) {
	srtBin, err := exec.LookPath("srt")
	if err != nil {
		return nil, nil, fmt.Errorf("srt not found on PATH: %w", err)
	}
	worktree := opts.WorktreeDir
	if worktree == "" {
		if worktree, err = os.Getwd(); err != nil {
			return nil, nil, fmt.Errorf("resolving the worktree for srt settings: %w", err)
		}
	}
	if worktree, err = filepath.Abs(worktree); err != nil {
		return nil, nil, fmt.Errorf("resolving the worktree for srt settings: %w", err)
	}
	dir, cleanup, err := srtSettingsDir(opts.SessionDir)
	if err != nil {
		return nil, nil, err
	}
	settings := buildSrtSettings(worktree, filepath.Join(dir, srtSettingsFile), p)
	path, err := writeSrtSettings(dir, settings)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	srtArgs := append([]string{"--settings", path, "--", bin}, args...)
	return exec.CommandContext(ctx, srtBin, srtArgs...), cleanup, nil
}
