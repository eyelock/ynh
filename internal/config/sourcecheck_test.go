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

func TestCheckRemoteSource_NilAllowAll(t *testing.T) {
	cfg := &Config{AllowedRemoteSources: nil}

	if err := cfg.CheckRemoteSource("github.com/anyone/anything"); err != nil {
		t.Errorf("nil allow list should permit all, got: %v", err)
	}
}

func TestCheckRemoteSource_EmptyDenyAll(t *testing.T) {
	cfg := &Config{AllowedRemoteSources: []string{}}

	if err := cfg.CheckRemoteSource("github.com/user/repo"); err == nil {
		t.Error("empty allow list should deny all remote sources")
	}
}

func TestCheckRemoteSource_MatchesAllowed(t *testing.T) {
	cfg := &Config{
		AllowedRemoteSources: []string{
			"github.com/eyelock/*",
			"github.com/acme-corp/shared-skills",
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
		{"github.com/acme-corp/shared-skills", true},
		{"github.com/acme-corp/other-repo", false},
		{"github.com/untrusted/repo", false},
		{"gitlab.com/eyelock/ynh", false},
	}

	for _, tt := range tests {
		err := cfg.CheckRemoteSource(tt.url)
		if tt.allowed && err != nil {
			t.Errorf("CheckRemoteSource(%q) should be allowed, got: %v", tt.url, err)
		}
		if !tt.allowed && err == nil {
			t.Errorf("CheckRemoteSource(%q) should be denied", tt.url)
		}
	}
}

func TestCheckRemoteSource_DeepPaths(t *testing.T) {
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
		err := cfg.CheckRemoteSource(tt.url)
		if tt.allowed && err != nil {
			t.Errorf("CheckRemoteSource(%q) should be allowed, got: %v", tt.url, err)
		}
		if !tt.allowed && err == nil {
			t.Errorf("CheckRemoteSource(%q) should be denied", tt.url)
		}
	}
}
