package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
)

// fakeProbe stubs both git and registry lookups for fillUpdates tests.
// gitResults maps "url|ref" → (sha, ok).
// registryResults maps "name|source|path" → (version, sha, ok).
type fakeProbe struct {
	gitResults map[string]struct {
		sha string
		ok  bool
	}
	registryResults map[string]struct {
		version, sha string
		ok           bool
	}
	gitCalls, regCalls atomic.Int64
}

func (f *fakeProbe) probe() updateProbe {
	return updateProbe{
		git: func(url, ref string) (string, bool) {
			f.gitCalls.Add(1)
			r, ok := f.gitResults[url+"|"+ref]
			if !ok {
				return "", false
			}
			return r.sha, r.ok
		},
		registry: func(name, source, path string) (string, string, bool) {
			f.regCalls.Add(1)
			r, ok := f.registryResults[name+"|"+source+"|"+path]
			if !ok {
				return "", "", false
			}
			return r.version, r.sha, r.ok
		},
	}
}

func TestFillUpdates_GitIncludeFloatingRef(t *testing.T) {
	// Floating-ref include: manifest declares Ref="main", install resolves
	// it to a SHA recorded as RefInstalled. Probe should target "main" upstream.
	entries := []listEntry{{
		Name: "demo",
		Includes: []listInclude{
			{Git: "github.com/example/repo", Ref: "main", RefInstalled: "oldsha", IsPinned: false},
		},
	}}

	fp := &fakeProbe{
		gitResults: map[string]struct {
			sha string
			ok  bool
		}{
			"github.com/example/repo|main": {sha: "newsha123", ok: true},
		},
	}
	fillUpdates(entries, fp.probe())

	if entries[0].Includes[0].RefAvailable != "newsha123" {
		t.Errorf("ref_available = %q, want newsha123", entries[0].Includes[0].RefAvailable)
	}
}

func TestFillUpdates_GitIncludePinnedProbesHEAD(t *testing.T) {
	// Pinned-to-SHA includes probe HEAD (empty ref) instead of re-resolving
	// the SHA. Otherwise a pinned include can never appear behind upstream.
	entries := []listEntry{{
		Name: "demo",
		Includes: []listInclude{
			{
				Git:          "github.com/example/repo",
				Ref:          "abc1234567890abcdef1234567890abcdef12345",
				RefInstalled: "abc1234567890abcdef1234567890abcdef12345",
				IsPinned:     true,
			},
		},
	}}

	fp := &fakeProbe{
		gitResults: map[string]struct {
			sha string
			ok  bool
		}{
			"github.com/example/repo|": {sha: "headsha", ok: true},
		},
	}
	fillUpdates(entries, fp.probe())

	if entries[0].Includes[0].RefAvailable != "headsha" {
		t.Errorf("ref_available = %q, want headsha (probed via empty ref)", entries[0].Includes[0].RefAvailable)
	}
}

func TestFillUpdates_ProbeFailureStaysOmitted(t *testing.T) {
	// A failed probe leaves _available unset — the contractually-correct
	// "unknown" three-state. The command must never error on probe failure.
	entries := []listEntry{{
		Name: "demo",
		Includes: []listInclude{
			{Git: "github.com/example/repo", Ref: "main", RefInstalled: "oldsha", IsPinned: false},
		},
	}}

	fp := &fakeProbe{
		gitResults: map[string]struct {
			sha string
			ok  bool
		}{
			"github.com/example/repo|main": {sha: "", ok: false},
		},
	}
	fillUpdates(entries, fp.probe())

	if entries[0].Includes[0].RefAvailable != "" {
		t.Errorf("expected ref_available omitted on probe failure, got %q", entries[0].Includes[0].RefAvailable)
	}
}

func TestFillUpdates_RegistryHarnessVersion(t *testing.T) {
	entries := []listEntry{{
		Name:         "demo",
		RefInstalled: "v0.1.0",
		InstalledFrom: &listInstalledFrom{
			SourceType: "registry",
			Source:     "github.com/example/registry-harness",
			Path:       "",
		},
	}}

	fp := &fakeProbe{
		registryResults: map[string]struct {
			version, sha string
			ok           bool
		}{
			"demo|github.com/example/registry-harness|": {version: "0.2.0", ok: true},
		},
	}
	fillUpdates(entries, fp.probe())

	if entries[0].VersionAvailable != "0.2.0" {
		t.Errorf("version_available = %q, want 0.2.0", entries[0].VersionAvailable)
	}
}

