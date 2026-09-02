package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// The adapter interface is implemented four times, and the defects it has
// actually shipped were all the same shape: one vendor emitting another
// vendor's layout. `ynd marketplace build` wrote Claude's index for Cursor
// (#301) and Cursor's generator produced the wrong shape for a year (#309).
// Both were found by hand, after release, in the least-covered package in the
// repository.
//
// So these tests are cross-vendor by construction. Asserting each adapter in
// isolation would not have caught either bug: each was internally consistent
// and wrong only by comparison.

func allAdapters(t *testing.T) map[string]Adapter {
	t.Helper()
	out := map[string]Adapter{}
	for _, name := range Available() {
		a, err := Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		out[name] = a
	}
	if len(out) < 4 {
		t.Fatalf("expected at least 4 registered vendors, got %d: %v", len(out), Available())
	}
	return out
}

// Each vendor's plugin manifest path is asserted explicitly, because the
// interesting failure is a path *changing*, not a path colliding.
//
// Copilot deliberately shares Claude's `.claude-plugin/plugin.json`: it
// silently loads no plugin content at all via --plugin-dir unless that exact
// file is at the directory root. That is a documented vendor quirk, not a
// vendor emitting someone else's layout, and the distinction is the whole
// reason this is a table rather than a uniqueness check.
func TestPluginManifestPaths(t *testing.T) {
	want := map[string]string{
		"claude":  ".claude-plugin/plugin.json",
		"codex":   ".codex-plugin/plugin.json",
		"cursor":  ".cursor-plugin/plugin.json",
		"copilot": ".claude-plugin/plugin.json",
	}
	hj := &plugin.HarnessJSON{Name: "demo", Version: "1.0.0", Description: "d"}

	for name, a := range allAdapters(t) {
		files, err := a.GeneratePluginManifest(hj, t.TempDir())
		if err != nil {
			t.Errorf("%s: GeneratePluginManifest: %v", name, err)
			continue
		}
		got := keysOf(files)
		if len(got) != 1 {
			t.Errorf("%s: expected exactly one manifest, got %v", name, got)
			continue
		}
		if filepath.ToSlash(got[0]) != want[name] {
			t.Errorf("%s: manifest path is %q, want %q", name, got[0], want[name])
		}
	}
}

// Each vendor's manifest must carry the harness's own identity. A generator
// that ignores its input produces a plausible file describing nothing.
func TestPluginManifestCarriesHarnessIdentity(t *testing.T) {
	hj := &plugin.HarnessJSON{
		Name:        "distinctive-harness-name",
		Version:     "9.9.9",
		Description: "a description that must survive",
	}
	for name, a := range allAdapters(t) {
		files, err := a.GeneratePluginManifest(hj, t.TempDir())
		if err != nil || len(files) == 0 {
			continue // vendors without a manifest are covered elsewhere
		}
		for path, data := range files {
			if !strings.Contains(string(data), hj.Name) {
				t.Errorf("%s: %s does not carry the harness name %q:\n%s",
					name, path, hj.Name, data)
			}
			// It must also be valid JSON, since a vendor parses it.
			var v any
			if err := json.Unmarshal(data, &v); err != nil {
				t.Errorf("%s: %s is not valid JSON: %v", name, path, err)
			}
		}
	}
}

// The marketplace index is the other half of #309: the generator ran, produced
// output, and produced the wrong vendor's shape.
func TestMarketplaceIndexIsValidAndDistinct(t *testing.T) {
	cfg := MarketplaceIndexConfig{
		Name: "demo-market", Description: "d", OwnerName: "o", OwnerEmail: "o@example.com",
	}
	plugins := []MarketplacePluginInfo{{Name: "p1", Description: "one", Version: "1.0.0"}}

	shapes := map[string]string{} // vendor -> sorted top-level keys
	for name, a := range allAdapters(t) {
		data, err := a.GenerateMarketplaceIndex(cfg, plugins)
		if err != nil {
			t.Errorf("%s: GenerateMarketplaceIndex: %v", name, err)
			continue
		}
		if len(data) == 0 {
			continue // not every vendor publishes a marketplace
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s: marketplace index is not valid JSON: %v\n%s", name, err, data)
			continue
		}
		keys := make([]string, 0, len(doc))
		for k := range doc {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		shapes[name] = strings.Join(keys, ",")

		if !strings.Contains(string(data), "p1") {
			t.Errorf("%s: index does not mention the plugin it was given:\n%s", name, data)
		}
	}
	if len(shapes) < 2 {
		t.Fatalf("fewer than two vendors produced an index; this cannot detect a shape clash: %v", shapes)
	}
}

// Where a vendor declares a marketplace manifest directory, it must be its own.
func TestMarketplaceManifestDirsAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for name, a := range allAdapters(t) {
		dir := a.MarketplaceManifestDir()
		if dir == "" {
			continue
		}
		if prev, clash := seen[dir]; clash {
			t.Errorf("%s and %s both claim marketplace dir %q", prev, name, dir)
			continue
		}
		seen[dir] = name
	}
	if len(seen) < 3 {
		t.Errorf("expected most vendors to declare a marketplace dir, got %d: %v", len(seen), seen)
	}
}

