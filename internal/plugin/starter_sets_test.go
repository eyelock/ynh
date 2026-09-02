package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The starter sets are the sensor library this repository already ships, and
// they are where a new harness author begins. They were JSON inside markdown
// fences, so nothing checked them: a field rename, a tightened enum or a typo
// would have left them stale in silence while `make check` linted the prose
// around them (#366).
//
// This extracts each fenced block and validates it the way a real manifest is
// validated, so the library is gated by the same rules as the product.
func starterSetsPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/plugin -> repo root
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(root, "skills", "ynh-sensors", "references", "starter-sets.md")
}

var jsonFence = regexp.MustCompile("(?s)```json\n(.*?)```")

func TestStarterSets_ValidateAsSensorDeclarations(t *testing.T) {
	path := starterSetsPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		// Not a skip. The file is committed; its absence is the failure.
		t.Fatalf("starter sets not found at %s: %v", path, err)
	}

	blocks := jsonFence.FindAllStringSubmatch(string(data), -1)
	if len(blocks) == 0 {
		t.Fatal("no json blocks found; the extractor is wrong or the file lost its examples")
	}

	sensorsSeen := 0
	for i, b := range blocks {
		body := strings.TrimSpace(b[1])
		// Blocks are manifest fragments, so wrap them into an object.
		var doc struct {
			Sensors map[string]Sensor `json:"sensors"`
		}
		if err := json.Unmarshal([]byte("{"+strings.TrimSuffix(body, ",")+"}"), &doc); err != nil {
			t.Errorf("block %d does not parse as a manifest fragment: %v", i+1, err)
			continue
		}
		if len(doc.Sensors) == 0 {
			continue // a block that is not a sensor set
		}
		sensorsSeen += len(doc.Sensors)

		for _, issue := range ValidateSensors(doc.Sensors, nil, nil) {
			t.Errorf("block %d: %s", i+1, issue)
		}
	}

	if sensorsSeen == 0 {
		t.Fatal("no sensors were validated; this test asserts nothing")
	}
	t.Logf("validated %d sensors across %d blocks", sensorsSeen, len(blocks))
}

// A sensor wrapping a tool that prints more than findings needs output.match,
// or fixing one finding registers the changed summary as a new one (#338).
// The library shipped without a single one, which is what #366 recorded.
func TestStarterSets_DecoratedToolsDeclareAMatch(t *testing.T) {
	data, err := os.ReadFile(starterSetsPath(t))
	if err != nil {
		t.Fatal(err)
	}

	// Tools known to print summaries, headers or context alongside findings.
	decorated := []string{
		"golangci-lint", "go test", "tsc", "ruff", "mypy", "pytest",
		"cargo clippy", "cargo test", "prettier",
	}

	for i, b := range jsonFence.FindAllStringSubmatch(string(data), -1) {
		var doc struct {
			Sensors map[string]Sensor `json:"sensors"`
		}
		if json.Unmarshal([]byte("{"+strings.TrimSuffix(strings.TrimSpace(b[1]), ",")+"}"), &doc) != nil {
			continue
		}
		for name, s := range doc.Sensors {
			cmd := s.Source.Command
			if cmd == "" {
				continue
			}
			for _, tool := range decorated {
				if !strings.Contains(cmd, tool) {
					continue
				}
				if s.Output.Match == "" {
					t.Errorf("block %d, sensor %q wraps %s and declares no output.match; "+
						"its summary lines will be recorded as findings", i+1, name, tool)
				}
				break
			}
		}
	}
}

// Every declared pattern must compile, or the sensor silently selects nothing
// on the day someone runs it.
func TestStarterSets_MatchPatternsCompile(t *testing.T) {
	data, err := os.ReadFile(starterSetsPath(t))
	if err != nil {
		t.Fatal(err)
	}
	compiled := 0
	for i, b := range jsonFence.FindAllStringSubmatch(string(data), -1) {
		var doc struct {
			Sensors map[string]Sensor `json:"sensors"`
		}
		if json.Unmarshal([]byte("{"+strings.TrimSuffix(strings.TrimSpace(b[1]), ",")+"}"), &doc) != nil {
			continue
		}
		for name, s := range doc.Sensors {
			if s.Output.Match == "" {
				continue
			}
			if _, err := regexp.Compile(s.Output.Match); err != nil {
				t.Errorf("block %d, sensor %q: output.match does not compile: %v", i+1, name, err)
				continue
			}
			compiled++
		}
	}
	if compiled == 0 {
		t.Fatal("no patterns were compiled; this test asserts nothing")
	}
	t.Logf("compiled %d match patterns", compiled)
}
