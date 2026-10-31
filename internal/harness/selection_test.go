package harness

import (
	"maps"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestSplitQualified(t *testing.T) {
	tests := []struct {
		in, ns, name string
		wantErr      bool
	}{
		{"work", "", "work", false},
		{"github:triage", "github", "triage", false},
		{"my.ns:a.b", "my.ns", "a.b", false},
		{":x", "", "", true},
		{"x:", "", "", true},
		{"a:b:c", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			ns, name, err := SplitQualified(tt.in)
			if (err != nil) != tt.wantErr || ns != tt.ns || name != tt.name {
				t.Errorf("SplitQualified(%q) = %q, %q, %v", tt.in, ns, name, err)
			}
		})
	}
}

func TestParseSelection(t *testing.T) {
	tests := []struct {
		name     string
		profiles []string
		focus    string
		want     Selection
		wantErr  string
	}{
		{name: "nothing"},
		{name: "root only", profiles: []string{"work"}, want: Selection{Profile: "work"}},
		{name: "root and included", profiles: []string{"work", "github:ci"},
			want: Selection{Profile: "work", Included: map[string]string{"github": "ci"}}},
		{name: "two namespaces", profiles: []string{"a:x", "b:y"},
			want: Selection{Included: map[string]string{"a": "x", "b": "y"}}},
		{name: "duplicate root", profiles: []string{"one", "two"}, wantErr: "at most one unqualified profile"},
		{name: "duplicate namespace", profiles: []string{"gh:one", "gh:two"}, wantErr: `namespace "gh"`},
		{name: "malformed", profiles: []string{"a:b:c"}, wantErr: "expected name or namespace:name"},
		{name: "root focus is not parsed", focus: "triage", want: Selection{}},
		{name: "namespaced focus", focus: "github:triage", want: Selection{FocusNS: "github", FocusName: "triage"}},
		{name: "malformed focus", focus: "x:", wantErr: "expected name or namespace:name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSelection(tt.profiles, tt.focus)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Profile != tt.want.Profile || got.FocusNS != tt.want.FocusNS || got.FocusName != tt.want.FocusName ||
				!maps.Equal(got.Included, tt.want.Included) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestValidateIncludeAlias(t *testing.T) {
	for alias, wantErr := range map[string]bool{
		"": false, "gh-work": false, "a.b_c": false,
		"a:b": true, "-x": true, "has space": true, ".dot": true,
	} {
		if err := ValidateIncludeAlias(alias); (err != nil) != wantErr {
			t.Errorf("ValidateIncludeAlias(%q) = %v, want error: %v", alias, err, wantErr)
		}
	}
}

func TestLoadDir_RejectsBadIncludeAlias(t *testing.T) {
	for _, as := range []string{"a:b", "-x"} {
		dir := t.TempDir()
		hj := &plugin.HarnessJSON{Name: "h", Version: "1.0.0", Includes: []plugin.IncludeMeta{{Local: "x", As: as}}}
		if err := plugin.SavePluginJSON(dir, hj); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "invalid include alias") {
			t.Errorf("as %q: error = %v", as, err)
		}
	}
}

func TestLoadDir_CarriesIncludeAlias(t *testing.T) {
	dir := t.TempDir()
	hj := &plugin.HarnessJSON{Name: "h", Version: "1.0.0", Includes: []plugin.IncludeMeta{{Local: "x", As: "gh-work"}}}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
	h, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Includes) != 1 || h.Includes[0].As != "gh-work" {
		t.Errorf("includes = %+v", h.Includes)
	}
}

func TestResolveProfile_ProfileIncludeKeepsAliasAndValidatesIt(t *testing.T) {
	h := &Harness{Name: "x", Profiles: map[string]plugin.Profile{
		"ok":  {Includes: []plugin.IncludeMeta{{Local: "p", As: "alias"}}},
		"bad": {Includes: []plugin.IncludeMeta{{Local: "p", As: "a:b"}}},
	}}
	got, err := ResolveProfile(h, "ok")
	if err != nil || len(got.Includes) != 1 || got.Includes[0].As != "alias" {
		t.Fatalf("ok: %+v, %v", got, err)
	}
	if _, err := ResolveProfile(h, "bad"); err == nil || !strings.Contains(err.Error(), "invalid include alias") {
		t.Errorf("bad: error = %v", err)
	}
}

func TestAddInclude_As(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{As: "acme"}); err != nil {
		t.Fatal(err)
	}
	if incs := loadIncludes(t, dir); len(incs) != 1 || incs[0].As != "acme" {
		t.Errorf("includes = %+v", incs)
	}
	if err := AddInclude(dir, "github.com/acme/other", AddOptions{As: "a:b"}); err == nil {
		t.Error("an alias with a colon should be refused")
	}
}