// Every vendor must emit AGENTS.md, the cross-vendor instruction file, with
// the content it was handed. A vendor that drops it ships a harness whose
// instructions are silently absent.
func TestSystemPromptAlwaysCarriesAgentsMD(t *testing.T) {
	content := []byte("# Instructions\n\nDistinctive body text.\n")
	for name, a := range allAdapters(t) {
		files := a.GenerateSystemPrompt(content)
		body, ok := files["AGENTS.md"]
		if !ok {
			t.Errorf("%s: GenerateSystemPrompt emitted no AGENTS.md, got %v", name, keysOf(files))
			continue
		}
		if string(body) != string(content) {
			t.Errorf("%s: AGENTS.md content was altered\nwant: %q\ngot:  %q", name, content, body)
		}
	}
}

// Claude does not read AGENTS.md natively, so it must also write a CLAUDE.md
// that imports it. This is the pairing that makes the cross-vendor file work
// on Claude at all.
func TestClaudeImportsAgentsMD(t *testing.T) {
	a, err := Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	files := a.GenerateSystemPrompt([]byte("body"))
	claudeMD, ok := files["CLAUDE.md"]
	if !ok {
		t.Fatalf("claude emitted no CLAUDE.md, got %v", keysOf(files))
	}
	if !strings.Contains(string(claudeMD), "@AGENTS.md") {
		t.Errorf("CLAUDE.md must @-import AGENTS.md, got: %q", claudeMD)
	}
}

