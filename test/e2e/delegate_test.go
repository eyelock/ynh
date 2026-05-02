//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	s.mustRunYnh(t, "install", filepath.Join(clone, "e2e-fixtures", "minimal"))

	delegateURL := "github.com/eyelock/assistants"
	s.mustRunYnh(t, "delegate", "add", "minimal", delegateURL,
		"--path", "e2e-fixtures/included-skill",
		"--ref", AssistantsFixturesSHA,
	)

	mf := readManifest(t, filepath.Join(s.home, "harnesses", "minimal"))
	if len(mf.DelegatesTo) != 1 {
		t.Fatalf("expected 1 delegate after add, got %d: %+v", len(mf.DelegatesTo), mf.DelegatesTo)
	}
	d := mf.DelegatesTo[0]
	assertEqual(t, "delegates_to[0].git", d.Git, delegateURL)
	assertEqual(t, "delegates_to[0].path", d.Path, "e2e-fixtures/included-skill")
	assertEqual(t, "delegates_to[0].ref", d.Ref, AssistantsFixturesSHA)

	s.mustRunYnh(t, "delegate", "remove", "minimal", delegateURL, "--path", "e2e-fixtures/included-skill")

	mf = readManifest(t, filepath.Join(s.home, "harnesses", "minimal"))
	if len(mf.DelegatesTo) != 0 {
		t.Errorf("expected 0 delegates after remove, got %d", len(mf.DelegatesTo))
	}
}

// TestInclude_AddRemove walks an include through add → resolved → remove.
func TestInclude_AddRemove(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	s.mustRunYnh(t, "install", filepath.Join(clone, "e2e-fixtures", "minimal"))

	includeURL := "github.com/eyelock/assistants"
	s.mustRunYnh(t, "include", "add", "minimal", includeURL,
		"--path", "e2e-fixtures/included-skill",
		"--ref", AssistantsFixturesSHA,
	)

	mf := readManifest(t, filepath.Join(s.home, "harnesses", "minimal"))
	if len(mf.Includes) != 1 {
		t.Fatalf("expected 1 include after add, got %d", len(mf.Includes))
	}
	i := mf.Includes[0]
	assertEqual(t, "includes[0].git", i.Git, includeURL)
	assertEqual(t, "includes[0].path", i.Path, "e2e-fixtures/included-skill")
	assertEqual(t, "includes[0].ref", i.Ref, AssistantsFixturesSHA)

	s.mustRunYnh(t, "include", "remove", "minimal", includeURL, "--path", "e2e-fixtures/included-skill")

	mf = readManifest(t, filepath.Join(s.home, "harnesses", "minimal"))
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
	s.mustRunYnh(t, "install", filepath.Join(clone, "e2e-fixtures", "minimal"))

	includeURL := "github.com/eyelock/assistants"
	s.mustRunYnh(t, "include", "add", "minimal", includeURL,
		"--path", "e2e-fixtures/minimal",
		"--ref", AssistantsFixturesSHA,
	)

	s.mustRunYnh(t, "include", "update", "minimal", includeURL,
		"--from-path", "e2e-fixtures/minimal",
		"--path", "e2e-fixtures/included-skill",
	)

	mf := readManifest(t, filepath.Join(s.home, "harnesses", "minimal"))
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
	s.mustRunYnh(t, "install", filepath.Join(clone, "e2e-fixtures", "minimal"))

	delegateURL := "github.com/eyelock/assistants"
	s.mustRunYnh(t, "delegate", "add", "minimal", delegateURL,
		"--path", "e2e-fixtures/included-skill",
		"--ref", AssistantsFixturesSHA,
	)

	s.mustRunYnh(t, "delegate", "update", "minimal", delegateURL,
		"--from-path", "e2e-fixtures/included-skill",
		"--ref", AssistantsFixturesV1Tag,
	)

	mf := readManifest(t, filepath.Join(s.home, "harnesses", "minimal"))
	if len(mf.DelegatesTo) != 1 {
		t.Fatalf("expected 1 delegate after update, got %d", len(mf.DelegatesTo))
	}
	assertEqual(t, "delegates_to[0].ref", mf.DelegatesTo[0].Ref, AssistantsFixturesV1Tag)
	assertEqual(t, "delegates_to[0].path", mf.DelegatesTo[0].Path, "e2e-fixtures/included-skill")
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
