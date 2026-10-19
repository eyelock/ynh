package agent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/eyelock/ynh/internal/baseline"
	"github.com/eyelock/ynh/internal/gate"
)

// SensorHash produces a stable short hash over a turn's gate result, used by
// the watchdog to detect when sensors stop changing between turns.
//
// The hash covers each sensor's output, not just its status. Hashing name and
// exit code alone meant that fixing three of fifty lint findings produced an
// identical hash — real progress read as no progress, and the watchdog killed
// the run at the no-progress threshold for doing exactly what it was asked to
// do.
//
// Where a baseline is in play the new-failure set is hashed rather than the
// whole output, so the watchdog tracks the same surface the gate blocks on:
// churn among findings that were already forgiven is not progress either.
//
// Output is fingerprinted rather than hashed raw so that file positions
// shifting (a line inserted above an existing finding) does not read as
// progress, and nor does a test failing identically with a different timing
// (#434). The same normalisation the baseline uses.
// matchers maps sensor name to its compiled output.match, nil where none is
// declared. Passed in because the envelope carries results, not declarations,
// and a hash over a tool's decoration would read a changed summary line as
// progress (#338).
func SensorHash(env *gate.Envelope, matchers map[string]*regexp.Regexp) string {
	if env == nil {
		return contentHash("")
	}
	var sb strings.Builder
	for _, r := range env.Sensors {
		if !r.Ran() {
			continue
		}
		fmt.Fprintf(&sb, "%s:%s:", r.Name, r.Status)
		out := r.NewOutput
		if out == "" {
			out = r.Stdout + "\n" + r.Stderr
		}
		for _, fp := range baseline.Fingerprints(out, "", matchers[r.Name]) {
			sb.WriteString(fp)
			sb.WriteByte(',')
		}
		sb.WriteByte('|')
	}
	return contentHash(sb.String())
}
