package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/eyelock/ynh/internal/baseline"
)

// SensorResult is the parsed output of `ynh sensors run`.
// Wire shape mirrors cmd/ynh sensors.go sensorRunResult exactly.
type SensorResult struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"` // files | command | focus
	Role     string `json:"role,omitempty"`
	Category string `json:"category,omitempty"`
	// Tolerance mirrors the sensor declaration. The loop must honour it or it
	// contradicts `ynh check` on the same manifest: advisory and report
	// sensors are non-gating by definition and cannot hold convergence open.
	Tolerance  string          `json:"tolerance,omitempty"`
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
// parsed result. overlayJSON is an optional partial sensor JSON (e.g.
// `{"source":{"command":"make fast"}}`) merged over the base declaration
// by the ynh binary before execution. Pass "" for no overlay.
func RunSensor(ynhPath, harnessName, sensorName, cwd, overlayJSON string) (*SensorResult, error) {
	return runSensorFn(ynhPath, harnessName, sensorName, cwd, overlayJSON)
}

func defaultRunSensor(ynhPath, harnessName, sensorName, cwd, overlayJSON string) (*SensorResult, error) {
	args := []string{"sensors", "run", harnessName, sensorName, "--format", "json"}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if overlayJSON != "" {
		args = append(args, "--sensor-overlay-json", overlayJSON)
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

// SensorHash produces a stable short hash over a set of sensor results, used
// by the watchdog to detect when sensors stop changing between turns.
//
// The hash covers each sensor's output, not just its exit code. Hashing
// name and exit code alone meant that fixing three of fifty lint findings
// produced an identical hash — real progress read as no progress, and the
// watchdog killed the run at the no-progress threshold for doing exactly what
// it was asked to do.
//
// Output is fingerprinted rather than hashed raw so that file positions
// shifting (a line inserted above an existing finding) does not read as
// progress either. The same normalisation the baseline uses.
func SensorHash(results []*SensorResult) string {
	var sb strings.Builder
	for _, r := range results {
		fmt.Fprintf(&sb, "%s:%d:", r.Name, r.ExitCode)
		for _, fp := range baseline.Fingerprints(r.Output.Stdout+"\n"+r.Output.Stderr, "") {
			sb.WriteString(fp)
			sb.WriteByte(',')
		}
		sb.WriteByte('|')
	}
	return contentHash(sb.String())
}

// sensorTier returns a sort priority for sensor execution order, cheapest
// signal first so an obviously broken turn fails fast.
//
// This switched on "build", "lint" and "test", which are not sensor
// categories — the schema permits only maintainability, architecture and
// behaviour, and validation enforces it. Every sensor therefore landed in the
// default tier and ordering was alphabetical, so the function did nothing.
func sensorTier(category string) int {
	switch strings.ToLower(category) {
	case "maintainability": // formatters, linters, vet — fast
		return 1
	case "architecture": // structural checks — usually cheap
		return 2
	case "behaviour": // test suites — slowest
		return 3
	default: // uncategorised
		return 4
	}
}
