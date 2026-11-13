package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/labels"
)

// listColumn is one column of `ynh ls` text output.
type listColumn struct {
	key    string // the name --columns takes
	header string
	// shrink orders the columns that give way when a row is wider than the
	// terminal: 1 first, then 2, and so on. 0 means the column is never cut.
	shrink int
	// minWidth is the narrowest a shrinking column gets.
	minWidth int
}

// listColumns are the columns of `ynh ls`, in their default order.
var listColumns = []listColumn{
	{key: "id", header: "ID"},
	{key: "kind", header: "KIND"},
	{key: "vendor", header: "VENDOR"},
	{key: "source", header: "SOURCE", shrink: 3, minWidth: 12},
	{key: "artifacts", header: "ARTIFACTS"},
	{key: "includes", header: "INCLUDES", shrink: 1, minWidth: 8},
	{key: "delegates", header: "DELEGATES TO", shrink: 2, minWidth: 8},
	{key: "labels", header: "LABELS", shrink: 4, minWidth: 8},
}

// parseListColumns resolves a --columns value to columns, in the order given.
func parseListColumns(names []string) ([]listColumn, error) {
	var out []listColumn
	seen := map[string]bool{}
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		name = strings.NewReplacer("_", "", "-", "", " ", "").Replace(name)
		if name == "delegatesto" {
			name = "delegates"
		}
		found := false
		for _, c := range listColumns {
			if c.key == name {
				found = true
				if !seen[name] {
					seen[name] = true
					out = append(out, c)
				}
			}
		}
		if !found {
			valid := make([]string, 0, len(listColumns))
			for _, c := range listColumns {
				valid = append(valid, c.key)
			}
			return nil, fmt.Errorf("--columns: unknown column %q (want %s)", raw, strings.Join(valid, ", "))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--columns requires at least one column")
	}
	return out, nil
}

// listCell is one cell: a single string, or a list that can be cut to fit.
type listCell struct {
	text  string
	items []string // non-nil for INCLUDES and DELEGATES TO
}

// listRow is one row, keyed by column key.
type listRow map[string]listCell

func cellLen(s string) int { return utf8.RuneCountInString(s) }

// fitCell cuts a cell to at most budget characters. A list keeps the items
// that fit and ends with "… (+N)", N being how many it hides; a single
// string ends with "…".
func fitCell(c listCell, budget int) string {
	if cellLen(c.text) <= budget {
		return c.text
	}
	if c.items == nil {
		if budget < 1 {
			return ""
		}
		r := []rune(c.text)
		return string(r[:budget-1]) + "…"
	}
	for keep := len(c.items) - 1; keep >= 0; keep-- {
		s := strings.Join(c.items[:keep], ", ")
		if keep > 0 {
			s += ", "
		}
		s += fmt.Sprintf("… (+%d)", len(c.items)-keep)
		if cellLen(s) <= budget {
			return s
		}
	}
	return fmt.Sprintf("… (+%d)", len(c.items))
}

// fitRows renders rows into cells. With width > 0, the columns that may be
// cut are narrowed, in their shrink order, until a row fits width.
func fitRows(cols []listColumn, rows []listRow, width int) [][]string {
	budget := make([]int, len(cols))
	for i := range budget {
		budget[i] = -1 // uncut
	}
	render := func() [][]string {
		out := make([][]string, len(rows))
		for r, row := range rows {
			out[r] = make([]string, len(cols))
			for i, c := range cols {
				cell := row[c.key]
				if budget[i] >= 0 {
					out[r][i] = fitCell(cell, budget[i])
				} else {
					out[r][i] = cell.text
				}
			}
		}
		return out
	}
	colWidth := func(cells [][]string, i int) int {
		w := cellLen(cols[i].header)
		for _, r := range cells {
			if n := cellLen(r[i]); n > w {
				w = n
			}
		}
		return w
	}
	// total is the widest line the tab writer prints: each column but the
	// last padded by two, the last as it is.
	total := func(cells [][]string) int {
		t := 0
		for i := range cols {
			t += colWidth(cells, i)
			if i < len(cols)-1 {
				t += 2
			}
		}
		return t
	}

	cells := render()
	if width <= 0 {
		return cells
	}
	for order := 1; order <= len(listColumns); order++ {
		for i, c := range cols {
			if c.shrink != order {
				continue
			}
			excess := total(cells) - width
			if excess <= 0 {
				return cells
			}
			cur := colWidth(cells, i)
			floor := max(c.minWidth, cellLen(c.header))
			if cur <= floor {
				continue
			}
			budget[i] = max(floor, cur-excess)
			cells = render()
		}
	}
	return cells
}

// terminalWidth is the width of w when it is a terminal, else 0, which means
// print everything: piped and redirected output is never cut.
func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return 0
	}
	width, _, err := term.GetSize(int(f.Fd()))
	if err != nil || width <= 0 {
		return 0
	}
	return width
}

