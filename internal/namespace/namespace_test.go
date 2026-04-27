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
