package resolver

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// Sensors travel through includes, so a set can be published as an ordinary
// harness and adopted the way any other harness is: installed, pinned to a
// ref, updated.
//
// Before this they did not travel at all. `ynd compose` emitted no sensors,
// and a local include could not even reach outside the harness root, so the
// only way to share one was to copy the JSON out of a document. That is why
// the shipped starter sets were prose, and why they drifted (#366): prose
// cannot be versioned, pinned or validated as a unit.
//
// What a consumer may change is deliberately narrow. See MergeIncludedSensors.

// IncludedSensor is a sensor obtained from an include, with the source it came
// from so collisions and errors can name it.
type IncludedSensor struct {
	Name   string
	Sensor plugin.Sensor
	Source string
	// Dir is the directory the manifest was read from.
	Dir string
}

// WithIncludedSensors folds in the sensors an include declares, so a sensor
// set can be published as an ordinary harness and adopted like any other.
//
// Every command that lists, runs or verifies against sensors goes through this
// (`ynh check`, `ynh sensors`, the agent loop's convergence verifier), so they
// all see the same set. It is not done in harness.LoadDir because LoadDir has
// no business reaching for includes: `ynh ls` should not pay for it.
func WithIncludedSensors(h *harness.Harness) (*harness.Harness, error) {
	if len(h.Includes) == 0 && len(h.SensorOverrides) == 0 && len(h.ProfileSensorOverrides) == 0 {
		return h, nil
	}
	merged, err := MergeIncludedSensors(h, h.SensorOverrides)
	if err != nil {
		return nil, err
	}
	// Last, so the order is declaration, then the harness's include-level
	// overrides, then the active profile's.
	if err := applyProfileSensorOverrides(merged, h.ActiveProfile, h.ProfileSensorOverrides); err != nil {
		return nil, err
	}
	h.Sensors = merged
	return h, nil
}

// MergeIncludedSensors returns the harness's own sensors plus those declared by
// its includes, with overrides applied.
//
// Git includes resolve from the cache only. A gate that reaches the network to
// discover what it is gating on is a gate that fails when GitHub is slow, and
// the sensors it could not fetch would silently not run — the worst available
// outcome. A cache miss is therefore an error naming the fix, not a fetch.
func MergeIncludedSensors(p *harness.Harness, overrides map[string]plugin.SensorOverride) (map[string]plugin.Sensor, error) {
	merged := map[string]plugin.Sensor{}
	origin := map[string]string{}
	for name, s := range p.Sensors {
		merged[name] = s
		origin[name] = "this harness"
	}

	incoming, err := collectIncludedSensors(p)
	if err != nil {
		return nil, err
	}
	// Deterministic order so a collision reports the same pair every run.
	sort.Slice(incoming, func(i, j int) bool {
		if incoming[i].Name != incoming[j].Name {
			return incoming[i].Name < incoming[j].Name
		}
		return incoming[i].Source < incoming[j].Source
	})

	for _, in := range incoming {
		if _, taken := merged[in.Name]; taken {
			// Refusing is the only honest option. Silently preferring one
			// would mean a gate observing something other than what the
			// manifest appears to say, and there is no rule ("nearest wins",
			// "first wins") a reader could apply without knowing the
			// resolution order.
			return nil, fmt.Errorf(
				"sensor %q is declared by both %s and %s: rename one, or drop the include",
				in.Name, origin[in.Name], in.Source)
		}
		// The sensor resolves its command and fixture against the harness
		// that published it, not the consumer.
		in.Sensor.HarnessDir = in.Dir
		in.Sensor.Origin = in.Source
		merged[in.Name] = in.Sensor
		origin[in.Name] = in.Source
	}

	for name, ov := range overrides {
		s, ok := merged[name]
		if !ok {
			// A stale override is the failure mode a silent no-op hides:
			// the sensor was renamed upstream, the softening stopped
			// applying, and the gate started blocking for reasons nobody
			// changed. Same rule --sensor-overlay applies.
			return nil, fmt.Errorf(
				"override names sensor %q, which no include or this harness declares", name)
		}
		if origin[name] == "this harness" {
			return nil, fmt.Errorf(
				"sensor %q is declared by this harness, so edit it directly rather than overriding it", name)
		}
		if ov.Tolerance != "" {
			if !plugin.ValidSensorTolerances[ov.Tolerance] {
				return nil, fmt.Errorf("override for %q: tolerance %q must be one of blocking, advisory, report", name, ov.Tolerance)
			}
			if ov.Tolerance != s.EffectiveTolerance() {
				s.ToleranceSource = "include"
			}
			s.Tolerance = ov.Tolerance
		}
		if ov.Ratchet != "" {
			s.Ratchet = ov.Ratchet
		}
		merged[name] = s
	}

	return merged, nil
}

