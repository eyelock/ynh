package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/eyelock/ynh/internal/gate"
)

// runCheckFn is the function used to run the gate. Replaceable in tests
// without a real ynh binary on disk.
var runCheckFn = defaultRunCheck

// RunCheck executes `ynh check <harness> --format json` and returns the
// envelope.
//
// The loop consumes the gate rather than running sensors itself so that one
// policy decides what blocks. Driving `ynh sensors run` per sensor and
// re-deriving pass/fail meant the loop consulted no baseline: on any
// repository with pre-existing failures — which is every real repository, and
// every corpus repo a factory would run against — convergence required the
// agent to fix debt nobody asked it to fix, and the run spent its whole
// budget on someone else's lint before reaching the task.
//
// Consuming the gate also inherits its tolerance policy and its verdict, so
// the loop and `ynh check` can no longer reach opposite conclusions about one
// manifest.
func RunCheck(ynhPath, harnessName, cwd string, only []string, overlay map[string]json.RawMessage) (*gate.Envelope, error) {
	return runCheckFn(ynhPath, harnessName, cwd, only, overlay)
}

func defaultRunCheck(ynhPath, harnessName, cwd string, only []string, overlay map[string]json.RawMessage) (*gate.Envelope, error) {
	args := []string{"check", harnessName, "--format", "json"}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if len(only) > 0 {
		args = append(args, "--only", strings.Join(only, ","))
	}
	if len(overlay) > 0 {
		raw, err := json.Marshal(overlay)
		if err != nil {
			return nil, fmt.Errorf("encoding sensor overlay: %w", err)
		}
		args = append(args, "--sensor-overlay", string(raw))
	}

	cmd := exec.Command(ynhPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// Exit 1 means blocking sensors failed and the report is on stdout —
	// the normal, expected outcome of a mid-loop turn, not an error. Exit 2
	// means the gate itself could not run. Treating them alike would make a
	// failing test indistinguishable from a broken harness, and the loop
	// would keep feeding the agent an empty sensor block while the real
	// fault went unreported.
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			if stderr.Len() > 0 {
				return nil, fmt.Errorf("ynh check: %s", strings.TrimSpace(stderr.String()))
			}
			return nil, fmt.Errorf("ynh check: %w", err)
		}
	}

	var env gate.Envelope
	if uErr := json.Unmarshal(stdout.Bytes(), &env); uErr != nil {
		return nil, fmt.Errorf("parsing check result: %w", uErr)
	}
	return &env, nil
}
