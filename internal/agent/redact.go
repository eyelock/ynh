package agent

import (
	"encoding/json"
	"sort"
	"strings"
)

// minRedactableLen is the shortest secret worth substituting.
//
// A two-character value appears inside ordinary words and paths, and replacing
// every occurrence would corrupt the trajectory while protecting nothing worth
// protecting. A credential shorter than this is not meaningfully secret.
const minRedactableLen = 8

// secretNameHints mark a variable whose value must never reach the trajectory.
//
// Matched case-insensitively as substrings, so ANTHROPIC_API_KEY,
// github_token and CLAUDE_CODE_GH_PAT all match.
var secretNameHints = []string{
	"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL",
	"APIKEY", "API_KEY", "_KEY", "PRIVATE", "_PAT", "SESSION_KEY", "AUTH",
}

// Redactor replaces known secret values wherever they appear in text.
//
// It redacts by *value*, not by pattern. Pattern-matching for secrets is a
// guessing game that misses bespoke formats and mangles innocent text; but the
// values a run was given are known exactly, so every occurrence of one can be
// substituted with certainty — including where a sensor echoed it into its
// output, which is the realistic leak. A curl that fails and prints its own
// Authorization header ends up in a trajectory otherwise.
//
// It is deliberately not a general secret scanner. A credential this process
// was never given — one the agent generated, or read from a file — is not
// covered, and the docs say so rather than implying a guarantee.
type Redactor struct {
	// values are sorted longest-first so an overlapping pair redacts the
	// larger match rather than leaving its tail behind.
	values []redactedValue
}

type redactedValue struct {
	value string
	label string
}

// NewRedactor builds a redactor from KEY=VALUE entries, selecting those whose
// name looks like a credential.
func NewRedactor(env []string) *Redactor {
	r := &Redactor{}
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || len(value) < minRedactableLen || !IsSecretName(name) {
			continue
		}
		r.values = append(r.values, redactedValue{value: value, label: name})
		// The trajectory is redacted after encoding, so a value containing a
		// quote or backslash appears there in escaped form and would not match
		// the raw one. Cheap to cover; a real hole if left.
		if esc, err := json.Marshal(value); err == nil {
			if inner := string(esc[1 : len(esc)-1]); inner != value {
				r.values = append(r.values, redactedValue{value: inner, label: name})
			}
		}
	}
	sort.Slice(r.values, func(i, j int) bool {
		return len(r.values[i].value) > len(r.values[j].value)
	})
	return r
}

// IsSecretName reports whether a variable name suggests its value is a credential.
func IsSecretName(name string) bool {
	up := strings.ToUpper(name)
	for _, hint := range secretNameHints {
		if strings.Contains(up, hint) {
			return true
		}
	}
	return false
}

// Redact replaces every known secret value with a labelled placeholder.
//
// The label names the variable rather than blanking the text, so a reader can
// tell "the run's GitHub token appeared here" from "some unknown string was
// removed" — the first is a finding about the harness, the second is noise.
func (r *Redactor) Redact(s string) string {
	if r == nil || len(r.values) == 0 || s == "" {
		return s
	}
	for _, v := range r.values {
		if strings.Contains(s, v.value) {
			s = strings.ReplaceAll(s, v.value, "[redacted:"+v.label+"]")
		}
	}
	return s
}

// Len reports how many distinct values this redactor will substitute.
func (r *Redactor) Len() int {
	if r == nil {
		return 0
	}
	return len(r.values)
}
