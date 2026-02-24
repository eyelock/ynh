package main

import (
	"os"
	"path/filepath"
	"sort"
)

// signalCategory groups discovered files by their purpose in a project.
type signalCategory string

const (
	catBuild   signalCategory = "Build"
	catCI      signalCategory = "CI/CD"
	catLint    signalCategory = "Lint/Format"
	catTest    signalCategory = "Test"
	catRelease signalCategory = "Release"
	catDocs    signalCategory = "Docs"
	catDocker  signalCategory = "Container"
	catIaC     signalCategory = "Infrastructure"
	catGitHub  signalCategory = "GitHub"
)

// signal is a discovered file with its category and reading priority.
type signal struct {
	Category signalCategory
	Path     string
	Priority int // lower = more important (read first for LLM context)
}

// signalEntry defines a file to look for.
type signalEntry struct {
	Name     string
	Category signalCategory
	Priority int
}

// rootSignals are specific files checked at the project root.
var rootSignals = []signalEntry{
	// Build systems — highest priority, tells you the language and tools
	{"Makefile", catBuild, 1},
	{"justfile", catBuild, 1},
	{"Taskfile.yml", catBuild, 1},
	{"Taskfile.yaml", catBuild, 1},
	{"Rakefile", catBuild, 1},

	// Go
	{"go.mod", catBuild, 1},
	{"go.sum", catBuild, 5},

	// Node.js
	{"package.json", catBuild, 1},
	{"package-lock.json", catBuild, 5},
	{"yarn.lock", catBuild, 5},
	{"pnpm-lock.yaml", catBuild, 5},
	{"bun.lockb", catBuild, 5},

	// Python
	{"pyproject.toml", catBuild, 1},
	{"setup.py", catBuild, 2},
	{"setup.cfg", catBuild, 2},
	{"requirements.txt", catBuild, 3},
	{"Pipfile", catBuild, 2},
	{"Pipfile.lock", catBuild, 5},
	{"poetry.lock", catBuild, 5},

	// Rust
	{"Cargo.toml", catBuild, 1},
	{"Cargo.lock", catBuild, 5},

	// Java/JVM
	{"pom.xml", catBuild, 1},
	{"build.gradle", catBuild, 1},
	{"build.gradle.kts", catBuild, 1},
	{"settings.gradle", catBuild, 3},
	{"settings.gradle.kts", catBuild, 3},
	{"gradle.properties", catBuild, 3},

	// C/C++
	{"CMakeLists.txt", catBuild, 1},
	{"Makefile.am", catBuild, 2},
	{"meson.build", catBuild, 1},

	// Ruby
	{"Gemfile", catBuild, 1},
	{"Gemfile.lock", catBuild, 5},

	// PHP
	{"composer.json", catBuild, 1},
	{"composer.lock", catBuild, 5},

	// .NET
	{"Directory.Build.props", catBuild, 2},
	{"global.json", catBuild, 3},

	// Swift
	{"Package.swift", catBuild, 1},
	{"Podfile", catBuild, 2},

	// Dart/Flutter
	{"pubspec.yaml", catBuild, 1},

	// Elixir
	{"mix.exs", catBuild, 1},

	// TypeScript config
	{"tsconfig.json", catBuild, 3},
	{"deno.json", catBuild, 1},
	{"deno.jsonc", catBuild, 1},

	// CI/CD — root-level CI configs
	{".gitlab-ci.yml", catCI, 2},
	{"Jenkinsfile", catCI, 2},
	{".travis.yml", catCI, 2},
	{"azure-pipelines.yml", catCI, 2},
	{"bitbucket-pipelines.yml", catCI, 2},
	{".drone.yml", catCI, 2},
	{"Procfile", catCI, 3},
	{"app.yaml", catCI, 3},

	// Lint/Format configs
	{".golangci.yml", catLint, 3},
	{".golangci.yaml", catLint, 3},
	{".eslintrc", catLint, 3},
	{".eslintrc.js", catLint, 3},
	{".eslintrc.cjs", catLint, 3},
	{".eslintrc.json", catLint, 3},
	{".eslintrc.yml", catLint, 3},
	{"eslint.config.js", catLint, 3},
	{"eslint.config.mjs", catLint, 3},
	{".prettierrc", catLint, 3},
	{".prettierrc.js", catLint, 3},
	{".prettierrc.json", catLint, 3},
	{".prettierrc.yml", catLint, 3},
	{"biome.json", catLint, 3},
	{".flake8", catLint, 3},
	{".pylintrc", catLint, 3},
	{"pylintrc", catLint, 3},
	{".ruff.toml", catLint, 3},
	{"ruff.toml", catLint, 3},
	{"rustfmt.toml", catLint, 3},
	{"clippy.toml", catLint, 3},
	{".rubocop.yml", catLint, 3},
	{".editorconfig", catLint, 4},
	{".markdownlint.json", catLint, 4},
	{".markdownlintrc", catLint, 4},
	{".shellcheckrc", catLint, 4},
	{".stylelintrc", catLint, 4},

	// Test configs
	{"jest.config.js", catTest, 3},
	{"jest.config.ts", catTest, 3},
	{"jest.config.json", catTest, 3},
	{"vitest.config.ts", catTest, 3},
	{"vitest.config.js", catTest, 3},
	{"vitest.config.mts", catTest, 3},
	{"pytest.ini", catTest, 3},
	{"conftest.py", catTest, 3},
	{"tox.ini", catTest, 3},
	{".mocharc.js", catTest, 3},
	{".mocharc.yml", catTest, 3},
	{".mocharc.json", catTest, 3},
	{"cypress.config.js", catTest, 3},
	{"cypress.config.ts", catTest, 3},
	{"playwright.config.ts", catTest, 3},
	{"playwright.config.js", catTest, 3},

	// Release
	{".goreleaser.yml", catRelease, 2},
	{".goreleaser.yaml", catRelease, 2},
	{".release-it.json", catRelease, 3},
	{"release.config.js", catRelease, 3},
	{"release.config.cjs", catRelease, 3},
	{"lerna.json", catRelease, 3},
	{".changeset", catRelease, 3},

	// Docs
	{"README.md", catDocs, 1},
	{"README", catDocs, 1},
	{"README.rst", catDocs, 1},
	{"CHANGELOG.md", catDocs, 3},
	{"CHANGELOG", catDocs, 3},
	{"SECURITY.md", catDocs, 4},
	{"CODE_OF_CONDUCT.md", catDocs, 5},

	// Container
	{"Dockerfile", catDocker, 3},
	{"docker-compose.yml", catDocker, 3},
	{"docker-compose.yaml", catDocker, 3},
	{"compose.yml", catDocker, 3},
	{"compose.yaml", catDocker, 3},
	{".dockerignore", catDocker, 5},

	// IaC
	{"Vagrantfile", catIaC, 3},
	{"Pulumi.yaml", catIaC, 3},
	{"serverless.yml", catIaC, 3},

	// Metadata
	{".gitignore", catBuild, 5},
	{".nvmrc", catBuild, 5},
	{".node-version", catBuild, 5},
	{".python-version", catBuild, 5},
	{".ruby-version", catBuild, 5},
	{".tool-versions", catBuild, 5},
	{".env.example", catBuild, 5},
}

