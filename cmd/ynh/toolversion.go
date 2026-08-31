package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// toolVersionTimeout bounds a version probe. A `--version` flag that opens a
// pager or waits on stdin would otherwise hang the gate, and a gate that hangs
// is worse than one with no version recorded.
const toolVersionTimeout = 5 * time.Second

// maxToolVersion caps what is recorded. Some tools print a banner; the first
// line is the version, the rest is decoration.
const maxToolVersion = 200

// toolVersions caches probes for the life of the process, keyed by command.
//
// The inner loop runs the gate every turn, and running `golangci-lint
// --version` once per sensor per turn would add real wall-clock to every
// iteration to re-learn a constant. Cached per process, not per run, because
// a tool cannot change underneath a single invocation.
var toolVersions sync.Map

// toolVersion runs a sensor's declared version_command and returns the first
// non-empty line of its output.
//
// Failure is never a sensor failure. A missing tool, a non-zero exit, a
// timeout — all yield an empty string, and the sensor result simply carries no
// version. Provenance is worth having; it is not worth failing a gate for.
func toolVersion(cmdline, cwd, harnessDir string) string {
	if strings.TrimSpace(cmdline) == "" {
		return ""
	}
	key := cwd + "\x00" + cmdline
	if v, ok := toolVersions.Load(key); ok {
		return v.(string)
	}

	ctx, cancel := context.WithTimeout(context.Background(), toolVersionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdline)
	cmd.Dir = cwd
	// Same reach as a sensor command: a version_command may invoke a script
	// the harness ships, and without this it could only address the measured
	// tree.
	cmd.Env = append(os.Environ(), "YNH_HARNESS_DIR="+harnessDir)
	// CommandContext kills the shell when the context expires, but Run blocks
	// until the output pipes close — and a shell that forks rather than execs
	// leaves its child holding them open. `sh -c "sleep 30"` therefore ran the
	// full thirty seconds on Linux while returning promptly on macOS, so the
	// bound was real only on the platform this was written on. WaitDelay caps
	// how long Run waits for that I/O after the kill.
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// A version comes from stdout. Combining the streams meant a missing tool
	// recorded `sh: foo: command not found` as its version — which would then
	// sit in every sensor result and in a graded corpus looking like real
	// provenance.
	//
	// stderr is read only on a clean exit, because some tools genuinely print
	// there (`java -version` is the classic). A non-zero exit with nothing on
	// stdout is a failed probe, not a version.
	version := firstLine(stdout.String())
	if version == "" && err == nil {
		version = firstLine(stderr.String())
	}
	if len(version) > maxToolVersion {
		version = version[:maxToolVersion]
	}
	// A timeout means the probe is unusable, whatever it managed to print.
	if ctx.Err() != nil {
		version = ""
	}

	toolVersions.Store(key, version)
	return version
}

// firstLine returns the first non-empty, trimmed line.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
