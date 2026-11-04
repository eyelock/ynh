package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/labels"
	"github.com/eyelock/ynh/internal/migration"
)

// labelHome is a YNH_HOME with the given config.json, and no YNH_LABELS.
func labelHome(t *testing.T, configJSON string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv(labels.Env, "") // restored when the test ends; resolveRunLabels sets it
	if configJSON != "" {
		if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(configJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// The sources merge per key, later winning, and the merged set is what every
// process the run starts inherits as YNH_LABELS.
func TestResolveRunLabels_MergesAndExports(t *testing.T) {
	labelHome(t, `{"labels":{"a":"config","b":"config","c":"config","d":"config","gone":"config"}}`)
	t.Setenv(labels.Env, "c:env,d:env")
	root := &harness.Harness{Labels: map[string]string{"b": "manifest", "c": "manifest", "d": "manifest"}}

	set, err := resolveRunLabels([]string{"d:flag", "gone:"}, root)
	if err != nil {
		t.Fatal(err)
	}
	const want = "a:config,b:manifest,c:env,d:flag"
	if got := labels.Encode(set); got != want {
		t.Errorf("labels = %s, want %s", got, want)
	}
	if got := os.Getenv(labels.Env); got != want {
		t.Errorf("YNH_LABELS = %q, want the merged set %q for every child", got, want)
	}
}

// Removing every label leaves a child no YNH_LABELS at all, not a stale one.
func TestResolveRunLabels_RemovalClearsTheExport(t *testing.T) {
	labelHome(t, `{"labels":{"a":"x"}}`)
	t.Setenv(labels.Env, "b:y")
	if _, err := resolveRunLabels([]string{"a:", "b:"}, nil); err != nil {
		t.Fatal(err)
	}
	if v, ok := os.LookupEnv(labels.Env); ok {
		t.Errorf("YNH_LABELS = %q, want it unset", v)
	}
}

func TestResolveRunLabels_RefusesAndLeavesTheEnvironment(t *testing.T) {
	labelHome(t, "")
	t.Setenv(labels.Env, "keep:me")
	for _, bad := range []string{"dir:/etc", "who:a@b.c", "Bad:v", "novalue"} {
		if _, err := resolveRunLabels([]string{bad}, nil); err == nil {
			t.Errorf("--label %s was accepted", bad)
		}
	}
	if got := os.Getenv(labels.Env); got != "keep:me" {
		t.Errorf("a refused run changed YNH_LABELS to %q", got)
	}
}

func TestResolveRunLabels_ConfigWithABadLabelRefuses(t *testing.T) {
	labelHome(t, `{"labels":{"Bad":"v"}}`)
	if _, err := resolveRunLabels(nil, nil); err == nil || !strings.Contains(err.Error(), "config.json") {
		t.Errorf("err = %v, want the config.json label refused", err)
	}
}

// Only the root harness's labels count: a harness it includes may declare
// its own, and they are ignored.
func TestResolveRunLabels_IncludedHarnessLabelsAreIgnored(t *testing.T) {
	home := labelHome(t, "")
	installListTestHarness(t, home, "inner", `{"name":"inner","version":"1","default_vendor":"claude","labels":{"inner":"yes"}}`)
	installListTestHarness(t, home, "outer", `{"name":"outer","version":"1","default_vendor":"claude","labels":{"outer":"yes"},`+
		`"includes":[{"local":"`+harness.InstalledDirByID("local/inner")+`"}]}`)
	root, err := harness.LoadIDOrPath("local/outer")
	if err != nil {
		t.Fatal(err)
	}
	set, err := resolveRunLabels(nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if got := labels.Encode(set); got != "outer:yes" {
		t.Errorf("labels = %s, want only the root's", got)
	}
}

func TestParseRunArgs_Label(t *testing.T) {
	ra := parseRunArgs([]string{"local/demo", "--label", "wheelhouse:ynw", "--label=batch:7", "--label", "old:", "--model", "haiku", "--", "hi"})
	if got := strings.Join(ra.Labels, " "); got != "wheelhouse:ynw batch:7 old:" {
		t.Errorf("labels = %q", got)
	}
	if ra.HarnessName != "local/demo" || ra.Prompt != "hi" {
		t.Errorf("harness %q prompt %q: --label's value was read as a bare word", ra.HarnessName, ra.Prompt)
	}
	for _, a := range ra.VendorArgs {
		if strings.Contains(a, "ynw") || a == "--label" {
			t.Errorf("--label reached the vendor: %v", ra.VendorArgs)
		}
	}
}

// `ynh run` refuses an invalid label before launching anything.
func TestCmdRun_RefusesAnInvalidLabel(t *testing.T) {
	labelHome(t, "")
	dir := t.TempDir()
	if err := writePluginJSONFile(dir, []byte(`{"name":"demo","version":"1","default_vendor":"claude"}`)); err != nil {
		t.Fatal(err)
	}
	err := cmdRun([]string{dir, "--label", "dir:/etc", "hi"})
	if err == nil || !strings.Contains(err.Error(), `label "dir"`) {
		t.Errorf("err = %v, want the label refused", err)
	}
}

func TestCmdRun_RefusesALabelInTheManifest(t *testing.T) {
	labelHome(t, "")
	dir := t.TempDir()
	if err := writePluginJSONFile(dir, []byte(`{"name":"demo","version":"1","default_vendor":"claude","labels":{"who":"a b"}}`)); err != nil {
		t.Fatal(err)
	}
	err := cmdRun([]string{dir, "hi"})
	if err == nil || !strings.Contains(err.Error(), `label "who"`) {
		t.Errorf("err = %v, want the manifest's label refused", err)
	}
}

// An agent run refuses an invalid label before a worker or any telemetry
// starts, as an invalid-input error.
func TestCmdAgentRun_RefusesAnInvalidLabel(t *testing.T) {
	labelHome(t, "")
	t.Setenv("YNH_AGENT_SESSION", "")
	vendorShims(t, "a refused run must not start a worker")
	spoolDir := filepath.Join(t.TempDir(), "spool")
	t.Setenv("YNR_SPOOL", spoolDir)
	var stdout, stderr bytes.Buffer
	err := cmdAgentRun([]string{"--task", "t", "--harness", "local/absent", "--label", "dir:/etc"}, &stdout, &stderr, strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), `label "dir"`) {
		t.Errorf("err = %v, want the label refused", err)
	}
	if _, serr := os.Stat(spoolDir); serr == nil {
		t.Error("telemetry started for a refused label")
	}
}

// A run's labels, from the harness manifest and --label, reach the worker and
// the sensor `ynh check` runs, and are ynh.label.* on its spooled records.
func TestCmdAgentRun_LabelsReachWorkerSensorAndSpool(t *testing.T) {
	home := labelHome(t, `{"labels":{"site":"lab"}}`)
	t.Setenv("YNH_AGENT_SESSION", "")
	t.Setenv(execMainEnv, "1")
	if err := migration.WriteSchemaVersion(home, migration.CurrentSchemaVersion); err != nil {
		t.Fatal(err)
	}
	sensorEnvFile := filepath.Join(t.TempDir(), "sensor-env")
	installListTestHarness(t, home, "probe", `{"name":"probe","version":"0.1.0","default_vendor":"claude",`+
		`"labels":{"wheelhouse":"ynw","site":"manifest"},`+
		`"sensors":{"probe":{"category":"maintainability","source":{"command":"env > '`+sensorEnvFile+`'"},"output":{"format":"text"}}}}`)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	envFile := convergingClaude(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	t.Setenv("YNR_SPOOL", spoolDir)

	var stdout, stderr bytes.Buffer
	err := cmdAgentRun([]string{"--format", "json", "--worktree", t.TempDir(), "--task", "t", "--no-plan",
		"--harness", "local/probe", "--label", "batch:7", "--label", "site:"}, &stdout, &stderr, strings.NewReader(""))
	if processExitCode(err) != 0 {
		t.Fatalf("run failed: %v\n%s", err, stderr.String())
	}

	const want = "YNH_LABELS=batch:7,wheelhouse:ynw\n"
	worker, _ := os.ReadFile(envFile)
	if !strings.Contains(string(worker), want) {
		t.Errorf("worker env lacks %q:\n%s", want, worker)
	}
	sensor, rerr := os.ReadFile(sensorEnvFile)
	if rerr != nil {
		t.Fatalf("the sensor never ran: %v", rerr)
	}
	if !strings.Contains(string(sensor), want) {
		t.Errorf("sensor env lacks %q:\n%s", want, sensor)
	}

	resources := spoolResourceAttrs(t, spoolDir)
	if len(resources) == 0 {
		t.Fatal("no records in the spool")
	}
	for _, r := range resources {
		if r["ynh.label.wheelhouse"] != "ynw" || r["ynh.label.batch"] != "7" {
			t.Errorf("resource lacks the labels: %v", r)
		}
		if _, ok := r["ynh.label.site"]; ok {
			t.Errorf("resource carries the removed label site: %v", r)
		}
	}
}

// Even a refused run is a ynh.run record, and carries the labels.
func TestCmdAgentRun_RefusedRunRecordCarriesLabels(t *testing.T) {
	labelHome(t, "")
	t.Setenv("YNH_AGENT_SESSION", "")
	vendorShims(t, "a refused run must not start a worker")
	spoolDir := filepath.Join(t.TempDir(), "spool")
	t.Setenv("YNR_SPOOL", spoolDir)
	t.Setenv(labels.Env, "wheelhouse:ynw")
	var stdout, stderr bytes.Buffer
	err := cmdAgentRun([]string{"--format", "json", "--worktree", t.TempDir(), "--task", "t", "--backend", "gemini"},
		&stdout, &stderr, strings.NewReader(""))
	if processExitCode(err) != agent.ExitRefused {
		t.Fatalf("err = %v, want the run refused", err)
	}
	var seen bool
	for _, r := range spoolResourceAttrs(t, spoolDir) {
		seen = true
		if r["ynh.label.wheelhouse"] != "ynw" {
			t.Errorf("resource = %v, want the YNH_LABELS label", r)
		}
	}
	if !seen {
		t.Error("no records in the spool")
	}
}

// spoolResourceAttrs is the string resource attributes of every request in
// the spool.
func spoolResourceAttrs(t *testing.T, dir string) []map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	type resource struct {
		Attributes []struct {
			Key   string
			Value struct{ StringValue string }
		}
	}
	var out []map[string]string
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var req struct {
				ResourceSpans []struct{ Resource resource }
				ResourceLogs  []struct{ Resource resource }
			}
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				t.Fatal(err)
			}
			var rs []resource
			for _, r := range req.ResourceSpans {
				rs = append(rs, r.Resource)
			}
			for _, r := range req.ResourceLogs {
				rs = append(rs, r.Resource)
			}
			for _, r := range rs {
				m := map[string]string{}
				for _, a := range r.Attributes {
					m[a.Key] = a.Value.StringValue
				}
				out = append(out, m)
			}
		}
	}
	return out
}

