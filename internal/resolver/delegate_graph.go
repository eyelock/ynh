package resolver

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
)

// FlatDelegate is one harness reachable through delegates_to, loaded once:
// the agent it becomes, however many harnesses declare it.
type FlatDelegate struct {
	// Entry is the delegates_to entry that first reached the harness.
	Entry harness.Delegate
	// BasePath is the directory the harness was loaded from.
	BasePath string
	// Harness is the loaded harness, with the entry's profile or focus
	// profile resolved (Delegate.Select).
	Harness *harness.Harness
	// Focus is the focus the entry names, nil for none.
	Focus *harness.DelegateFocus
	// Declared names, in declaration order, the delegates this harness
	// declares itself, each once. The root and the harness itself are never
	// among them: neither is an agent a delegate can hand to.
	Declared []string
	// Notes remark on edges the flattening skipped: a delegate that names
	// the root, or itself.
	Notes []string

	// sel is the entry's selection, to tell two entries for one harness apart.
	sel string
}

// FlattenDelegates walks delegates_to from the root and returns every
// harness it reaches, once each, in breadth-first order from the root's own
// entries. The root is the harness whose entries these are: rootDir is where
// it lives and rootName is what it is called. It is never one of the results,
// however a delegate reaches it, nor is a harness its own delegate; both are
// noted on the harness that declares the edge. Several routes to one harness
// are one result, but two routes that select it differently (another
// profile or focus), or two different harnesses sharing a name, are errors
// naming both routes, as one agent cannot be both.
//
// resolve turns an entry's source into a directory; a relative source is
// resolved against the directory of the harness that declares it.
func FlattenDelegates(entries []harness.Delegate, rootDir, rootName string, resolve func(gs harness.GitSource, harnessDir string) (string, error)) ([]FlatDelegate, error) {
	type pending struct {
		entry  harness.Delegate
		parent int // index into out, -1 for the root
		dir    string
		route  string
	}
	ids := newIdentities()
	// The root is known the way a delegate is, by source, so a seat that
	// reaches the root through a clone of its repository is recognised as the
	// root. A root with uncommitted work, or at a commit the clone does not
	// hold, differs in source, and is still caught by its name.
	rootID := ids.of(rootDir)

	var out []FlatDelegate
	routes := []string{}
	byID := map[string]int{}
	byName := map[string]int{}
	var queue []pending
	for _, e := range entries {
		queue = append(queue, pending{entry: e, parent: -1, dir: rootDir, route: "the root"})
	}

	declare := func(parent int, name string) {
		if parent < 0 {
			return
		}
		for _, d := range out[parent].Declared {
			if d == name {
				return
			}
		}
		out[parent].Declared = append(out[parent].Declared, name)
	}
	note := func(parent int, s string) {
		if parent >= 0 {
			out[parent].Notes = append(out[parent].Notes, s)
		}
	}

	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		base, err := resolve(p.entry.GitSource, p.dir)
		if err != nil {
			return nil, fmt.Errorf("delegate: %w", err)
		}
		id := ids.of(base)
		loaded, err := harness.LoadDir(base)
		if err != nil {
			return nil, fmt.Errorf("loading delegate harness %s: %w", p.entry.Git, err)
		}

		if id == rootID || (rootName != "" && loaded.Name == rootName) {
			note(p.parent, fmt.Sprintf("delegate %s is the root harness %s, which is no agent of its own; skipped", p.entry.Label(), rootName))
			continue
		}
		if p.parent >= 0 && id == ids.of(routesDir(out, p.parent)) {
			note(p.parent, fmt.Sprintf("harness %s names itself as a delegate; skipped", loaded.Name))
			continue
		}

		selected, focus, err := p.entry.Select(loaded)
		if err != nil {
			return nil, err
		}
		sel := p.entry.Profile + "|" + p.entry.Focus

		if at, seen := byID[id]; seen {
			if out[at].sel != sel {
				return nil, fmt.Errorf("delegate %s is reached with two selections: %s by %s, and %s by %s; select it one way",
					loaded.Name, describeSelection(out[at].sel), routes[at], describeSelection(sel), p.route)
			}
			declare(p.parent, loaded.Name)
			continue
		}
		if at, clash := byName[loaded.Name]; clash {
			return nil, fmt.Errorf("two delegates are both named %s: %s by %s, and %s by %s; an agent carries one name",
				loaded.Name, out[at].BasePath, routes[at], base, p.route)
		}

		idx := len(out)
		byID[id], byName[loaded.Name] = idx, idx
		out = append(out, FlatDelegate{Entry: p.entry, BasePath: base, Harness: selected, Focus: focus, sel: sel})
		routes = append(routes, p.route)
		declare(p.parent, loaded.Name)
		for _, e := range selected.DelegatesTo {
			queue = append(queue, pending{entry: e, parent: idx, dir: base, route: p.route + " > " + loaded.Name})
		}
	}
	return out, nil
}

// identities names harness directories by source (see sourceIdentity),
// remembering each answer: a walk asks about the same directories again for
// every edge, and the repository behind many of them is one, and each answer
// costs git processes on the launch path.
type identities struct {
	byDir map[string]string
	byTop map[string]topSource
}

// topSource is what a clone's top-level directory resolves to: where it came
// from, and the commit it holds.
type topSource struct{ origin, sha string }

func newIdentities() *identities {
	return &identities{byDir: map[string]string{}, byTop: map[string]topSource{}}
}

// of is the identity of a harness directory by what it was resolved from
// rather than where it sits. A directory in a git clone is the clone's
// origin, the commit it holds and its path in the clone; when the origin is
// itself a local repository (a relative git source resolved from inside a
// cached clone is cloned again from that clone), the walk follows it back to
// the source, so two clones of one repository at one commit give one harness
// one identity. A directory in no clone is known by its path (dirIdentity).
func (c *identities) of(dir string) string {
	abs := dirIdentity(dir)
	if id, ok := c.byDir[abs]; ok {
		return id
	}
	id := c.compute(abs)
	c.byDir[abs] = id
	return id
}

func (c *identities) compute(abs string) string {
	top := gitToplevel(abs)
	if top == "" {
		return abs
	}
	rel, err := filepath.Rel(top, abs)
	if err != nil {
		return abs
	}
	src, ok := c.byTop[top]
	if !ok {
		src = topSource{origin: followOrigin(top), sha: gitHead(top)}
		c.byTop[top] = src
	}
	if src.sha == "" {
		return abs
	}
	return src.origin + "@" + src.sha + "//" + filepath.ToSlash(rel)
}

// followOrigin is the origin of the clone at top, following it through local
// clones back to the source; bounded in case of a loop. A repository with no
// origin is its own.
func followOrigin(top string) string {
	origin := top
	for i := 0; i < 16; i++ {
		out, err := exec.Command(gitBin, "-C", origin, "remote", "get-url", "origin").Output()
		if err != nil {
			break
		}
		url := strings.TrimSpace(string(out))
		if !filepath.IsAbs(url) {
			return url
		}
		if gitToplevel(url) == "" {
			return url
		}
		origin = dirIdentity(url)
	}
	return origin
}

func routesDir(out []FlatDelegate, i int) string { return out[i].BasePath }

func describeSelection(sel string) string {
	switch {
	case sel == "|":
		return "no selection"
	case sel[0] == '|':
		return "focus " + sel[1:]
	default:
		return "profile " + sel[:len(sel)-1]
	}
}
