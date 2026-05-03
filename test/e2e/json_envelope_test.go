//go:build e2e

package e2e

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// envelopeLs is a subset of the `ynh ls --format json` shape.
type envelopeLs struct {
	Capabilities string         `json:"capabilities"`
	YnhVersion   string         `json:"ynh_version"`
	Harnesses    []envelopeItem `json:"harnesses"`
}

type envelopeItem struct {
	Name             string             `json:"name"`
	Namespace        string             `json:"namespace,omitempty"`
	VersionInstalled string             `json:"version_installed"`
	Description      string             `json:"description,omitempty"`
	DefaultVendor    string             `json:"default_vendor,omitempty"`
	Path             string             `json:"path"`
	IsPinned         bool               `json:"is_pinned"`
	InstalledFrom    installedJSONShape `json:"installed_from"`
	Includes         []any              `json:"includes"`
	DelegatesTo      []any              `json:"delegates_to"`
}

type envelopeInfo struct {
	Capabilities string `json:"capabilities"`
	YnhVersion   string `json:"ynh_version"`
	Harness      struct {
		envelopeItem
		Manifest map[string]any `json:"manifest"`
	} `json:"harness"`
}

func TestLs_JSON_Shape(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	s.mustRunYnh(t, "install", filepath.Join(clone, "e2e-fixtures", "minimal"))

	out, _ := s.mustRunYnh(t, "ls", "--format", "json")

	var got envelopeLs
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing ls JSON: %v\n%s", err, out)
	}
	if got.Capabilities == "" {
		t.Error("capabilities field empty")
	}
	if got.YnhVersion == "" {
		t.Error("ynh_version field empty")
	}
	if len(got.Harnesses) != 1 {
		t.Fatalf("expected 1 harness, got %d", len(got.Harnesses))
	}
	h := got.Harnesses[0]
	assertEqual(t, "harnesses[0].name", h.Name, "minimal")
	assertEqual(t, "harnesses[0].version_installed", h.VersionInstalled, "0.1.0")
	assertEqual(t, "harnesses[0].default_vendor", h.DefaultVendor, "claude")
	assertEqual(t, "harnesses[0].installed_from.source_type", h.InstalledFrom.SourceType, "local")
}

func TestInfo_JSON_Shape(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	s.mustRunYnh(t, "install", filepath.Join(clone, "e2e-fixtures", "minimal"))

	out, _ := s.mustRunYnh(t, "info", "minimal", "--format", "json")

	var got envelopeInfo
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing info JSON: %v\n%s", err, out)
	}
	if got.Capabilities == "" {
		t.Error("capabilities field empty")
	}
	assertEqual(t, "harness.name", got.Harness.Name, "minimal")
	if got.Harness.Manifest == nil {
		t.Fatal("expected manifest to be populated")
	}
	// Manifest is a verbatim echo of plugin.json — assert a few fields.
	if name, _ := got.Harness.Manifest["name"].(string); name != "minimal" {
		t.Errorf("manifest.name = %v, want minimal", got.Harness.Manifest["name"])
	}
	if got.Harness.Manifest["default_vendor"] != "claude" {
		t.Errorf("manifest.default_vendor = %v, want claude", got.Harness.Manifest["default_vendor"])
	}
}

func TestVendors_JSON_Shape(t *testing.T) {
	s := newSandbox(t)
	out, _ := s.mustRunYnh(t, "vendors", "--format", "json")

	var vendors []struct {
		Name      string `json:"name"`
		ConfigDir string `json:"config_dir"`
	}
	if err := json.Unmarshal([]byte(out), &vendors); err != nil {
		t.Fatalf("parsing vendors JSON: %v\n%s", err, out)
	}
	want := map[string]bool{"claude": false, "codex": false, "cursor": false}
	for _, v := range vendors {
		if _, ok := want[v.Name]; ok {
			want[v.Name] = true
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("vendors output missing %q", name)
		}
	}
}
