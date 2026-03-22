package main

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

func TestResolveInstallSourceLocalPath(t *testing.T) {
	cfg := &config.Config{}

	// Rule 1: starts with . or /
	result, err := resolveInstallSource("./my-persona", "", cfg)
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