// ls and info show a harness's own labels, and ls --label filters on them.
func installLabelledHarnesses(t *testing.T) {
	t.Helper()
	home := labelHome(t, "")
	installListTestHarness(t, home, "ynw-hynde", `{"name":"ynw-hynde","version":"1","default_vendor":"claude","labels":{"wheelhouse":"ynw","role":"worker"}}`)
	installListTestHarness(t, home, "ynw-eno", `{"name":"ynw-eno","version":"1","default_vendor":"claude","labels":{"wheelhouse":"ynw","role":"crew"}}`)
	installListTestHarness(t, home, "other", `{"name":"other","version":"1","default_vendor":"claude","labels":{"wheelhouse":"other"}}`)
	installListTestHarness(t, home, "plain", `{"name":"plain","version":"1","default_vendor":"claude"}`)
}

func listIDs(t *testing.T, args ...string) (ids []string, raw string) {
	t.Helper()
	var stdout bytes.Buffer
	if err := cmdListTo(append(args, "--format", "json"), &stdout, io.Discard); err != nil {
		t.Fatalf("ls %v: %v", args, err)
	}
	var env struct {
		Harnesses []struct {
			ID     string
			Labels map[string]string
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	for _, h := range env.Harnesses {
		ids = append(ids, h.ID)
	}
	return ids, stdout.String()
}

func TestCmdList_LabelFilter(t *testing.T) {
	installLabelledHarnesses(t)
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"no filter", nil, "local/other local/plain local/ynw-eno local/ynw-hynde"},
		{"one label", []string{"--label", "wheelhouse:ynw"}, "local/ynw-eno local/ynw-hynde"},
		{"every label must match", []string{"--label", "wheelhouse:ynw", "--label", "role:worker"}, "local/ynw-hynde"},
		{"no match", []string{"--label", "wheelhouse:ynw", "--label", "role:release"}, ""},
		{"same key, two values", []string{"--label", "wheelhouse:ynw", "--label", "wheelhouse:other"}, ""},
		{"unlabelled never matches", []string{"--label", "role:crew"}, "local/ynw-eno"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ids, _ := listIDs(t, tt.args...)
			if got := strings.Join(ids, " "); got != tt.want {
				t.Errorf("ids = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCmdList_LabelsInJSONAndText(t *testing.T) {
	installLabelledHarnesses(t)
	_, raw := listIDs(t, "--label", "role:worker")
	if !strings.Contains(raw, `"wheelhouse": "ynw"`) || !strings.Contains(raw, `"role": "worker"`) {
		t.Errorf("json lacks the labels:\n%s", raw)
	}
	_, plain := listIDs(t, "--label", "wheelhouse:other")
	if !strings.Contains(plain, `"wheelhouse": "other"`) {
		t.Errorf("json:\n%s", plain)
	}

	var text bytes.Buffer
	if err := cmdListTo([]string{"--label", "wheelhouse:ynw"}, &text, io.Discard); err != nil {
		t.Fatal(err)
	}
	out := text.String()
	if !strings.Contains(out, "LABELS") || !strings.Contains(out, "role:worker,wheelhouse:ynw") {
		t.Errorf("text lacks the labels column:\n%s", out)
	}
	if strings.Contains(out, "other") || strings.Contains(out, "plain") {
		t.Errorf("text shows harnesses the filter excludes:\n%s", out)
	}

	var none bytes.Buffer
	if err := cmdListTo([]string{"--label", "wheelhouse:nobody"}, &none, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(none.String(), "No harnesses carry every label given.") {
		t.Errorf("text for no match:\n%s", none.String())
	}
}

func TestCmdList_LabelFlagErrors(t *testing.T) {
	installLabelledHarnesses(t)
	for _, args := range [][]string{{"--label"}, {"--label", "wheelhouse"}, {"--label", "wheelhouse:"}, {"--label", "Bad:v"}} {
		if err := cmdListTo(args, io.Discard, io.Discard); err == nil {
			t.Errorf("ls %v was accepted", args)
		}
	}
}

func TestCmdInfo_ShowsLabels(t *testing.T) {
	installLabelledHarnesses(t)
	var text bytes.Buffer
	if err := cmdInfoTo([]string{"local/ynw-hynde"}, &text, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "Labels:       role:worker,wheelhouse:ynw") {
		t.Errorf("info text:\n%s", text.String())
	}
	var js bytes.Buffer
	if err := cmdInfoTo([]string{"local/ynw-hynde", "--format", "json"}, &js, io.Discard); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Harness struct{ Labels map[string]string }
	}
	if err := json.Unmarshal(js.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Harness.Labels["wheelhouse"] != "ynw" || env.Harness.Labels["role"] != "worker" {
		t.Errorf("info json labels = %v", env.Harness.Labels)
	}

	var plain bytes.Buffer
	if err := cmdInfoTo([]string{"local/plain"}, &plain, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "Labels:") {
		t.Errorf("a harness with no labels shows a Labels line:\n%s", plain.String())
	}
}