// applyProfileSensorOverrides applies the active profile's sensor_overrides to
// the merged set, the harness's own sensors and included ones alike. Unlike an
// include-level override, which refuses the harness's own sensors because they
// can be edited directly, a profile is a context and the declaration must stay
// as written.
//
// Three refusals, each naming the profile and the sensor:
//   - a sensor nothing declares. Same reasoning as the include-level rule: a
//     renamed sensor would stop being softened and the gate would start
//     blocking for reasons nobody changed;
//   - a ratchet, which is the baseline's business and the repository's;
//   - any override of a convergence verifier. A verifier decides when a run is
//     done, so no context may change how seriously its verdict is taken.
//     Refusing "blocking" too, which would soften nothing, is deliberate: an
//     entry that cannot do anything is noise a reader must reason about, and
//     there is one rule to state rather than two.
//
// The map is walked in sorted order so the same manifest reports the same
// refusal every run.
func applyProfileSensorOverrides(merged map[string]plugin.Sensor, profile string, overrides map[string]plugin.SensorOverride) error {
	names := make([]string, 0, len(overrides))
	for n := range overrides {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		ov := overrides[name]
		s, ok := merged[name]
		if !ok {
			return fmt.Errorf("profile %q: sensor_overrides names sensor %q, which neither this harness nor any include declares", profile, name)
		}
		if s.Role == "convergence-verifier" {
			return fmt.Errorf("profile %q: sensor_overrides names %q, a convergence verifier: it decides when a run is done, so a profile may not change its tolerance", profile, name)
		}
		if ov.Ratchet != "" {
			return fmt.Errorf("profile %q: sensor_overrides for %q: %s", profile, name, plugin.ProfileRatchetRefusal)
		}
		if ov.Tolerance == "" {
			return fmt.Errorf("profile %q: sensor_overrides for %q sets no tolerance", profile, name)
		}
		if !plugin.ValidSensorTolerances[ov.Tolerance] {
			return fmt.Errorf("profile %q: sensor_overrides for %q: tolerance %q must be one of blocking, advisory, report", profile, name, ov.Tolerance)
		}
		if ov.Tolerance != s.EffectiveTolerance() {
			s.ToleranceSource = "profile"
		}
		s.Tolerance = ov.Tolerance
		merged[name] = s
	}
	return nil
}

// collectIncludedSensors reads the sensors declared by each include.
func collectIncludedSensors(p *harness.Harness) ([]IncludedSensor, error) {
	var out []IncludedSensor
	for _, inc := range p.Includes {
		// Checked before the include is resolved: an include whose sensors
		// are ignored anyway must not fail a gate for not being cached.
		if !inc.SensorsEnabled() {
			continue
		}
		base, source, err := includeBase(p, inc)
		if err != nil {
			return nil, err
		}
		if base == "" {
			continue
		}
		// includeBase has already applied inc.Path.
		dir := base
		hj, err := plugin.LoadPluginJSON(dir)
		if err != nil {
			// An include with no manifest contributes artifacts by directory
			// layout and simply has no sensors. That is legitimate, so it is
			// not an error.
			continue
		}
		for name, s := range hj.Sensors {
			out = append(out, IncludedSensor{Name: name, Sensor: s, Source: source, Dir: dir})
		}
	}
	return out, nil
}

// errNotCached is what lookupOnlyRepo reports for a cache miss.
var errNotCached = errors.New("not in the include cache")

// lookupOnlyRepo is a repoFunc that never reaches the network: a cache miss is
// an error. (CacheOnlyRepo, by contrast, falls back to a fetch on a miss.)
func lookupOnlyRepo(gitURL, ref string) (RepoResult, error) {
	res, ok := LookupCache(gitURL, ref)
	if !ok {
		return RepoResult{}, errNotCached
	}
	return res, nil
}

// includeBase resolves one include to a directory on disk without touching the
// network. A run fetches its includes before it merges sensors, so by then
// the cache is warm; `ynh check` and `ynh sensors` never fetch from a remote.
// The one exception is the harness's own repo (IsOwnRepoSource): it is on
// disk, so it is refreshed here too, and a sensor committed to it is seen
// without an update.
func includeBase(p *harness.Harness, inc harness.Include) (base, source string, err error) {
	if inc.IsLocal() {
		b, err := resolveLocalSource(inc.GitSource, p.Dir)
		if err != nil {
			return "", "", err
		}
		return b, "include " + inc.Local, nil
	}
	fetch := repoFunc(lookupOnlyRepo)
	if IsOwnRepoSource(inc.GitSource, p.Dir) {
		fetch = EnsureRepo
	}
	b, _, err := resolveGitSourceWith(inc.GitSource, p.Dir, fetch)
	if err != nil {
		return "", "", fmt.Errorf(
			"include %s is not in the cache, so its sensors cannot be read: %w\nrun `ynh update` (checking a gate must not reach the network)",
			inc.Git, err)
	}
	return b, "include " + inc.Git, nil
}

// ResolveIncludesLocal resolves the includes of p, and of every harness they
// reach, without touching the network: a git include that is not cached is an
// error, as it is for the sensors. The harness's own repo is the exception
// (IsOwnRepoSource), refreshed as a run refreshes it. sel applies the
// profiles of included harnesses a run could select (--profile ns:name).
// `ynh trust` reads what a run would carry through it.
func ResolveIncludesLocal(p *harness.Harness, sel harness.Selection) ([]ResolveResult, error) {
	results, _, err := resolveSelected(p, nil, func(gs harness.GitSource, dir string) (string, *RepoResult, error) {
		fetch := repoFunc(lookupOnlyRepo)
		if IsOwnRepoSource(gs, dir) {
			fetch = EnsureRepo
		}
		return resolveGitSourceWith(gs, dir, fetch)
	}, sel)
	return results, err
}

// ReadIncludeSensors reports the sensors one include declares, for static
// validation. Unlike the merge it never fetches, not even the harness's own
// repo: a local include is read from disk and a git include only from the
// cache, so a cache miss is an error the caller reports as "not checked".
//
// An include with no manifest declares none, and that is not an error, as at
// run time. A manifest that exists but cannot be read is, because an author
// validating needs to hear it rather than be told the include has no sensors.
func ReadIncludeSensors(harnessDir string, inc harness.Include) (map[string]plugin.Sensor, error) {
	var dir string
	if inc.IsLocal() {
		b, err := resolveLocalSource(inc.GitSource, harnessDir)
		if err != nil {
			return nil, err
		}
		dir = b
	} else {
		b, _, err := resolveGitSourceWith(inc.GitSource, harnessDir, lookupOnlyRepo)
		if err != nil {
			return nil, err
		}
		dir = b
	}
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return hj.Sensors, nil
}
