package main

import (
	"os"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

func TestResolveInstallSourceLocalPath(t *testing.T) {
	cfg := &config.Config{}

	// Rule 1: starts with . or /
	result, err := resolveInstallSource("./my-harness", "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.gitURL != "" {
		t.Error("local path should not resolve to gitURL")
	}
	if result.sourceType != "local" {
		t.Errorf("sourceType = %q, want %q", result.sourceType, "local")
	}

	result, err = resolveInstallSource("/abs/path", "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.gitURL != "" {
		t.Error("absolute path should not resolve to gitURL")
	}
	if result.sourceType != "local" {
		t.Errorf("sourceType = %q, want %q", result.sourceType, "local")
	}
}

func TestResolveInstallSourceGitSSH(t *testing.T) {
	cfg := &config.Config{}

	result, err := resolveInstallSource("git@github.com:user/repo.git", "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.gitURL != "" {
		t.Error("SSH URL should pass through without registry lookup")
	}
	if result.sourceType != "git" {
		t.Errorf("sourceType = %q, want %q", result.sourceType, "git")
	}
}

func TestResolveInstallSourceGitHTTPS(t *testing.T) {
	cfg := &config.Config{}

	result, err := resolveInstallSource("https://github.com/user/repo", "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.gitURL != "" {
		t.Error("HTTPS URL should pass through without registry lookup")
	}
	if result.sourceType != "git" {
		t.Errorf("sourceType = %q, want %q", result.sourceType, "git")
	}
}

func TestResolveInstallSourceGitShorthand(t *testing.T) {
	cfg := &config.Config{}

	result, err := resolveInstallSource("github.com/user/repo", "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.gitURL != "" {
		t.Error("shorthand URL should pass through without registry lookup")
	}
	if result.sourceType != "git" {
		t.Errorf("sourceType = %q, want %q", result.sourceType, "git")
	}
}

func TestResolveInstallSourcePlainWordNoRegistries(t *testing.T) {
	cfg := &config.Config{}

	_, err := resolveInstallSource("david", "", cfg)
	if err == nil {
		t.Fatal("expected error for no registries")
	}
	if !strings.Contains(err.Error(), "no registries configured") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestResolveInstallSourceAtSignNoRegistries(t *testing.T) {
	cfg := &config.Config{}

	_, err := resolveInstallSource("david@myregistry", "", cfg)
	if err == nil {
		t.Fatal("expected error for no registries")
	}
	if !strings.Contains(err.Error(), "no registries configured") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestResolveInstallSourceHTTPNotRegistry(t *testing.T) {
	cfg := &config.Config{}

	// http:// should be treated as Git URL, not registry
	result, err := resolveInstallSource("http://github.com/user/repo", "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.gitURL != "" {
		t.Error("HTTP URL should pass through")
	}
	if result.sourceType != "git" {
		t.Errorf("sourceType = %q, want %q", result.sourceType, "git")
	}
}

func TestResolveInstallSourceMissingRelativePath(t *testing.T) {
	cfg := &config.Config{}
	t.Chdir(t.TempDir())

	for _, src := range []string{"harnesses/martin", "a/b/c"} {
		_, err := resolveInstallSource(src, "", cfg)
		if err == nil {
			t.Fatalf("%q: expected an error for a missing local path", src)
		}
		if !strings.Contains(err.Error(), "does not exist as a local path") || !strings.Contains(err.Error(), "./"+src) {
			t.Errorf("%q: error %q should name the missing path and suggest ./", src, err)
		}
		if strings.Contains(err.Error(), "git@") {
			t.Errorf("%q: error must not mention an SSH URL: %v", src, err)
		}
	}
}

func TestResolveInstallSourceExistingRelativePath(t *testing.T) {
	cfg := &config.Config{}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("harnesses/martin", 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := resolveInstallSource("harnesses/martin", "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.sourceType != "local" {
		t.Errorf("sourceType = %q, want local", result.sourceType)
	}
}

func TestResolveInstallSourceGitFormsKeepWorking(t *testing.T) {
	cfg := &config.Config{}
	t.Chdir(t.TempDir())

	for _, src := range []string{
		"github.com/org/repo",
		"gitlab.example.com/org/repo",
		"git@github.com:org/repo.git",
		"https://github.com/org/repo",
		"localhost:8080/org/repo",
	} {
		result, err := resolveInstallSource(src, "", cfg)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", src, err)
			continue
		}
		if result.sourceType != "git" {
			t.Errorf("%q: sourceType = %q, want git", src, result.sourceType)
		}
	}
}
