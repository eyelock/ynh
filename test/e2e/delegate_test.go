//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pluginManifest mirrors the on-disk .ynh-plugin/plugin.json shape we
// need to read after editor mutations. Subset of internal/plugin.PluginJSON.
type pluginManifest struct {
	Name        string               `json:"name"`
	Version     string               `json:"version"`
	Description string               `json:"description,omitempty"`
	Includes    []manifestSourceJSON `json:"includes,omitempty"`
	DelegatesTo []manifestSourceJSON `json:"delegates_to,omitempty"`
}

type manifestSourceJSON struct {
	Git   string   `json:"git,omitempty"`
	Local string   `json:"local,omitempty"`
	Ref   string   `json:"ref,omitempty"`
	Path  string   `json:"path,omitempty"`
	Pick  []string `json:"pick,omitempty"`
}

// TestDelegate_AddRemove walks a delegate through add → resolved → remove
// and asserts the manifest and installed.json both reflect each step.
func TestDelegate_AddRemove(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")
	s.mustRunYnh(t, "install", sourceDir)

	delegateURL := "https://github.com/eyelock/assistants"
	s.mustRunYnh(t, "delegate", "add", "local/minimal", delegateURL,
		"--path", "e2e-fixtures/fork-source",
		"--ref", AssistantsFixturesSHA,
	)

	mf := readManifest(t, sourceDir)
	if len(mf.DelegatesTo) != 1 {
		t.Fatalf("expected 1 delegate after add, got %d: %+v", len(mf.DelegatesTo), mf.DelegatesTo)
	}
	d := mf.DelegatesTo[0]
	assertEqual(t, "delegates_to[0].git", d.Git, delegateURL)
	assertEqual(t, "delegates_to[0].path", d.Path, "e2e-fixtures/fork-source")
	assertEqual(t, "delegates_to[0].ref", d.Ref, AssistantsFixturesSHA)

	s.mustRunYnh(t, "delegate", "remove", "local/minimal", delegateURL, "--path", "e2e-fixtures/fork-source")

	mf = readManifest(t, sourceDir)
	if len(mf.DelegatesTo) != 0 {
		t.Errorf("expected 0 delegates after remove, got %d", len(mf.DelegatesTo))
	}
}

// TestInclude_AddRemove walks an include through add → resolved → remove.
func TestInclude_AddRemove(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")
	s.mustRunYnh(t, "install", sourceDir)

	includeURL := "https://github.com/eyelock/assistants"
	s.mustRunYnh(t, "include", "add", "local/minimal", includeURL,
		"--path", "e2e-fixtures/included-skill",
		"--ref", AssistantsFixturesSHA,
	)

	mf := readManifest(t, sourceDir)
	if len(mf.Includes) != 1 {
		t.Fatalf("expected 1 include after add, got %d", len(mf.Includes))
	}
	i := mf.Includes[0]
	assertEqual(t, "includes[0].git", i.Git, includeURL)
	assertEqual(t, "includes[0].path", i.Path, "e2e-fixtures/included-skill")
	assertEqual(t, "includes[0].ref", i.Ref, AssistantsFixturesSHA)

	s.mustRunYnh(t, "include", "remove", "local/minimal", includeURL, "--path", "e2e-fixtures/included-skill")

	mf = readManifest(t, sourceDir)
	if len(mf.Includes) != 0 {
		t.Errorf("expected 0 includes after remove, got %d", len(mf.Includes))
	}
}

// TestInclude_Update mutates an existing include's --path and asserts the
// manifest reflects the change. Covers cmdIncludeUpdate (0% E2E previously).
//
// We mutate --path rather than --ref to keep the test SHA-stable: the
// AssistantsFixturesSHA contains both `e2e-fixtures/minimal` and
// `e2e-fixtures/included-skill` so the path can flip between them without
// needing to advance the pinned ref.
func TestInclude_Update(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")
	s.mustRunYnh(t, "install", sourceDir)

	includeURL := "https://github.com/eyelock/assistants"
	s.mustRunYnh(t, "include", "add", "local/minimal", includeURL,
		"--path", "e2e-fixtures/minimal",
		"--ref", AssistantsFixturesSHA,
	)

	s.mustRunYnh(t, "include", "update", "local/minimal", includeURL,
		"--from-path", "e2e-fixtures/minimal",
		"--path", "e2e-fixtures/included-skill",
	)

	mf := readManifest(t, sourceDir)
	if len(mf.Includes) != 1 {
		t.Fatalf("expected 1 include after update, got %d", len(mf.Includes))
	}
	assertEqual(t, "includes[0].path", mf.Includes[0].Path, "e2e-fixtures/included-skill")
	assertEqual(t, "includes[0].ref unchanged", mf.Includes[0].Ref, AssistantsFixturesSHA)
}