// globSignals are patterns checked via filepath.Glob.
var globSignals = []struct {
	Pattern  string
	Category signalCategory
	Priority int
}{
	// GitHub Actions workflows
	{".github/workflows/*.yml", catCI, 2},
	{".github/workflows/*.yaml", catCI, 2},

	// GitHub community files
	{".github/CONTRIBUTING.md", catDocs, 2},
	{".github/CONTRIBUTING", catDocs, 2},
	{".github/PULL_REQUEST_TEMPLATE.md", catGitHub, 3},
	{".github/CODEOWNERS", catGitHub, 3},
	{".github/dependabot.yml", catCI, 3},
	{".github/dependabot.yaml", catCI, 3},
	{".github/ISSUE_TEMPLATE/*.md", catGitHub, 4},
	{".github/ISSUE_TEMPLATE/*.yml", catGitHub, 4},

	// CircleCI
	{".circleci/config.yml", catCI, 2},
	{".circleci/config.yaml", catCI, 2},

	// Terraform (check for .tf files in common locations)
	{"terraform/*.tf", catIaC, 3},
	{"infra/*.tf", catIaC, 3},
	{"infrastructure/*.tf", catIaC, 3},

	// .NET project files at root
	{"*.csproj", catBuild, 2},
	{"*.fsproj", catBuild, 2},
	{"*.sln", catBuild, 2},

	// Kubernetes
	{"k8s/*.yml", catDocker, 3},
	{"k8s/*.yaml", catDocker, 3},
	{"kubernetes/*.yml", catDocker, 3},
	{"kubernetes/*.yaml", catDocker, 3},

	// Helm
	{"helm/Chart.yaml", catDocker, 3},
	{"charts/*/Chart.yaml", catDocker, 3},
}

// scanSignals finds project signal files in root. Returns them sorted by priority.
func scanSignals(root string) []signal {
	var found []signal
	seen := make(map[string]bool)

	// Check specific root-level files
	for _, entry := range rootSignals {
		path := filepath.Join(root, entry.Name)
		if _, err := os.Stat(path); err == nil {
			if !seen[path] {
				found = append(found, signal{entry.Category, path, entry.Priority})
				seen[path] = true
			}
		}
	}

	// Check glob patterns
	for _, g := range globSignals {
		pattern := filepath.Join(root, g.Pattern)
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if !seen[m] {
				found = append(found, signal{g.Category, m, g.Priority})
				seen[m] = true
			}
		}
	}

	// Sort by priority (lower = more important)
	sort.Slice(found, func(i, j int) bool {
		if found[i].Priority != found[j].Priority {
			return found[i].Priority < found[j].Priority
		}
		return found[i].Path < found[j].Path
	})

	return found
}

// signalsByCategory groups signals by their category for display.
func signalsByCategory(signals []signal) map[signalCategory][]signal {
	grouped := make(map[signalCategory][]signal)
	for _, s := range signals {
		grouped[s.Category] = append(grouped[s.Category], s)
	}
	return grouped
}

// topSignalFiles returns the highest-priority files up to the given count.
func topSignalFiles(signals []signal, maxFiles int) []signal {
	if len(signals) <= maxFiles {
		return signals
	}
	return signals[:maxFiles]
}

// categoryOrder defines display order for signal categories.
var categoryOrder = []signalCategory{
	catBuild,
	catCI,
	catTest,
	catLint,
	catRelease,
	catDocs,
	catGitHub,
	catDocker,
	catIaC,
}
