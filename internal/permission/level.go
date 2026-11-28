// Package permission is the one place that knows what a harness's permission
// level means: the words none, edits and all, their order, how a request is
// narrowed, what each vendor's CLI is passed for each level, how a project's
// own permission mode compares, the refusal for Claude Code as root, and the
// check of the mode a session actually opened in.
//
// It serves ynh run, delegates and ynh agent run alike (ynh #1058, D7). It
// holds no policy about who may run a harness (that is trust) and reads no
// manifest (the auto_approve declaration is the manifest's); callers hand it
// words and a working folder.
package permission

import "fmt"

// Level is a permission level for a one-shot run.
type Level string

// The levels, narrowest first. None approves nothing, Edits approves file
// edits and withholds commands, All approves everything the worker asks.
const (
	None  Level = "none"
	Edits Level = "edits"
	All   Level = "all"
)

// Words is the accepted spelling of every level, for error messages.
const Words = "none, edits, all"

// Parse reads a level word. The empty string is not a level: callers decide
// what "no level given" means.
func Parse(s string) (Level, error) {
	switch l := Level(s); l {
	case None, Edits, All:
		return l, nil
	}
	return "", fmt.Errorf("unknown auto-approve level %q (supported: %s)", s, Words)
}

// Valid reports whether l is one of the three levels.
func (l Level) Valid() bool { return l == None || l == Edits || l == All }

// Rank orders the levels: all > edits > none. An invalid level ranks below
// none, so it never wins a comparison.
func (l Level) Rank() int {
	switch l {
	case None:
		return 0
	case Edits:
		return 1
	case All:
		return 2
	}
	return -1
}

// Narrower reports whether l grants strictly less than other.
func (l Level) Narrower(other Level) bool { return l.Rank() < other.Rank() }

// Narrow returns the narrower of two levels: the most a request may ever get
// under a ceiling.
func Narrow(a, b Level) Level {
	if b.Narrower(a) {
		return b
	}
	return a
}

// Request narrows ceiling by a request, and refuses a request wider than the
// ceiling (D2): a wider ask is an error before any session starts, never
// quietly clipped. An empty request leaves the ceiling as it is.
func Request(ceiling, request Level) (Level, error) {
	if request == "" {
		return ceiling, nil
	}
	if !request.Valid() {
		return "", fmt.Errorf("unknown auto-approve level %q (supported: %s)", string(request), Words)
	}
	if ceiling.Narrower(request) {
		return "", fmt.Errorf("auto-approve %s is wider than the ceiling %s; a request may only narrow (all > edits > none)", request, ceiling)
	}
	return request, nil
}
