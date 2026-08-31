package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// A sensor command runs in the tree being measured, which is the point of a
// sensor. That left a harness no way to address a script it ships itself:
// "./tools/lint.sh" resolves against the measured tree, so it runs whichever
// copy that tree holds and exits 127 on any commit predating the script.
//
// reference.path already resolves against the harness. This gives commands
// the same reach without moving where they run.
func TestSensorCommand_CanAddressTheHarnessDir(t *testing.T) {
	harnessDir := t.TempDir()
	measured := t.TempDir() // deliberately has no copy of the script

	tools := filepath.Join(harnessDir, "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(tools, "probe.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho harness-copy\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := &harness.Harness{Dir: harnessDir}
	s := plugin.Sensor{
		Source: plugin.SensorSource{Command: `"$YNH_HARNESS_DIR/tools/probe.sh"`},
		Output: plugin.SensorOutput{Format: "text"},
	}

	res, err := runSensor(p, "probe", s, measured, true)
	if err != nil {
		t.Fatalf("runSensor: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.ExitCode, res.Output.Stderr)
	}
	if !strings.Contains(res.Output.Stdout, "harness-copy") {
		t.Errorf("the harness's script did not run: %q", res.Output.Stdout)
	}
}

// The command must still run IN the measured tree. If it ran in the harness
// directory instead, every sensor would measure the wrong thing.
func TestSensorCommand_StillRunsInTheMeasuredTree(t *testing.T) {
	harnessDir := t.TempDir()
	measured := t.TempDir()

	p := &harness.Harness{Dir: harnessDir}
	s := plugin.Sensor{
		Source: plugin.SensorSource{Command: "pwd"},
		Output: plugin.SensorOutput{Format: "text"},
	}
	res, err := runSensor(p, "probe", s, measured, true)
	if err != nil {
		t.Fatalf("runSensor: %v", err)
	}
	got := strings.TrimSpace(res.Output.Stdout)
	wantResolved, _ := filepath.EvalSymlinks(measured)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("command ran in %q, want the measured tree %q", got, measured)
	}
}

// A command that never mentions the variable behaves exactly as before.
func TestSensorCommand_UnaffectedWhenUnused(t *testing.T) {
	measured := t.TempDir()
	if err := os.WriteFile(filepath.Join(measured, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &harness.Harness{Dir: t.TempDir()}
	s := plugin.Sensor{
		Source: plugin.SensorSource{Command: "ls marker"},
		Output: plugin.SensorOutput{Format: "text"},
	}
	res, err := runSensor(p, "probe", s, measured, true)
	if err != nil {
		t.Fatalf("runSensor: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("a command not using the variable should be unaffected, exit %d", res.ExitCode)
	}
}