// TestDelegate_Update mutates an existing delegate's --ref and asserts the
// manifest reflects the new ref. Covers cmdDelegateUpdate (0% E2E previously).
func TestDelegate_Update(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")
	s.mustRunYnh(t, "install", sourceDir)

	delegateURL := "https://github.com/eyelock/assistants"
	s.mustRunYnh(t, "delegate", "add", "local/minimal", delegateURL,
		"--path", "e2e-fixtures/fork-source",
		"--ref", AssistantsFixturesSHA,
	)

	s.mustRunYnh(t, "delegate", "update", "local/minimal", delegateURL,
		"--from-path", "e2e-fixtures/fork-source",
		"--ref", AssistantsFixturesV1Tag,
	)

	mf := readManifest(t, sourceDir)
	if len(mf.DelegatesTo) != 1 {
		t.Fatalf("expected 1 delegate after update, got %d", len(mf.DelegatesTo))
	}
	assertEqual(t, "delegates_to[0].ref", mf.DelegatesTo[0].Ref, AssistantsFixturesV1Tag)
	assertEqual(t, "delegates_to[0].path", mf.DelegatesTo[0].Path, "e2e-fixtures/fork-source")
}

// TestInclude_LocalInstall_WritesToSource locks in the schema-3 read/write
// symmetry for local installs. The original bug (v0.3.1) was that include
// add wrote to one place and ynh run / info read from another; this test
// pins both halves at the CLI boundary so the regression cannot recur
// without the e2e suite going red:
//
//   - The edit lands in the user's source tree (the canonical authored
//     location — `ynh include add` must not touch any copy).
//   - The same install id, loaded back via `ynh info`, sees the new
//     include — i.e. the read path agrees with the write path.
//   - No copy dir exists at HarnessesDir/local--minimal at all — schema 3
//     does not duplicate content for pointer-form installs.
//   - Remove is symmetric on both halves.
func TestInclude_LocalInstall_WritesToSource(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")
	copyDir := filepath.Join(s.home, "harnesses", "local--minimal")

	s.mustRunYnh(t, "install", sourceDir)

	includeURL := "https://github.com/eyelock/assistants"
	s.mustRunYnh(t, "include", "add", "local/minimal", includeURL,
		"--path", "e2e-fixtures/included-skill",
		"--ref", AssistantsFixturesSHA,
	)

	// Write path: the edit is in the user's source tree.
	srcMf := readManifest(t, sourceDir)
	if len(srcMf.Includes) != 1 {
		t.Fatalf("source dir: expected 1 include after add, got %d", len(srcMf.Includes))
	}
	assertEqual(t, "source includes[0].git", srcMf.Includes[0].Git, includeURL)

	// Read path: ynh info on the same id sees the new include. This is
	// the assertion that would fail under the v0.3.1 read/write split.
	infoOut, _ := s.mustRunYnh(t, "info", "local/minimal", "--format", "json")
	if !strings.Contains(infoOut, includeURL) {
		t.Errorf("ynh info did not surface the new include — write/read split has regressed.\nincludeURL=%q\noutput:\n%s", includeURL, infoOut)
	}

	// Topology: no copy dir for pointer-form installs.
	if _, err := os.Stat(copyDir); !os.IsNotExist(err) {
		t.Errorf("expected no copy dir for local install, got err=%v", err)
	}

	// Remove is symmetric on both halves.
	s.mustRunYnh(t, "include", "remove", "local/minimal", includeURL, "--path", "e2e-fixtures/included-skill")
	srcMf = readManifest(t, sourceDir)
	if len(srcMf.Includes) != 0 {
		t.Errorf("source dir: expected 0 includes after remove, got %d", len(srcMf.Includes))
	}
	infoOut, _ = s.mustRunYnh(t, "info", "local/minimal", "--format", "json")
	if strings.Contains(infoOut, includeURL) {
		t.Errorf("ynh info still surfaces removed include — read path went stale.\noutput:\n%s", infoOut)
	}
}

