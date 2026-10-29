package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

// includedSensorSetup installs a consumer harness that includes a second
// harness by absolute path. The second ships tools/x.sh and a calibration
// fixture, and declares a sensor addressing both through its own directory.
// The consumer has neither. includeExtra is spliced into the include entry.
func includedSensorSetup(t *testing.T, includeExtra string) (publisher, tree string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "")

	publisher = t.TempDir()
	for _, d := range []string{"tools", "fixtures/bad"} {
		if err := os.MkdirAll(filepath.Join(publisher, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\nprintf '%s|%s' \"$PWD\" \"$YNH_HARNESS_DIR\" > \"$PWD/ran.txt\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(publisher, "tools", "x.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pub := `{"name":"pub","version":"1.0.0","default_vendor":"claude","sensors":{
      "pubsensor":{"tolerance":"blocking","source":{"command":"$YNH_HARNESS_DIR/tools/x.sh"},
        "output":{"format":"text"},
        "reference":{"path":"fixtures/bad","expect":"fail"}}}}`
	if err := writePluginJSONFile(publisher, []byte(pub)); err != nil {
		t.Fatal(err)
	}

	inc, err := json.Marshal(map[string]string{"local": publisher})
	if err != nil {
		t.Fatal(err)
	}
	incStr := string(inc)
	if includeExtra != "" {
		incStr = strings.TrimSuffix(incStr, "}") + "," + includeExtra + "}"
	}
	installListTestHarness(t, home, "con",
		`{"name":"con","version":"0.1.0","default_vendor":"claude","includes":[`+incStr+`]}`)
	return publisher, t.TempDir()
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// An included sensor's command and fixture belong to the harness that
// published it. The command still runs in, and measures, the tree under test.
func TestCheck_IncludedSensorResolvesAgainstItsOwnHarness(t *testing.T) {
	publisher, tree := includedSensorSetup(t, "")

	var out bytes.Buffer
	var stderr bytes.Buffer
	err := cmdCheck([]string{"local/con", "--format", "json", "--cwd", tree}, &out, &stderr)
	var env gate.Envelope
	if jErr := json.Unmarshal(out.Bytes(), &env); jErr != nil {
		t.Fatalf("decoding: %v\n%s (err %v, stderr %s)", jErr, out.String(), err, stderr.String())
	}
	res := sensorNamed(t, env, "pubsensor")
	if gate.CommandDidNotRun(res.ExitCode) {
		t.Fatalf("the included script was not found (exit %d): it resolved against the consumer", res.ExitCode)
	}
	got, rErr := os.ReadFile(filepath.Join(tree, "ran.txt"))
	if rErr != nil {
		t.Fatalf("the script did not run in the tree under test: %v", rErr)
	}
	parts := strings.SplitN(string(got), "|", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected marker %q", got)
	}
	if realPath(t, parts[0]) != realPath(t, tree) {
		t.Errorf("cwd = %q, want the tree under test %q", parts[0], tree)
	}
	if realPath(t, parts[1]) != realPath(t, publisher) {
		t.Errorf("YNH_HARNESS_DIR = %q, want the include %q", parts[1], publisher)
	}

	var cal bytes.Buffer
	calErr := cmdCheck([]string{"local/con", "--calibrate", "--format", "json", "--cwd", tree}, &cal, io.Discard)
	var cenv gate.CalibrationEnvelope
	if jErr := json.Unmarshal(cal.Bytes(), &cenv); jErr != nil {
		t.Fatalf("decoding calibration: %v\n%s (err %v)", jErr, cal.String(), calErr)
	}
	if len(cenv.Sensors) != 1 || cenv.Sensors[0].Status != gate.CalibCalibrated {
		t.Errorf("calibration = %+v (err %v), want the include's fixture found and the sensor calibrated", cenv.Sensors, calErr)
	}
}

// "sensors": false adopts a harness's artifacts without its gate.
func TestCheck_IncludeWithSensorsFalseContributesNone(t *testing.T) {
	_, tree := includedSensorSetup(t, `"sensors":false`)

	var out bytes.Buffer
	_ = cmdCheck([]string{"local/con", "--format", "json", "--cwd", tree}, &out, io.Discard)
	if strings.Contains(out.String(), "pubsensor") {
		t.Errorf("an include with sensors:false must contribute no sensors:\n%s", out.String())
	}
}