// NeedsSymlinks and Install must agree. A vendor claiming it needs symlinks
// and then installing none leaves a project silently unconfigured.
func TestInstallAgreesWithNeedsSymlinks(t *testing.T) {
	for name, a := range allAdapters(t) {
		staging := t.TempDir()
		project := t.TempDir()
		// installSymlinks reads <staging>/<configDir>/<artifactDir>, so the
		// fixture has to mirror that. Writing to <staging>/<artifactDir>
		// produces zero entries and a test that passes for the wrong reason.
		for _, d := range a.ArtifactDirs() {
			dir := filepath.Join(staging, a.ConfigDir(), d)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "x.md"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		entries, err := a.Install(staging, project)
		if err != nil {
			t.Errorf("%s: Install: %v", name, err)
			continue
		}
		if a.NeedsSymlinks() && len(entries) == 0 {
			t.Errorf("%s: NeedsSymlinks() is true but Install created nothing", name)
		}
		if !a.NeedsSymlinks() && len(entries) != 0 {
			t.Errorf("%s: NeedsSymlinks() is false but Install created %d entries", name, len(entries))
		}

		// Whatever it created, Clean must remove.
		if err := a.Clean(entries); err != nil {
			t.Errorf("%s: Clean: %v", name, err)
			continue
		}
		for _, e := range entries {
			if _, err := os.Lstat(e.Link); err == nil {
				t.Errorf("%s: Clean left %s behind", name, e.Link)
			}
		}
	}
}

// Copilot's hooks are confirmed to silently never fire, so ynh emits no hook
// config for it. A config that looks wired and does nothing is worse than an
// honest gap, and this asserts the gap stays deliberate.
func TestCopilotEmitsNoHookConfig(t *testing.T) {
	a, err := Get("copilot")
	if err != nil {
		t.Fatal(err)
	}
	files, err := a.GenerateHookConfig(map[string][]plugin.HookEntry{
		"before_tool": {{Command: "guard.sh"}},
	})
	if err != nil {
		t.Fatalf("GenerateHookConfig: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("copilot must emit no hook config while its hooks do not fire, got %v", keysOf(files))
	}
}

// ClaudeHookEvent maps ynh's canonical names to Claude's, and reports whether
// the name was canonical at all. `ynh doctor` relies on the second return to
// spot a canonical name leaking into a vendor-native settings file.
func TestClaudeHookEvent(t *testing.T) {
	cases := []struct {
		in          string
		wantNative  string
		wantIsCanon bool
	}{
		{"before_tool", "PreToolUse", true},
		{"after_tool", "PostToolUse", true},
		{"on_session_start", "SessionStart", true},
		{"on_stop", "Stop", true},
		{"PreToolUse", "", false},
		{"not_an_event", "", false},
	}
	for _, tc := range cases {
		native, isCanon := ClaudeHookEvent(tc.in)
		if isCanon != tc.wantIsCanon {
			t.Errorf("ClaudeHookEvent(%q) canonical = %v, want %v", tc.in, isCanon, tc.wantIsCanon)
		}
		if isCanon && native != tc.wantNative {
			t.Errorf("ClaudeHookEvent(%q) = %q, want %q", tc.in, native, tc.wantNative)
		}
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The declarative surface: values the assembler and exporter branch on. These
// are one-line returns, but they are the switches that decide what a harness
// ships, and a silent flip is not visible anywhere else. Asserted as a table
// so a change has to be deliberate.
func TestVendorDeclarativeContract(t *testing.T) {
	type contract struct {
		displayName     string
		configDir       string
		needsSymlinks   bool
		exportDelegates bool
		initialPrompt   bool
		supportsResume  bool
		marketplaceDir  string
		exportArtifacts map[string]string
	}
	want := map[string]contract{
		"claude": {
			displayName: "Claude Code", configDir: ".claude",
			needsSymlinks: false, exportDelegates: true, initialPrompt: true,
			supportsResume: true, marketplaceDir: ".claude-plugin",
		},
		"codex": {
			displayName: "OpenAI Codex", configDir: ".codex",
			needsSymlinks: true, exportDelegates: false, initialPrompt: true,
			supportsResume: true, marketplaceDir: filepath.Join(".agents", "plugins"),
			exportArtifacts: map[string]string{"skills": "skills"},
		},
		"cursor": {
			displayName: "Cursor", configDir: ".cursor",
			needsSymlinks: true, exportDelegates: true, initialPrompt: true,
			supportsResume: true, marketplaceDir: ".cursor-plugin",
		},
		"copilot": {
			displayName: "GitHub Copilot CLI", configDir: ".copilot",
			needsSymlinks: false, exportDelegates: true, initialPrompt: true,
			supportsResume: true, marketplaceDir: filepath.Join(".github", "plugin"),
			exportArtifacts: map[string]string{"agents": "agents", "skills": "skills"},
		},
	}

	for name, a := range allAdapters(t) {
		w, ok := want[name]
		if !ok {
			t.Errorf("vendor %q is registered but not described here; add it", name)
			continue
		}
		if got := a.DisplayName(); got != w.displayName {
			t.Errorf("%s DisplayName = %q, want %q", name, got, w.displayName)
		}
		if got := a.ConfigDir(); got != w.configDir {
			t.Errorf("%s ConfigDir = %q, want %q", name, got, w.configDir)
		}
		if got := a.NeedsSymlinks(); got != w.needsSymlinks {
			t.Errorf("%s NeedsSymlinks = %v, want %v", name, got, w.needsSymlinks)
		}
		if got := a.SupportsExportDelegates(); got != w.exportDelegates {
			t.Errorf("%s SupportsExportDelegates = %v, want %v", name, got, w.exportDelegates)
		}
		// SupportsInitialPrompt is not on Adapter. It is an optional interface
		// declared where it is consumed (cmd/ynh/run.go), feeding
		// `ynh vendors --format json`. Asserted through the same shape, so a
		// vendor dropping the method shows up here rather than silently
		// reporting false to that command.
		ip, ok := a.(interface{ SupportsInitialPrompt() bool })
		if !ok {
			t.Errorf("%s no longer implements SupportsInitialPrompt; "+
				"`ynh vendors --format json` will report false for it", name)
		} else if got := ip.SupportsInitialPrompt(); got != w.initialPrompt {
			t.Errorf("%s SupportsInitialPrompt = %v, want %v", name, got, w.initialPrompt)
		}
		if got := a.SupportsResume(); got != w.supportsResume {
			t.Errorf("%s SupportsResume = %v, want %v", name, got, w.supportsResume)
		}
		if got := a.MarketplaceManifestDir(); got != w.marketplaceDir {
			t.Errorf("%s MarketplaceManifestDir = %q, want %q", name, got, w.marketplaceDir)
		}
		// ExportArtifactDirs remaps artifact directories on export. nil means
		// "no remapping", which is a real answer and not an absent one.
		if got := a.ExportArtifactDirs(); len(got) != len(w.exportArtifacts) {
			t.Errorf("%s ExportArtifactDirs = %v, want %v", name, got, w.exportArtifacts)
		}
	}
}
