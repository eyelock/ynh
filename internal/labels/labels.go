// Package labels is ynh's run labels: short key:value ids the user chooses
// for a run, such as wheelhouse:ynw. They belong to the run, not to a vendor,
// and are exported as the resource attributes ynh.label.<key> (ynr ADR-006,
// rule 10). A label that breaks the rule's limits is refused at the source:
// ynh never exports a sanitised version.
//
// A run's labels are merged from four sources, per key, later winning:
// config.json, the root harness's manifest, YNH_LABELS, then --label.
package labels

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Env is the variable that carries a run's labels, comma-separated key:value,
// in and out: an operator may set it, and ynh sets it for every process it
// starts, to the merged set.
const Env = "YNH_LABELS"

// AttrPrefix is the resource attribute prefix a label is exported under,
// declared in telemetry/registry/attributes.yaml as a template attribute.
const AttrPrefix = "ynh.label."

// Max is the most labels one run may carry (ADR-006 rule 10).
const Max = 16

var (
	keyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,31}$`)
	valuePattern = regexp.MustCompile(`^[A-Za-z0-9._/#-]{1,64}$`)
)

// Set is a run's labels, key to value.
type Set map[string]string

// Op is one --label argument: set Key to Value, or, with Remove, drop Key.
type Op struct {
	Key, Value string
	Remove     bool
}

// ParseOp parses one --label value: "key:value", or "key:" to remove key.
func ParseOp(s string) (Op, error) {
	k, v, ok := strings.Cut(s, ":")
	if !ok || k == "" {
		return Op{}, fmt.Errorf("label %q is not key:value (key: removes a label)", s)
	}
	return Op{Key: k, Value: v, Remove: v == ""}, nil
}

// ParseList parses a comma-separated list of key:value, as YNH_LABELS holds
// it. Empty entries are skipped; the removal form is not valid here.
func ParseList(s string) (Set, error) {
	out := Set{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		op, err := ParseOp(part)
		if err != nil {
			return nil, err
		}
		if op.Remove {
			return nil, fmt.Errorf("label %q has no value", part)
		}
		out[op.Key] = op.Value
	}
	return out, nil
}

// Merge returns base overlaid with each later layer, per key.
func Merge(layers ...Set) Set {
	out := Set{}
	for _, l := range layers {
		for k, v := range l {
			out[k] = v
		}
	}
	return out
}

// Apply returns s with ops applied in order: a key set, or removed.
func (s Set) Apply(ops []Op) Set {
	out := Merge(s)
	for _, op := range ops {
		if op.Remove {
			delete(out, op.Key)
		} else {
			out[op.Key] = op.Value
		}
	}
	return out
}

// ValidateKey reports why key is not a label key, or nil.
func ValidateKey(key string) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("label key %q must match %s", key, keyPattern)
	}
	return nil
}

// Validate reports the first reason s is not a valid label set, or nil. home
// is the user's home folder; a value that contains it is refused, as is any
// value that starts with / or ~. It never echoes a refused value that might
// be a path or a secret: it names the key.
func Validate(s Set, home string) error {
	if len(s) > Max {
		return fmt.Errorf("a run may carry at most %d labels, got %d", Max, len(s))
	}
	for _, k := range sortedKeys(s) {
		if err := ValidateKey(k); err != nil {
			return err
		}
		v := s[k]
		switch {
		case strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~"):
			return fmt.Errorf("label %q: a value must not start with / or ~ (it looks like a path)", k)
		case home != "" && home != "/" && strings.Contains(v, home):
			return fmt.Errorf("label %q: a value must not contain the home folder", k)
		case !valuePattern.MatchString(v):
			return fmt.Errorf("label %q: a value must match %s (no spaces, no @)", k, valuePattern)
		}
	}
	return nil
}

// Encode is s as YNH_LABELS holds it: key:value, comma-separated, sorted by
// key. It is empty for an empty set.
func Encode(s Set) string {
	parts := make([]string, 0, len(s))
	for _, k := range sortedKeys(s) {
		parts = append(parts, k+":"+s[k])
	}
	return strings.Join(parts, ",")
}

// EnvEntry is s as an environment entry for a child process, or "" when s is
// empty (a child then has no YNH_LABELS from ynh).
func EnvEntry(s Set) string {
	if len(s) == 0 {
		return ""
	}
	return Env + "=" + Encode(s)
}

// Sources are the places a run's labels come from, lowest first.
type Sources struct {
	Config   Set
	Manifest Set // the root harness's own; included harnesses' are ignored
	Env      string
	Flags    []string
}

// Resolve merges the sources, validates the result and returns it. The error
// is fit to show the user as the reason the run is refused.
func Resolve(src Sources, home string) (Set, error) {
	if err := validatePairs("config.json", src.Config); err != nil {
		return nil, err
	}
	if err := validatePairs("the harness manifest", src.Manifest); err != nil {
		return nil, err
	}
	fromEnv, err := ParseList(src.Env)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", Env, err)
	}
	var ops []Op
	for _, f := range src.Flags {
		op, err := ParseOp(f)
		if err != nil {
			return nil, fmt.Errorf("--label: %w", err)
		}
		if err := ValidateKey(op.Key); err != nil {
			return nil, fmt.Errorf("--label: %w", err)
		}
		ops = append(ops, op)
	}
	merged := Merge(src.Config, src.Manifest, fromEnv).Apply(ops)
	if err := Validate(merged, home); err != nil {
		return nil, err
	}
	return merged, nil
}

// validatePairs checks the keys of one source, so a bad key there is named
// for that source even when a later layer removes it.
func validatePairs(source string, s Set) error {
	for _, k := range sortedKeys(s) {
		if err := ValidateKey(k); err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
	}
	return nil
}

// Home is the user's home folder, "" when unknown.
func Home() string {
	h, _ := os.UserHomeDir()
	return h
}

// Matches reports whether s holds every key:value in want.
func (s Set) Matches(want Set) bool {
	for k, v := range want {
		if got, ok := s[k]; !ok || got != v {
			return false
		}
	}
	return true
}

func sortedKeys(s Set) []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
