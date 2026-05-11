package namespace

import "testing"

func TestDeriveFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/eyelock/assistants", "eyelock/assistants"},
		{"https://github.com/eyelock/assistants.git", "eyelock/assistants"},
		{"git@github.com:eyelock/assistants.git", "eyelock/assistants"},
		{"git@github.com:eyelock/assistants", "eyelock/assistants"},
		{"https://gitlab.com/myorg/tools", "myorg/tools"},
		{"github.com/eyelock/assistants", "eyelock/assistants"},
		{"/Users/david/harnesses", "david/harnesses"},
		{"/harnesses", "local/harnesses"},
		{"./harnesses", "local/harnesses"},
	}

	for _, tc := range tests {
		got := DeriveFromURL(tc.url)
		if got != tc.want {
			t.Errorf("DeriveFromURL(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestParseQualified(t *testing.T) {
	tests := []struct {
		ref     string
		name    string
		ns      string
		wantErr bool
	}{
		{"david@eyelock/assistants", "david", "eyelock/assistants", false},
		{"david", "david", "", false},
		{"david@", "", "", true},
		{"@eyelock/assistants", "", "", true},
	}

	for _, tc := range tests {
		name, ns, err := ParseQualified(tc.ref)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseQualified(%q): expected error, got none", tc.ref)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseQualified(%q): unexpected error: %v", tc.ref, err)
			continue
		}
		if name != tc.name {
			t.Errorf("ParseQualified(%q) name = %q, want %q", tc.ref, name, tc.name)
		}
		if ns != tc.ns {
			t.Errorf("ParseQualified(%q) ns = %q, want %q", tc.ref, ns, tc.ns)
		}
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		ref  string
		want RefKind
	}{
		// Paths
		{"./planner", RefPath},
		{"../shared/reviewer", RefPath},
		{"/abs/path/planner", RefPath},
		{"~/work/planner", RefPath},
		{".", RefPath},
		{"..", RefPath},
		{"C:/Users/david", RefPath},
		{`C:\Users\david`, RefPath},
		{"d:/lower", RefPath},
		// Canonical ids
		{"github.com/eyelock/assistants/planner", RefID},
		{"gitlab.com/myorg/tools/linter", RefID},
		{"local/planner", RefID},
		{"a/b", RefID},
		{"internal/team/planner", RefID},
		// Invalid
		{"", RefInvalid},
		{"planner", RefInvalid},                                  // bare name
		{"planner@eyelock/assistants", RefInvalid},               // legacy qualified form
		{"github.com/eyelock/assistants/planner@v2", RefInvalid}, // version pin not yet accepted
	}
	for _, tc := range tests {
		got := Classify(tc.ref)
		if got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.ref, got, tc.want)
		}
	}
}

func TestSplitID(t *testing.T) {
	tests := []struct {
		id     string
		wantNS string
		wantNm string
	}{
		{"github.com/eyelock/assistants/planner", "github.com/eyelock/assistants", "planner"},
		{"local/planner", "local", "planner"},
		{"a/b", "a", "b"},
		{"planner", "", ""},
		{"", "", ""},
	}
	for _, tc := range tests {
		ns, nm := SplitID(tc.id)
		if ns != tc.wantNS || nm != tc.wantNm {
			t.Errorf("SplitID(%q) = (%q, %q), want (%q, %q)", tc.id, ns, nm, tc.wantNS, tc.wantNm)
		}
	}
}

func TestCanonicalID(t *testing.T) {
	tests := []struct {
		sourceURL string
		name      string
		want      string
	}{
		{"https://github.com/eyelock/assistants", "planner", "github.com/eyelock/assistants/planner"},
		{"https://github.com/eyelock/assistants.git", "planner", "github.com/eyelock/assistants/planner"},
		{"git@github.com:eyelock/assistants.git", "planner", "github.com/eyelock/assistants/planner"},
		{"git@github.com:eyelock/assistants", "planner", "github.com/eyelock/assistants/planner"},
		{"https://gitlab.com/myorg/tools", "linter", "gitlab.com/myorg/tools/linter"},
		{"http://codeberg.org/team/repo", "fmt", "codeberg.org/team/repo/fmt"},
		{"", "planner", "local/planner"},
		{"/Users/david/harnesses", "planner", "local/planner"},
		{"./harnesses", "planner", "local/planner"},
		{"~/harnesses", "planner", "local/planner"},
		{"github.com/eyelock/assistants", "planner", "github.com/eyelock/assistants/planner"}, // host-prefixed, no scheme
		{"https://example.com", "planner", "local/planner"},                                   // no org/repo
		{"https://github.com/eyelock/assistants", "", ""},                                     // empty name → empty id
	}
	for _, tc := range tests {
		got := CanonicalID(tc.sourceURL, tc.name)
		if got != tc.want {
			t.Errorf("CanonicalID(%q, %q) = %q, want %q", tc.sourceURL, tc.name, got, tc.want)
		}
	}
}

func TestToFromFSName(t *testing.T) {
	tests := []string{
		"eyelock/assistants",
		"myorg/tools",
		"a/b",
	}
	for _, ns := range tests {
		fs := ToFSName(ns)
		back := FromFSName(fs)
		if back != ns {
			t.Errorf("round-trip %q → %q → %q", ns, fs, back)
		}
	}
}
