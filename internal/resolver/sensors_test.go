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

// WithIncludedSensors is the one fold every sensor command shares; a harness
// with no include is returned as it was.
func TestWithIncludedSensors(t *testing.T) {
	h := consumerWith(t, nil, map[string]any{"go-vet": commandSensor("blocking")})
	got, err := WithIncludedSensors(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Sensors["go-vet"]; !ok {
		t.Errorf("included sensor missing: %v", got.Sensors)
	}

	plain := &harness.Harness{Name: "plain", Dir: t.TempDir()}
	if got, err := WithIncludedSensors(plain); err != nil || got != plain || got.Sensors != nil {
		t.Errorf("a harness without includes must pass through untouched: %v %v", got, err)
	}
}

// Sensor merging never reaches the network (`ynh check` must not): a git
// include missing from the cache is an error naming `ynh update`, and nothing
// is cloned. A run fetches its includes before it merges (ynf #130), after
// which the same merge reads the warm cache even with the remote gone.
func TestWithIncludedSensors_GitIncludeIsCacheOnly(t *testing.T) {
	src := t.TempDir()
	writeHarness(t, src, "up", map[string]any{"go-vet": commandSensor("blocking")})
	runGit(t, src, "init")
	runGit(t, src, "config", "user.email", "test@test.com")
	runGit(t, src, "config", "user.name", "Test")
	runGit(t, src, "add", ".")
	runGit(t, src, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())
	newConsumer := func() *harness.Harness {
		return &harness.Harness{
			Name: "consumer",
			Dir:  t.TempDir(),
			Includes: []harness.Include{
				{GitSource: harness.GitSource{Git: src}},
			},
		}
	}

	_, err := WithIncludedSensors(newConsumer())
	if err == nil || !strings.Contains(err.Error(), "ynh update") {
		t.Fatalf("a cold cache must refuse and name `ynh update`, got %v", err)
	}
	if _, ok := LookupCache(src, ""); ok {
		t.Fatal("merging sensors must not populate the cache")
	}

	if _, err := ResolveFromCache(newConsumer(), nil); err != nil {
		t.Fatalf("fetching the include: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(src, ".git")); err != nil {
		t.Fatal(err)
	}
	got, err := WithIncludedSensors(newConsumer())
	if err != nil {
		t.Fatalf("a warm cache must merge: %v", err)
	}
	if _, ok := got.Sensors["go-vet"]; !ok {
		t.Errorf("included sensor missing: %v", got.Sensors)
	}
}

func TestIncludedSensorRecordsItsOwnHarnessDir(t *testing.T) {
	own := map[string]plugin.Sensor{"mine": {Tolerance: "blocking"}}
	h := consumerWith(t, own, map[string]any{"go-vet": commandSensor("blocking")})
	merged, err := MergeIncludedSensors(h, nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	want := filepath.Join(h.Dir, "vendored", "up")
	if got := merged["go-vet"].HarnessDir; got != want {
		t.Errorf("included HarnessDir = %q, want %q", got, want)
	}
	if got := merged["go-vet"].Dir(h.Dir); got != want {
		t.Errorf("included Dir = %q, want %q", got, want)
	}
	if got := merged["mine"].HarnessDir; got != "" {
		t.Errorf("own sensor HarnessDir = %q, want empty", got)
	}
	if got := merged["mine"].Dir(h.Dir); got != h.Dir {
		t.Errorf("own sensor Dir = %q, want the fallback %q", got, h.Dir)
	}
}

func TestIncludedSensorDirHonoursIncludePath(t *testing.T) {
	root := t.TempDir()
	writeHarness(t, filepath.Join(root, "vendored", "mono", "sub"), "sub", map[string]any{"s": commandSensor("blocking")})
	h := &harness.Harness{
		Name: "consumer",
		Dir:  root,
		Includes: []harness.Include{{GitSource: harness.GitSource{
			Local: "vendored/mono", Path: "sub",
		}}},
	}
	merged, err := MergeIncludedSensors(h, nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	want := filepath.Join(root, "vendored", "mono", "sub")
	if got := merged["s"].HarnessDir; got != want {
		t.Errorf("HarnessDir = %q, want %q", got, want)
	}
}

func TestIncludeWithSensorsFalseContributesNone(t *testing.T) {
	f := false
	tr := true
	tests := []struct {
		name    string
		sensors *bool
		want    bool
	}{
		{"absent travels", nil, true},
		{"true travels", &tr, true},
		{"false is dropped", &f, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := consumerWith(t, nil, map[string]any{"go-vet": commandSensor("blocking")})
			h.Includes[0].Sensors = tt.sensors
			merged, err := MergeIncludedSensors(h, nil)
			if err != nil {
				t.Fatalf("merge: %v", err)
			}
			if _, ok := merged["go-vet"]; ok != tt.want {
				t.Errorf("sensor present = %v, want %v", ok, tt.want)
			}
			if len(h.Includes) != 1 {
				t.Errorf("the include itself must remain")
			}
		})
	}
}

func TestOverrideOfAnOptedOutSensorIsRefused(t *testing.T) {
	f := false
	h := consumerWith(t, nil, map[string]any{"go-vet": commandSensor("blocking")})
	h.Includes[0].Sensors = &f
	_, err := MergeIncludedSensors(h, map[string]plugin.SensorOverride{"go-vet": {Tolerance: "advisory"}})
	if err == nil || !strings.Contains(err.Error(), "no include or this harness declares") {
		t.Errorf("expected the unknown-sensor refusal, got: %v", err)
	}
}

// A gate must not fail for an include it would ignore anyway.
func TestOptedOutGitIncludeNeedsNoCache(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	f := false
	h := &harness.Harness{
		Name: "consumer",
		Dir:  t.TempDir(),
		Includes: []harness.Include{{
			GitSource: harness.GitSource{Git: "github.com/example/not-cached"},
			Sensors:   &f,
		}},
	}
	if _, err := MergeIncludedSensors(h, nil); err != nil {
		t.Errorf("an opted-out include must not need the cache: %v", err)
	}
	h.Includes[0].Sensors = nil
	if _, err := MergeIncludedSensors(h, nil); err == nil {
		t.Error("control: a sensor-bearing uncached include should still error")
	}
}

// profiled returns a consumer with own sensors "mine" and "gate-v" (a
// convergence verifier), one included sensor "go-vet", and the given overrides
// applied as the active profile "factory".
func profiled(t *testing.T, include, profile map[string]plugin.SensorOverride) *harness.Harness {
	t.Helper()
	own := map[string]plugin.Sensor{
		"mine":   {Tolerance: "blocking"},
		"gate-v": {Role: "convergence-verifier"},
	}
	h := consumerWith(t, own, map[string]any{"go-vet": commandSensor("blocking")})
	h.SensorOverrides = include
	h.ActiveProfile = "factory"
	h.ProfileSensorOverrides = profile
	return h
}

func TestProfileOverridesApplyToOwnAndIncludedSensors(t *testing.T) {
	h := profiled(t, nil, map[string]plugin.SensorOverride{
		"mine":   {Tolerance: "advisory"},
		"go-vet": {Tolerance: "report"},
	})
	got, err := WithIncludedSensors(h)
	if err != nil {
		t.Fatalf("WithIncludedSensors: %v", err)
	}
	for name, want := range map[string]string{"mine": "advisory", "go-vet": "report"} {
		s := got.Sensors[name]
		if s.Tolerance != want || s.ToleranceSource != "profile" {
			t.Errorf("%s: tolerance %q source %q, want %q from profile", name, s.Tolerance, s.ToleranceSource, want)
		}
	}
}

// Declaration, then the harness's include-level override, then the profile's:
// the later layer wins and is the one named as the source.
func TestProfileOverrideComesLastInTheMergeOrder(t *testing.T) {
	cases := []struct {
		name          string
		include       map[string]plugin.SensorOverride
		profile       map[string]plugin.SensorOverride
		wantTolerance string
		wantSource    string
	}{
		{"declared only", nil, nil, "blocking", ""},
		{"include override only", map[string]plugin.SensorOverride{"go-vet": {Tolerance: "advisory"}}, nil, "advisory", "include"},
		{"profile beats include", map[string]plugin.SensorOverride{"go-vet": {Tolerance: "advisory"}},
			map[string]plugin.SensorOverride{"go-vet": {Tolerance: "report"}}, "report", "profile"},
		{"profile hardens what include softened", map[string]plugin.SensorOverride{"go-vet": {Tolerance: "advisory"}},
			map[string]plugin.SensorOverride{"go-vet": {Tolerance: "blocking"}}, "blocking", "profile"},
		{"profile restating the declaration changes nothing", nil,
			map[string]plugin.SensorOverride{"go-vet": {Tolerance: "blocking"}}, "blocking", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := WithIncludedSensors(profiled(t, c.include, c.profile))
			if err != nil {
				t.Fatalf("WithIncludedSensors: %v", err)
			}
			s := got.Sensors["go-vet"]
			if s.EffectiveTolerance() != c.wantTolerance || s.ToleranceSource != c.wantSource {
				t.Errorf("got %q from %q, want %q from %q", s.EffectiveTolerance(), s.ToleranceSource, c.wantTolerance, c.wantSource)
			}
		})
	}
}

// A sensor committed to the harness's own repo after the first resolution is
// seen without an update, because that repo is on disk and is refreshed. Any
// other repo stays on the lookup-only path (#596).
func TestWithIncludedSensors_OwnRepoIsRefreshed(t *testing.T) {
	tests := []struct {
		name        string
		git         string
		otherRepo   bool
		wantRefresh bool
	}{
		{"own repo", ".", false, true},
		{"sibling repo", "../other", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			parent, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			h := newRepoAt(t, filepath.Join(parent, "h"), "keep")
			other := newRepoAt(t, filepath.Join(parent, "other"), "keep")
			source := h
			if tt.otherRepo {
				source = other
			}
			commit := func(sensors map[string]any) {
				t.Helper()
				writeHarness(t, filepath.Join(source, "up"), "up", sensors)
				runGit(t, source, "add", ".")
				runGit(t, source, "commit", "-m", "sensors")
			}
			consumer := func() *harness.Harness {
				return &harness.Harness{Name: "consumer", Dir: h, Includes: []harness.Include{
					{GitSource: harness.GitSource{Git: tt.git, Path: "up"}},
				}}
			}

			commit(map[string]any{"first": commandSensor("blocking")})
			if _, err := ResolveFromCache(consumer(), nil); err != nil {
				t.Fatalf("first run: %v", err)
			}
			commit(map[string]any{"first": commandSensor("blocking"), "second": commandSensor("blocking")})

			got, err := WithIncludedSensors(consumer())
			if err != nil {
				t.Fatal(err)
			}
			_, sawSecond := got.Sensors["second"]
			if sawSecond != tt.wantRefresh {
				t.Errorf("sensor added after the first run seen = %v, want %v", sawSecond, tt.wantRefresh)
			}
			if _, ok := got.Sensors["first"]; !ok {
				t.Error("first sensor missing")
			}
		})
	}
}

func TestProfileOverrideRefusals(t *testing.T) {
	cases := []struct {
		name    string
		profile map[string]plugin.SensorOverride
		want    []string
	}{
		{"unknown sensor", map[string]plugin.SensorOverride{"ghost": {Tolerance: "advisory"}},
			[]string{`profile "factory"`, `"ghost"`, "declares"}},
		{"ratchet", map[string]plugin.SensorOverride{"mine": {Ratchet: "count"}},
			[]string{`profile "factory"`, `"mine"`, "ratchet", "baseline belongs to the repository"}},
		{"ratchet alongside tolerance", map[string]plugin.SensorOverride{"mine": {Tolerance: "advisory", Ratchet: "count"}},
			[]string{`"mine"`, "ratchet"}},
		{"verifier softened", map[string]plugin.SensorOverride{"gate-v": {Tolerance: "advisory"}},
			[]string{`profile "factory"`, `"gate-v"`, "convergence verifier"}},
		{"verifier restated as blocking", map[string]plugin.SensorOverride{"gate-v": {Tolerance: "blocking"}},
			[]string{`"gate-v"`, "convergence verifier"}},
		{"invalid tolerance", map[string]plugin.SensorOverride{"mine": {Tolerance: "loud"}},
			[]string{`profile "factory"`, `"mine"`, "must be one of"}},
		{"empty override", map[string]plugin.SensorOverride{"mine": {}},
			[]string{`"mine"`, "no tolerance"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := WithIncludedSensors(profiled(t, nil, c.profile))
			if err == nil {
				t.Fatal("want a refusal")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err, w)
				}
			}
		})
	}
}

