package vendor

import (
	"strconv"
	"strings"
)

// Some vendor renderings put their own YAML frontmatter at the top of a file
// whose source may already open with one: Cursor's .mdc rules, Copilot's
// instructions file. Writing the vendor block in front of the source's gives
// the file two, and only the first is read as frontmatter (#532). These
// helpers let a renderer merge the two into one.
//
// They read the subset of YAML that rule and instruction frontmatter uses
// (top-level keys with a plain or quoted scalar, a folded or literal block
// scalar, or a flow or block list) and no more: a full YAML parser is a
// dependency this repository has chosen not to carry.

// splitFrontmatter separates a leading frontmatter block from the body.
// lines holds the block's content lines without the "---" fences or line
// endings; body is everything after the closing fence. ok is false when data
// does not open with a closed block, and body is then all of data: an opening
// "---" with no closing one is text, not frontmatter, so nothing is dropped.
func splitFrontmatter(data []byte) (lines []string, body []byte, ok bool) {
	s := strings.TrimPrefix(string(data), "\ufeff")
	first, rest, found := strings.Cut(s, "\n")
	if !found || strings.TrimRight(first, "\r \t") != "---" {
		return nil, data, false
	}
	lines = []string{}
	for {
		line, after, more := strings.Cut(rest, "\n")
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimRight(line, " \t") == "---" {
			return lines, []byte(after), true
		}
		if !more {
			return nil, data, false
		}
		lines = append(lines, line)
		rest = after
	}
}

// frontmatterEntry is one top-level key: the text after its colon, and the
// indented or list-item lines that continue it.
type frontmatterEntry struct {
	key   string
	value string
	cont  []string
	raw   []string // the key line and its continuation lines, verbatim
}

// frontmatterEntries groups block lines into top-level entries. A line that
// is neither a "key:" line nor a continuation of one (no colon, an empty key,
// a stray line before the first key) is skipped, so a malformed block yields
// whatever entries it does contain rather than failing.
func frontmatterEntries(lines []string) []frontmatterEntry {
	var out []frontmatterEntry
	for _, line := range lines {
		isCont := line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '-'
		if isCont {
			if n := len(out); n > 0 {
				out[n-1].cont = append(out[n-1].cont, line)
				out[n-1].raw = append(out[n-1].raw, line)
			}
			continue
		}
		if line[0] == '#' {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			continue
		}
		out = append(out, frontmatterEntry{key: key, value: strings.TrimSpace(value), raw: []string{line}})
	}
	return out
}

// scalar returns the entry's value as one string. A folded (>) or literal (|)
// block scalar, or a plain scalar continued on indented lines, is joined with
// single spaces: every caller wants a one-line value.
func (e frontmatterEntry) scalar() string {
	v := e.value
	if v == "" || v[0] == '>' || v[0] == '|' {
		var parts []string
		for _, l := range e.cont {
			if t := strings.TrimSpace(l); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " ")
	}
	return unquoteScalar(v)
}

// list returns the entry's items: a flow list ("[a, b]"), a block list of
// "- item" lines, or a lone scalar as a single item. A scalar is never split
// on commas, because a glob such as "*.{ts,tsx}" contains them.
func (e frontmatterEntry) list() []string {
	var items []string
	switch v := e.value; {
	case strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]"):
		items = splitFlowList(v[1 : len(v)-1])
	case v != "":
		items = []string{unquoteScalar(v)}
	default:
		for _, l := range e.cont {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "-") {
				items = append(items, unquoteScalar(strings.TrimSpace(t[1:])))
			}
		}
	}
	out := items[:0]
	for _, it := range items {
		if it != "" {
			out = append(out, it)
		}
	}
	return out
}

// splitFlowList splits the inside of a YAML flow list on the commas that
// separate items, not those inside quotes or braces.
func splitFlowList(s string) []string {
	var items []string
	var quote rune
	depth, start := 0, 0
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '{' || r == '[':
			depth++
		case r == '}' || r == ']':
			depth--
		case r == ',' && depth == 0:
			items = append(items, unquoteScalar(strings.TrimSpace(s[start:i])))
			start = i + 1
		}
	}
	return append(items, unquoteScalar(strings.TrimSpace(s[start:])))
}

// unquoteScalar strips YAML quoting from a single-line scalar, or a trailing
// comment from a plain one.
func unquoteScalar(v string) string {
	if len(v) >= 2 {
		switch {
		case v[0] == '"' && v[len(v)-1] == '"':
			if u, err := strconv.Unquote(v); err == nil {
				return u
			}
			return v[1 : len(v)-1]
		case v[0] == '\'' && v[len(v)-1] == '\'':
			return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}
