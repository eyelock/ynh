package resolver

import (
	"fmt"
	"path/filepath"
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
			s.Tolerance = ov.Tolerance
		}
		if ov.Ratchet != "" {
			s.Ratchet = ov.Ratchet
		}
		merged[name] = s
	}

	return merged, nil
}

// collectIncludedSensors reads the sensors declared by each include.
func collectIncludedSensors(p *harness.Harness) ([]IncludedSensor, error) {
	var out []IncludedSensor
	for _, inc := range p.Includes {
		base, source, err := includeBase(p, inc)
		if err != nil {
			return nil, err
		}
		if base == "" {
			continue
		}
		dir := base
		if inc.Path != "" {
			dir = filepath.Join(base, inc.Path)
		}
		hj, err := plugin.LoadPluginJSON(dir)
		if err != nil {
			// An include with no manifest contributes artifacts by directory
			// layout and simply has no sensors. That is legitimate, so it is
			// not an error.
			continue
		}
		for name, s := range hj.Sensors {
			out = append(out, IncludedSensor{Name: name, Sensor: s, Source: source})
		}
	}
	return out, nil
}

// includeBase resolves one include to a directory on disk without touching the
// network.
func includeBase(p *harness.Harness, inc harness.Include) (base, source string, err error) {
	if inc.IsLocal() {
		b, err := resolveLocalSource(inc.GitSource, p.Dir)
		if err != nil {
			return "", "", err
		}
		return b, "include " + inc.Local, nil
	}
	b, _, err := ResolveGitSourceFromCache(inc.GitSource)
	if err != nil {
		return "", "", fmt.Errorf(
			"include %s is not in the cache, so its sensors cannot be read: %w\nrun `ynh update` (checking a gate must not reach the network)",
			inc.Git, err)
	}
	return b, "include " + inc.Git, nil
}
