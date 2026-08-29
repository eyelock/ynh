package pathutil

import "testing"

// TestTraversalRejected pins the guarantee that the schemas previously
// asserted with an unenforceable regex.
//
// plugin.schema.json and marketplace.schema.json carried
// `^(?!\.\./|\.\.($))[^/]` on every `path` field. That pattern cannot compile
// under Go's RE2, so no ynh validator ever applied it — the real enforcement
// has always been this function, called from the resolver (include and
// delegate paths, local includes) and the marketplace loader. The regex was
// removed; this test is what keeps the property.
func TestTraversalRejected(t *testing.T) {
	rejected := []string{
		"..",
		"../",
		"../etc",
		"../../etc/passwd",
		"a/../..",
		"a/../../b",
		"/etc/passwd",
		"/",
	}
	for _, p := range rejected {
		t.Run("reject/"+p, func(t *testing.T) {
			if err := CheckSubpath(p); err == nil {
				t.Errorf("CheckSubpath(%q) = nil, want error — this escapes the source root", p)
			}
		})
	}

	// Paths that stay inside the root must keep working. A guard that
	// rejects legitimate subdirectories is its own kind of bug: dotted and
	// hidden directory names are ordinary in harness sources.
	accepted := []string{
		"skills",
		"skills/dev",
		"a/../b",
		".claude",
		".github/plugin",
		"./skills",
		"my.dir/sub",
	}
	for _, p := range accepted {
		t.Run("accept/"+p, func(t *testing.T) {
			if err := CheckSubpath(p); err != nil {
				t.Errorf("CheckSubpath(%q) = %v, want nil", p, err)
			}
		})
	}
}
