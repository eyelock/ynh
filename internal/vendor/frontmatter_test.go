package vendor

import (
	"reflect"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantLines []string
		wantBody  string
		wantOK    bool
	}{
		{"none", "body\n", nil, "body\n", false},
		{"opener only, no newline", "---", nil, "---", false},
		{"unclosed", "---\na: b\n", nil, "---\na: b\n", false},
		{"empty block", "---\n---\nbody", []string{}, "body", true},
		{"closing fence at end of file", "---\na: b\n---", []string{"a: b"}, "", true},
		{"fence with trailing spaces", "--- \na: b\n---  \nbody", []string{"a: b"}, "body", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, body, ok := splitFrontmatter([]byte(tt.in))
			if ok != tt.wantOK || string(body) != tt.wantBody || !reflect.DeepEqual(lines, tt.wantLines) {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)", lines, body, ok, tt.wantLines, tt.wantBody, tt.wantOK)
			}
		})
	}
}

func TestFrontmatterEntries(t *testing.T) {
	lines := []string{
		"  stray continuation before any key",
		"# a comment",
		"plain: value # trailing comment",
		"single: 'it''s'",
		"badquote: \"unterminated\\\"",
		"literal: |",
		"  one",
		"",
		"  two",
		"flow: ['a,b', \"c\", , {x,y}]",
		"block:",
		"- top-level item",
		"  - nested item",
		"  not an item",
		"empty:",
	}
	got := map[string]frontmatterEntry{}
	var keys []string
	for _, e := range frontmatterEntries(lines) {
		got[e.key] = e
		keys = append(keys, e.key)
	}
	wantKeys := []string{"plain", "single", "badquote", "literal", "flow", "block", "empty"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %q, want %q", keys, wantKeys)
	}

	scalars := map[string]string{
		"plain":    "value",
		"single":   "it's",
		"badquote": "unterminated\\",
		"literal":  "one two",
		"empty":    "",
	}
	for k, want := range scalars {
		if s := got[k].scalar(); s != want {
			t.Errorf("%s.scalar() = %q, want %q", k, s, want)
		}
	}

	lists := map[string][]string{
		"flow":  {"a,b", "c", "{x,y}"},
		"block": {"top-level item", "nested item"},
		"plain": {"value"},
		"empty": nil,
	}
	for k, want := range lists {
		if l := got[k].list(); !reflect.DeepEqual(l, want) {
			t.Errorf("%s.list() = %q, want %q", k, l, want)
		}
	}

	if raw := got["literal"].raw; len(raw) != 4 {
		t.Errorf("literal.raw = %q, want the key line and its three continuation lines", raw)
	}
}