// An include that opted out with "sensors": false contributes none, so a
// profile naming one of its sensors is stale, as for include-level overrides.
func TestProfileOverrideOfAnOptedOutSensorIsRefused(t *testing.T) {
	h := profiled(t, nil, map[string]plugin.SensorOverride{"go-vet": {Tolerance: "advisory"}})
	off := false
	h.Includes[0].Sensors = &off
	if _, err := WithIncludedSensors(h); err == nil || !strings.Contains(err.Error(), `"go-vet"`) {
		t.Fatalf("want go-vet refused as undeclared, got %v", err)
	}
}

// ResolveProfile hands the profile's overrides to the sensor merge.
func TestResolveProfileCarriesSensorOverrides(t *testing.T) {
	h := &harness.Harness{Profiles: map[string]plugin.Profile{
		"factory": {SensorOverrides: map[string]plugin.SensorOverride{"mine": {Tolerance: "advisory"}}},
	}, Sensors: map[string]plugin.Sensor{"mine": {}}}
	got, err := harness.ResolveProfile(h, "factory")
	if err != nil {
		t.Fatal(err)
	}
	got, err = WithIncludedSensors(got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sensors["mine"].Tolerance != "advisory" {
		t.Errorf("tolerance = %q, want advisory", got.Sensors["mine"].Tolerance)
	}
	if h.Sensors["mine"].Tolerance != "" {
		t.Error("the original harness was mutated")
	}
}

func TestReadIncludeSensors(t *testing.T) {
	root := t.TempDir()
	writeHarness(t, filepath.Join(root, "up"), "up", map[string]any{"go-vet": commandSensor("blocking")})
	if err := os.MkdirAll(filepath.Join(root, "bare"), 0o755); err != nil {
		t.Fatal(err)
	}
	local := func(p string) harness.Include {
		return harness.Include{GitSource: harness.GitSource{Local: p}}
	}

	got, err := ReadIncludeSensors(root, local("up"))
	if err != nil || got["go-vet"].Source.Kind() != "command" {
		t.Fatalf("local include: %v %v", got, err)
	}
	if got, err := ReadIncludeSensors(root, local("bare")); err != nil || len(got) != 0 {
		t.Errorf("an include with no manifest declares none and is not an error: %v %v", got, err)
	}
	if _, err := ReadIncludeSensors(root, local("missing")); err == nil {
		t.Error("a missing local include must be an error")
	}

	// A git include is read from the cache only.
	src := t.TempDir()
	writeHarness(t, src, "up", map[string]any{"lint": commandSensor("blocking")})
	runGit(t, src, "init")
	runGit(t, src, "config", "user.email", "test@test.com")
	runGit(t, src, "config", "user.name", "Test")
	runGit(t, src, "add", ".")
	runGit(t, src, "commit", "-m", "init")
	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())
	inc := harness.Include{GitSource: harness.GitSource{Git: src}}
	if _, err := ReadIncludeSensors(root, inc); err == nil {
		t.Fatal("a cold cache must be an error")
	}
	if _, ok := LookupCache(src, ""); ok {
		t.Fatal("reading must not populate the cache")
	}
	h := &harness.Harness{Name: "c", Dir: t.TempDir(), Includes: []harness.Include{inc}}
	if _, err := ResolveFromCache(h, nil); err != nil {
		t.Fatal(err)
	}
	got, err = ReadIncludeSensors(root, inc)
	if err != nil || got["lint"].Source.Kind() != "command" {
		t.Fatalf("warm cache: %v %v", got, err)
	}
}
