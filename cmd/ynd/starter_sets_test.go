package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The starter sets are JSON inside markdown fences, so nothing checked them: a
// field rename or a tightened enum would have left them stale in silence
// (#366). Each fenced sensor block is wrapped into a manifest and run through
// the same checks `ynd validate` applies to a real one.
func TestStarterSets_ValidateAsSensorDeclarations(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	path := filepath.Join(root, "skills", "ynh-sensors", "references", "starter-sets.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("starter sets not found at %s: %v", path, err)
	}

	blocks := regexp.MustCompile("(?s)```json\n(.*?)```").FindAllStringSubmatch(string(data), -1)
	if len(blocks) == 0 {
		t.Fatal("no json blocks found; the extractor is wrong or the file lost its examples")
	}
	seen := 0
	for i, b := range blocks {
		manifest := []byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"set","version":"1.0.0",` +
			strings.TrimSuffix(strings.TrimSpace(b[1]), ",") + `}`)
		var hj map[string]any
		if err := json.Unmarshal(manifest, &hj); err != nil {
			t.Errorf("block %d does not parse as a manifest fragment: %v", i+1, err)
			continue
		}
		sensors, _ := hj["sensors"].(map[string]any)
		if len(sensors) == 0 {
			continue // a block that is not a sensor set
		}
		seen += len(sensors)

		dir := t.TempDir()
		if err := writePluginJSONFile(dir, manifest); err != nil {
			t.Fatal(err)
		}
		// A reference fixture ships with the set; the document only shows
		// the declaration, so provide the directory it names.
		for _, raw := range sensors {
			if ref, _ := raw.(map[string]any)["reference"].(map[string]any); ref != nil {
				if p, _ := ref["path"].(string); p != "" && filepath.IsLocal(p) {
					mkdirAll(t, filepath.Join(dir, p))
				}
			}
		}
		var problems []string
		for _, si := range lintHarnessJSON(harnessManifestPath(dir)) {
			problems = append(problems, si.Message)
		}
		problems = append(problems, validateHarnessSensors(hj)...)
		problems = append(problems, validateSensorDetails(dir, manifest)...)
		for _, p := range problems {
			t.Errorf("block %d: %s", i+1, p)
		}
	}
	if seen == 0 {
		t.Fatal("no sensors were validated; this test asserts nothing")
	}
}