// writeListTable prints the rows as an aligned table.
func writeListTable(w io.Writer, cols []listColumn, rows []listRow, width int) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	heads := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = c.header
	}
	_, _ = fmt.Fprintln(tw, strings.Join(heads, "\t"))
	for _, cells := range fitRows(cols, rows, width) {
		_, _ = fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// ---- filters ---------------------------------------------------------------

// listFilter selects harnesses. Different filters must all match; within a
// repeated --source or --includes, one is enough.
type listFilter struct {
	labels   labels.Set
	sources  []string
	includes []string
}

func (f listFilter) active() bool {
	return len(f.labels) > 0 || len(f.sources) > 0 || len(f.includes) > 0
}

func (f listFilter) matches(p *harness.Harness) bool {
	if !labels.Set(p.Labels).Matches(f.labels) {
		return false
	}
	if len(f.sources) > 0 {
		var cands []string
		if p.InstalledFrom != nil {
			cands = append(cands, repoCandidate(p.InstalledFrom.Source, p.InstalledFrom.Path, p.Dir))
		}
		if !anyRepoMatch(f.sources, cands) {
			return false
		}
	}
	if len(f.includes) > 0 {
		var cands []string
		for _, inc := range p.Includes {
			if inc.IsLocal() {
				cands = append(cands, repoCandidate(inc.Local, inc.Path, p.Dir))
			} else {
				cands = append(cands, repoCandidate(inc.Git, inc.Path, p.Dir))
			}
		}
		for _, d := range p.DelegatesTo {
			cands = append(cands, repoCandidate(d.Git, d.Path, p.Dir))
		}
		if !anyRepoMatch(f.includes, cands) {
			return false
		}
	}
	return true
}

func anyRepoMatch(queries, candidates []string) bool {
	for _, q := range queries {
		qk := normalizeRepo(q, "")
		for _, c := range candidates {
			if repoWithin(c, qk) {
				return true
			}
		}
	}
	return false
}

// repoCandidate is the normalised repository of a source plus its subpath.
// base resolves a relative local path (an include's, against its harness).
func repoCandidate(source, path, base string) string {
	k := normalizeRepo(source, base)
	if k == "" {
		return ""
	}
	if path != "" {
		k += "/" + strings.Trim(filepath.ToSlash(path), "/")
	}
	return k
}

// repoWithin reports whether candidate is the repository query names, or
// inside it: whole path segments, never a substring or a glob.
func repoWithin(candidate, query string) bool {
	if candidate == "" || query == "" {
		return false
	}
	return candidate == query || strings.HasPrefix(candidate, strings.TrimSuffix(query, "/")+"/")
}

// normalizeRepo reduces a repository reference to one comparable form:
// "host/owner/repo" for a remote (lowercased, without scheme, user, ".git" or
// a trailing slash; a reference with no host is on github.com), or
// "path:/abs/dir" for a local path (relative ones against base, or the
// working directory when base is empty).
func normalizeRepo(ref, base string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if p, ok := strings.CutPrefix(ref, "file://"); ok {
		ref = p
	}
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, ".") || strings.HasPrefix(ref, "~") {
		if strings.HasPrefix(ref, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				ref = filepath.Join(home, strings.TrimPrefix(ref, "~"))
			}
		}
		if !filepath.IsAbs(ref) {
			if base == "" {
				abs, err := filepath.Abs(ref)
				if err != nil {
					return ""
				}
				ref = abs
			} else {
				ref = filepath.Join(base, ref)
			}
		}
		return "path:" + filepath.ToSlash(filepath.Clean(ref))
	}
	if _, rest, ok := strings.Cut(ref, "://"); ok {
		ref = rest
	}
	if at := strings.Index(ref, "@"); at >= 0 && at < strings.IndexAny(ref+"/", "/") {
		ref = ref[at+1:]
	}
	// scp-like host:owner/repo
	if colon, slash := strings.Index(ref, ":"), strings.Index(ref, "/"); colon >= 0 && (slash < 0 || colon < slash) {
		ref = ref[:colon] + "/" + ref[colon+1:]
	}
	ref = strings.Trim(ref, "/")
	ref = strings.TrimSuffix(ref, ".git")
	ref = strings.ToLower(ref)
	first, _, _ := strings.Cut(ref, "/")
	if !strings.Contains(first, ".") {
		ref = "github.com/" + ref
	}
	return ref
}

// add stores the value of --source or --includes.
func (f *listFilter) add(flag, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s requires a repository (owner/repo, a URL or a path)", flag)
	}
	switch flag {
	case "--source":
		f.sources = append(f.sources, value)
	case "--includes":
		f.includes = append(f.includes, value)
	}
	return nil
}
