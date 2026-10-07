package config

import "testing"

func TestNormalizeForMatch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/user/repo", "github.com/user/repo"},
		{"git@github.com:user/repo.git", "github.com/user/repo"},
		{"https://github.com/user/repo.git", "github.com/user/repo"},
		{"https://github.com/user/repo", "github.com/user/repo"},
		{"http://gitlab.com/org/project.git", "gitlab.com/org/project"},
		{"git@gitlab.com:org/deep/nested/repo.git", "gitlab.com/org/deep/nested/repo"},
	}

	for _, tt := range tests {
		got := normalizeForMatch(tt.input)
		if got != tt.want {
			t.Errorf("normalizeForMatch(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// Exact match
		{"github.com/user/repo", "github.com/user/repo", true},
		{"github.com/user/repo", "github.com/user/other", false},

		// Single wildcard
		{"github.com/user/*", "github.com/user/repo", true},
		{"github.com/user/*", "github.com/user/other", true},
		{"github.com/user/*", "github.com/other/repo", false},
		{"github.com/*/repo", "github.com/user/repo", true},
		{"github.com/*/repo", "github.com/org/repo", true},
		{"github.com/*/repo", "github.com/user/other", false},

		// * does not cross path boundaries
		{"github.com/*", "github.com/user/repo", false},

		// ** matches zero or more segments
		{"github.com/user/**", "github.com/user/repo", true},
		{"github.com/user/**", "github.com/user/repo/sub", true},
		{"github.com/user/**", "github.com/user/a/b/c", true},
		{"github.com/user/**", "github.com/other/repo", false},

		// ** matches zero segments
		{"github.com/**/repo", "github.com/repo", true},
		{"github.com/**/repo", "github.com/user/repo", true},
		{"github.com/**/repo", "github.com/a/b/repo", true},
		{"github.com/**/repo", "github.com/a/b/other", false},

		// ** in the middle
		{"github.com/org/**/skills/*", "github.com/org/mono/packages/skills/deploy", true},
		{"github.com/org/**/skills/*", "github.com/org/skills/deploy", true},
		{"github.com/org/**/skills/*", "github.com/other/skills/deploy", false},

		// All hosts wildcard
		{"*/user/repo", "github.com/user/repo", true},
		{"*/user/repo", "gitlab.com/user/repo", true},

		// Different hosts
		{"gitlab.com/org/*", "github.com/org/repo", false},
	}

	for _, tt := range tests {
		got := matchGlob(tt.pattern, tt.path)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestCheckSource_NilAllowAll(t *testing.T) {
	cfg := &Config{AllowedRemoteSources: nil}

	if err := cfg.CheckSource("github.com/anyone/anything", ""); err != nil {
		t.Errorf("nil allow list should permit all, got: %v", err)
	}
}

func TestCheckSource_EmptyDenyAll(t *testing.T) {
	cfg := &Config{AllowedRemoteSources: []string{}}

	if err := cfg.CheckSource("github.com/user/repo", ""); err == nil {
		t.Error("empty allow list should deny all remote sources")
	}
}

func TestCheckSource_MatchesAllowed(t *testing.T) {
	cfg := &Config{
		AllowedRemoteSources: []string{
			"github.com/eyelock/*",
			"github.com/example-org/shared-skills",
		},
	}

	tests := []struct {
		url     string
		allowed bool
	}{
		{"github.com/eyelock/ynh", true},
		{"github.com/eyelock/other-repo", true},
		{"git@github.com:eyelock/ynh.git", true},
		{"https://github.com/eyelock/ynh.git", true},
		{"github.com/example-org/shared-skills", true},
		{"github.com/example-org/other-repo", false},
		{"github.com/untrusted/repo", false},
		{"gitlab.com/eyelock/ynh", false},
	}

	for _, tt := range tests {
		err := cfg.CheckSource(tt.url, "")
		if tt.allowed && err != nil {
			t.Errorf("CheckSource(%q) should be allowed, got: %v", tt.url, err)
		}
		if !tt.allowed && err == nil {
			t.Errorf("CheckSource(%q) should be denied", tt.url)
		}
	}
}

func TestCheckSource_DeepPaths(t *testing.T) {
	cfg := &Config{
		AllowedRemoteSources: []string{
			"github.com/org/**/my-team-skills/*",
		},
	}

	tests := []struct {
		url     string
		allowed bool
	}{
		{"github.com/org/mono/packages/my-team-skills/deploy", true},
		{"github.com/org/my-team-skills/review", true},
		{"github.com/org/a/b/c/my-team-skills/lint", true},
		{"github.com/org/other-skills/deploy", false},
		{"github.com/other-org/mono/my-team-skills/deploy", false},
	}

	for _, tt := range tests {
		err := cfg.CheckSource(tt.url, "")
		if tt.allowed && err != nil {
			t.Errorf("CheckSource(%q) should be allowed, got: %v", tt.url, err)
		}
		if !tt.allowed && err == nil {
			t.Errorf("CheckSource(%q) should be denied", tt.url)
		}
	}
}

func TestCheckSource_GitURLMessage(t *testing.T) {
	cfg := &Config{AllowedRemoteSources: []string{"github.com/eyelock/**"}}
	err := cfg.CheckSource("https://github.com/anthropics/skills.git", "")
	want := `remote source "https://github.com/anthropics/skills.git" is not in the allowed sources list (add "github.com/anthropics/skills" to allowed_remote_sources)`
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

func TestCheckSource_LocalPaths(t *testing.T) {
	const harnessDir = "/work/harness"
	tests := []struct {
		name    string
		allow   []string
		source  string
		allowed bool
	}{
		{"exact absolute", []string{"/tmp/inc"}, "/tmp/inc", true},
		{"other absolute", []string{"/tmp/inc"}, "/tmp/other", false},
		{"entry with trailing slash", []string{"/tmp/inc/"}, "/tmp/inc", true},
		{"source not clean", []string{"/tmp/inc"}, "/tmp/x/../inc/", true},
		{"dot-dot cannot escape a glob", []string{"/tmp/*"}, "/tmp/../etc", false},
		{"star is one segment", []string{"/tmp/*"}, "/tmp/inc", true},
		{"star does not cross a slash", []string{"/tmp/*"}, "/tmp/a/inc", false},
		{"double star is any depth", []string{"/Users/me/shared/**"}, "/Users/me/shared/a/b/inc", true},
		{"double star covers the root itself", []string{"/Users/me/shared/**"}, "/Users/me/shared", true},
		{"double star stays under its root", []string{"/Users/me/shared/**"}, "/Users/me/other/inc", false},
		{"file url", []string{"/tmp/inc"}, "file:///tmp/inc", true},
		{"relative resolved against the harness", []string{"/work/harness/inc"}, "./inc", true},
		{"parent-relative resolved against the harness", []string{"/work/shared/*"}, "../shared/inc", true},
		{"relative is not matched as written", []string{"./inc"}, "./inc", false},
		{"git url entry does not admit a path", []string{"github.com/**"}, "/tmp/inc", false},
		{"path entry does not admit a git url", []string{"/**"}, "github.com/user/repo", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{AllowedRemoteSources: tt.allow}
			err := cfg.CheckSource(tt.source, harnessDir)
			if tt.allowed && err != nil {
				t.Errorf("CheckSource(%q) should be allowed by %v, got: %v", tt.source, tt.allow, err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("CheckSource(%q) should be denied by %v", tt.source, tt.allow)
			}
		})
	}
}

func TestCheckSource_LocalPathMessage(t *testing.T) {
	cfg := &Config{AllowedRemoteSources: []string{"github.com/eyelock/**"}}
	tests := []struct {
		source string
		want   string
	}{
		{"/tmp/inc", `source "/tmp/inc" is not in the allowed sources list (add "/tmp/inc" to allowed_remote_sources)`},
		{"./inc", `source "./inc" is not in the allowed sources list (add "/work/harness/inc" to allowed_remote_sources)`},
		{"file:///tmp/inc", `source "file:///tmp/inc" is not in the allowed sources list (add "/tmp/inc" to allowed_remote_sources)`},
	}
	for _, tt := range tests {
		err := cfg.CheckSource(tt.source, "/work/harness")
		if err == nil || err.Error() != tt.want {
			t.Errorf("CheckSource(%q) = %v, want %q", tt.source, err, tt.want)
		}
	}
}

func TestLocalSourcePath(t *testing.T) {
	tests := []struct {
		source  string
		baseDir string
		want    string
		local   bool
	}{
		{"/tmp/inc", "/h", "/tmp/inc", true},
		{"./inc", "/h", "/h/inc", true},
		{"../inc", "/h/x", "/h/inc", true},
		{"file:///tmp/inc", "/h", "/tmp/inc", true},
		{"file://host/tmp/inc", "/h", "", false},
		{"~/inc", "/h", "", false},
		{"github.com/user/repo", "/h", "", false},
		{"git@github.com:user/repo.git", "/h", "", false},
		{"https://github.com/user/repo", "/h", "", false},
		{"./inc", "", "inc", true},
	}
	for _, tt := range tests {
		got, local := localSourcePath(tt.source, tt.baseDir)
		if got != tt.want || local != tt.local {
			t.Errorf("localSourcePath(%q, %q) = (%q, %v), want (%q, %v)", tt.source, tt.baseDir, got, local, tt.want, tt.local)
		}
	}
}