// TestDelegate_LocalInstall_WritesToSource is the delegate counterpart to
// TestInclude_LocalInstall_WritesToSource. Pins the same read/write
// symmetry — the original bug surfaced through delegates too.
func TestDelegate_LocalInstall_WritesToSource(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")
	copyDir := filepath.Join(s.home, "harnesses", "local--minimal")

	s.mustRunYnh(t, "install", sourceDir)

	delegateURL := "https://github.com/eyelock/assistants"
	s.mustRunYnh(t, "delegate", "add", "local/minimal", delegateURL,
		"--path", "e2e-fixtures/fork-source",
		"--ref", AssistantsFixturesSHA,
	)

	srcMf := readManifest(t, sourceDir)
	if len(srcMf.DelegatesTo) != 1 {
		t.Fatalf("source dir: expected 1 delegate after add, got %d", len(srcMf.DelegatesTo))
	}
	assertEqual(t, "source delegates_to[0].git", srcMf.DelegatesTo[0].Git, delegateURL)

	infoOut, _ := s.mustRunYnh(t, "info", "local/minimal", "--format", "json")
	if !strings.Contains(infoOut, delegateURL) {
		t.Errorf("ynh info did not surface the new delegate — write/read split has regressed.\ndelegateURL=%q\noutput:\n%s", delegateURL, infoOut)
	}

	if _, err := os.Stat(copyDir); !os.IsNotExist(err) {
		t.Errorf("expected no copy dir for local install, got err=%v", err)
	}

	s.mustRunYnh(t, "delegate", "remove", "local/minimal", delegateURL, "--path", "e2e-fixtures/fork-source")
	srcMf = readManifest(t, sourceDir)
	if len(srcMf.DelegatesTo) != 0 {
		t.Errorf("source dir: expected 0 delegates after remove, got %d", len(srcMf.DelegatesTo))
	}
	infoOut, _ = s.mustRunYnh(t, "info", "local/minimal", "--format", "json")
	if strings.Contains(infoOut, delegateURL) {
		t.Errorf("ynh info still surfaces removed delegate — read path went stale.\noutput:\n%s", infoOut)
	}
}

// TestLocalInstall_PluginEditVisibleToInfo locks in the most user-visible
// half of the schema-3 contract: a hand-edit to the source tree's
// plugin.json (the exact thing the user did on their other laptop and
// expected to see) shows up in ynh info immediately, with no `ynh
// update` step in between. This is the test that would have caught the
// original bug from the user's perspective.
func TestLocalInstall_PluginEditVisibleToInfo(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")

	s.mustRunYnh(t, "install", sourceDir)

	// Hand-edit the user's source tree to add a focus.
	mfPath := filepath.Join(sourceDir, ".ynh-plugin", "plugin.json")
	body, err := os.ReadFile(mfPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var mf map[string]any
	if err := json.Unmarshal(body, &mf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mf["focuses"] = map[string]any{
		"hand-edit-focus": map[string]any{
			"prompt": "added by hand, must be visible to ynh info with no sync step",
		},
	}
	edited, _ := json.MarshalIndent(mf, "", "  ")
	if err := os.WriteFile(mfPath, edited, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	infoOut, _ := s.mustRunYnh(t, "info", "local/minimal", "--format", "json")
	if !strings.Contains(infoOut, "hand-edit-focus") {
		t.Errorf("hand-edit to source plugin.json not visible to ynh info — schema-3 read path is broken.\noutput:\n%s", infoOut)
	}
}

func readManifest(t *testing.T, harnessDir string) pluginManifest {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(harnessDir, ".ynh-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("reading plugin.json: %v", err)
	}
	var mf pluginManifest
	if err := json.Unmarshal(body, &mf); err != nil {
		t.Fatalf("parsing plugin.json: %v\n%s", err, body)
	}
	return mf
}