func TestFillUpdates_LocalHarnessNotProbed(t *testing.T) {
	// Pure local installs (no installed_from) yield no probes — there is no
	// upstream to query.
	entries := []listEntry{{Name: "local"}}

	fp := &fakeProbe{}
	fillUpdates(entries, fp.probe())

	if fp.gitCalls.Load() != 0 || fp.regCalls.Load() != 0 {
		t.Errorf("expected zero probes for local-only harness, got git=%d reg=%d",
			fp.gitCalls.Load(), fp.regCalls.Load())
	}
}

func TestFillUpdates_GitHarnessProbesSource(t *testing.T) {
	entries := []listEntry{{
		Name:         "demo",
		RefInstalled: "v1.0",
		InstalledFrom: &listInstalledFrom{
			SourceType: "git",
			Source:     "github.com/example/harness-repo",
		},
	}}

	fp := &fakeProbe{
		gitResults: map[string]struct {
			sha string
			ok  bool
		}{
			"github.com/example/harness-repo|v1.0": {sha: "headsha", ok: true},
		},
	}
	fillUpdates(entries, fp.probe())

	if entries[0].RefAvailable != "headsha" {
		t.Errorf("harness ref_available = %q, want headsha", entries[0].RefAvailable)
	}
}

func TestCmdListCheckUpdatesEndToEnd(t *testing.T) {
	// Full path through cmdListTo with --check-updates: stub the git ls-remote
	// shellout so the test is hermetic, then confirm version_available /
	// ref_available appear in the JSON.
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	hj := `{
		"name": "demo",
		"version": "0.1.0",
		"default_vendor": "claude",
		"includes": [
			{"git": "github.com/example/repo", "ref": "main"}
		]
	}`
	dir := filepath.Join(home, "harnesses", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stub the network primitive — cmdListTo → defaultProbe() routes
	// through resolver.LsRemoteFunc. Force failure so the test is hermetic
	// (no real network call to github.com/example/repo).
	originalLsRemote := resolver.LsRemoteFunc
	resolver.LsRemoteFunc = func(string, string) (string, error) {
		return "", fmt.Errorf("stubbed: probe failure")
	}
	t.Cleanup(func() { resolver.LsRemoteFunc = originalLsRemote })

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json", "--check-updates"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	var env listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(env.Harnesses) != 1 {
		t.Fatalf("expected 1 harness, got %d", len(env.Harnesses))
	}
	// Stubbed probe fails ⇒ _available fields stay omitted. The assertion is
	// that the command emits the envelope cleanly and does not error on
	// probe failure — the contractually-correct "unknown" three-state.
	if strings.Contains(stdout.String(), `"version_available"`) {
		t.Errorf("version_available should be omitted when probe fails; got: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), `"ref_available"`) {
		t.Errorf("ref_available should be omitted when probe fails; got: %s", stdout.String())
	}
}

func TestCmdListCheckUpdatesEndToEndSuccess(t *testing.T) {
	// Same harness, stubbed probe succeeds — confirms the success path
	// surfaces ref_available on the include.
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	hj := `{
		"name": "demo",
		"version": "0.1.0",
		"default_vendor": "claude",
		"includes": [
			{"git": "github.com/example/repo", "ref": "main"}
		]
	}`
	dir := filepath.Join(home, "harnesses", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}

	originalLsRemote := resolver.LsRemoteFunc
	resolver.LsRemoteFunc = func(string, string) (string, error) {
		return "deadbeef0123456789abcdef0123456789abcdef", nil
	}
	t.Cleanup(func() { resolver.LsRemoteFunc = originalLsRemote })

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json", "--check-updates"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	var env listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Harnesses) != 1 || len(env.Harnesses[0].Includes) != 1 {
		t.Fatalf("unexpected envelope shape: %+v", env)
	}
	if got := env.Harnesses[0].Includes[0].RefAvailable; got != "deadbeef0123456789abcdef0123456789abcdef" {
		t.Errorf("ref_available = %q, want deadbeef…", got)
	}
}
