package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// writeHarness creates a harness directory declaring the given sensors.
func writeHarness(t *testing.T, dir, name string, sensors map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	hj := map[string]any{"name": name, "version": "1.0.0"}
	if sensors != nil {
		hj["sensors"] = sensors
	}
	data, err := json.Marshal(hj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func commandSensor(tolerance string) map[string]any {
	return map[string]any{
		"category":  "maintainability",
		"tolerance": tolerance,
		"source":    map[string]any{"command": "true"},
		"output":    map[string]any{"format": "text", "channel": "stdout+exit"},
	}
}

// consumerWith builds a harness including one local set.
func consumerWith(t *testing.T, own map[string]plugin.Sensor, upstream map[string]any) *harness.Harness {
	t.Helper()
	root := t.TempDir()
	writeHarness(t, filepath.Join(root, "vendored", "up"), "up", upstream)
	return &harness.Harness{
		Name:     "consumer",
		Dir:      root,
		Sensors:  own,
		Includes: []harness.Include{{GitSource: harness.GitSource{Local: "vendored/up"}}},
	}
}

func TestMergeIncludedSensorsBringsThemAcross(t *testing.T) {
	h := consumerWith(t, nil, map[string]any{"go-vet": commandSensor("blocking")})
	merged, err := MergeIncludedSensors(h, nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	s, ok := merged["go-vet"]
	if !ok {
		t.Fatalf("included sensor missing; got %v", keys(merged))
	}
	if s.Tolerance != "blocking" {
		t.Errorf("tolerance = %q, want blocking", s.Tolerance)
	}
}

// The consumer's own sensors must survive the merge untouched.
func TestMergeKeepsOwnSensors(t *testing.T) {
	own := map[string]plugin.Sensor{"mine": {Tolerance: "report"}}
	h := consumerWith(t, own, map[string]any{"go-vet": commandSensor("blocking")})
	merged, err := MergeIncludedSensors(h, nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(merged) != 2 {
		t.Errorf("want both sensors, got %v", keys(merged))
	}
}

// Severity is the consumer's to choose; nobody adopts a set that fails their
// repository on day one.
func TestOverrideChangesToleranceOnly(t *testing.T) {
	h := consumerWith(t, nil, map[string]any{"go-vet": commandSensor("blocking")})
	merged, err := MergeIncludedSensors(h, map[string]plugin.SensorOverride{
		"go-vet": {Tolerance: "advisory"},
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := merged["go-vet"].Tolerance; got != "advisory" {
		t.Errorf("tolerance = %q, want advisory", got)
	}
	// The observation itself must be untouched.
	if got := merged["go-vet"].Source.Command; got != "true" {
		t.Errorf("command = %q, want it unchanged", got)
	}
}

// A stale override is the failure a silent no-op hides: the sensor was renamed
// upstream, the softening stopped applying, and the gate began blocking for
// reasons nobody changed.
func TestOverrideForUnknownSensorIsRefused(t *testing.T) {
	h := consumerWith(t, nil, map[string]any{"go-vet": commandSensor("blocking")})
	_, err := MergeIncludedSensors(h, map[string]plugin.SensorOverride{"typo": {Tolerance: "advisory"}})
	if err == nil || !strings.Contains(err.Error(), "no include or this harness declares") {
		t.Errorf("expected a refusal naming the unknown sensor, got: %v", err)
	}
}

func TestOverrideOfOwnSensorIsRefused(t *testing.T) {
	own := map[string]plugin.Sensor{"mine": {Tolerance: "blocking"}}
	h := consumerWith(t, own, nil)
	_, err := MergeIncludedSensors(h, map[string]plugin.SensorOverride{"mine": {Tolerance: "advisory"}})
	if err == nil || !strings.Contains(err.Error(), "edit it directly") {
		t.Errorf("expected a refusal pointing at direct editing, got: %v", err)
	}
}

func TestOverrideRejectsAnInvalidTolerance(t *testing.T) {
	h := consumerWith(t, nil, map[string]any{"go-vet": commandSensor("blocking")})
	_, err := MergeIncludedSensors(h, map[string]plugin.SensorOverride{"go-vet": {Tolerance: "sometimes"}})
	if err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Errorf("expected a tolerance refusal, got: %v", err)
	}
}

// Silently preferring one would mean the gate observes something other than
// what the manifest appears to say, and no resolution order is guessable by a
// reader.
func TestCollidingSensorNamesAreRefused(t *testing.T) {
	root := t.TempDir()
	writeHarness(t, filepath.Join(root, "a"), "a", map[string]any{"lint": commandSensor("blocking")})
	writeHarness(t, filepath.Join(root, "b"), "b", map[string]any{"lint": commandSensor("advisory")})
	h := &harness.Harness{Name: "c", Dir: root, Includes: []harness.Include{
		{GitSource: harness.GitSource{Local: "a"}},
		{GitSource: harness.GitSource{Local: "b"}},
	}}
	_, err := MergeIncludedSensors(h, nil)
	if err == nil || !strings.Contains(err.Error(), "declared by both") {
		t.Errorf("expected a collision refusal, got: %v", err)
	}
}

// An include that ships artifacts by directory layout and declares no manifest
// is legitimate and simply contributes no sensors.
func TestIncludeWithoutAManifestIsNotAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &harness.Harness{Name: "c", Dir: root, Includes: []harness.Include{
		{GitSource: harness.GitSource{Local: "plain"}},
	}}
	if _, err := MergeIncludedSensors(h, nil); err != nil {
		t.Errorf("an include without a manifest must not fail the merge: %v", err)
	}
}

func keys(m map[string]plugin.Sensor) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
