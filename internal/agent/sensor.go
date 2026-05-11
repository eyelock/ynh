package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// SensorResult is the parsed output of `ynh sensors run`.
// Wire shape mirrors cmd/ynh sensors.go sensorRunResult exactly.
type SensorResult struct {
	Name       string          `json:"name"`
	Kind       string          `json:"kind"` // files | command | focus
	Role       string          `json:"role,omitempty"`
	Category   string          `json:"category,omitempty"`
	ExitCode   int             `json:"exit_code"`
	DurationMS int64           `json:"duration_ms"`
	Output     SensorRunOutput `json:"output"`
}

// Passed applies loop-driver policy:
//   - command sensors: exit_code == 0
//   - files sensors: at least one file matched
//   - focus sensors: always informational (true); driver handles agent invocation
func (r *SensorResult) Passed() bool {
	switch r.Kind {
	case "command":
		return r.ExitCode == 0
	case "files":
		return len(r.Output.Files) > 0
	case "focus":
		return true
	default:
		return r.ExitCode == 0
	}
}

// Summary returns a short human-readable description of the sensor result.
func (r *SensorResult) Summary() string {
	switch r.Kind {
	case "command":
		if r.ExitCode == 0 {
			return "passed"
		}
		if r.Output.Stdout != "" {
			lines := strings.SplitN(strings.TrimSpace(r.Output.Stdout), "\n", 4)
			if len(lines) > 3 {
				lines = append(lines[:3], "…")
			}
			return strings.Join(lines, "\n")
		}
		if r.Output.Stderr != "" {
			lines := strings.SplitN(strings.TrimSpace(r.Output.Stderr), "\n", 4)
			if len(lines) > 3 {
				lines = append(lines[:3], "…")
			}
			return strings.Join(lines, "\n")
		}
		return fmt.Sprintf("failed (exit %d)", r.ExitCode)
	case "files":
		if len(r.Output.Files) == 0 {
			return "no files matched"
		}
		return fmt.Sprintf("%d file(s)", len(r.Output.Files))
	case "focus":
		return "focus sensor (loop driver invokes agent runtime)"
	default:
		return ""
	}
}

// SensorRunOutput mirrors the wire shape from ynh sensors run.
type SensorRunOutput struct {
	Format  string       `json:"format"`
	Channel string       `json:"channel,omitempty"`
	Stdout  string       `json:"stdout,omitempty"`
	Stderr  string       `json:"stderr,omitempty"`
	Files   []SensorFile `json:"files,omitempty"`
	Note    string       `json:"note,omitempty"`
}

// SensorFile is a file artifact returned by a files-sourced sensor.
type SensorFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content,omitempty"`
}

// runSensorFn is the function used to execute sensors.
// Replaceable in tests without touching the real ynh binary.
var runSensorFn = defaultRunSensor

// RunSensor executes `ynh sensors run <harness> <name>` and returns the
// parsed result. The ynh binary at ynhPath is used; cwd is passed as
// --cwd if non-empty.
func RunSensor(ynhPath, harnessName, sensorName, cwd string) (*SensorResult, error) {
	return runSensorFn(ynhPath, harnessName, sensorName, cwd)
}

func defaultRunSensor(ynhPath, harnessName, sensorName, cwd string) (*SensorResult, error) {
	args := []string{"sensors", "run", harnessName, sensorName, "--format", "json"}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}

	cmd := exec.Command(ynhPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("ynh sensors run: %s", strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("ynh sensors run: %w", err)
	}

	var r SensorResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		return nil, fmt.Errorf("parsing sensor result: %w", err)
	}
	return &r, nil
}

// SensorHash produces a stable short hash over a set of sensor results.
// Used by the watchdog to detect when sensors stop changing between turns.
func SensorHash(results []*SensorResult) string {
	var sb strings.Builder
	for _, r := range results {
		fmt.Fprintf(&sb, "%s:%d|", r.Name, r.ExitCode)
	}
	return contentHash(sb.String())
}

// sensorTier returns a sort priority for sensor execution order.
// Build sensors run before lint, lint before test, everything else last.
func sensorTier(category string) int {
	switch strings.ToLower(category) {
	case "build":
		return 1
	case "lint":
		return 2
	case "test":
		return 3
	default:
		return 4
	}
}
