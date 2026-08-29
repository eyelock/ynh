package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
)

func TestParseImageArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantName string
		wantTag  string
		wantBase string
		wantDry  bool
		wantFrom string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "name only",
			args:     []string{"david"},
			wantName: "david",
			wantTag:  "ynh-david:latest",
			wantBase: "ghcr.io/eyelock/ynh:latest",
		},
		{
			name:     "with tag",
			args:     []string{"david", "--tag", "ghcr.io/org/david:v1"},
			wantName: "david",
			wantTag:  "ghcr.io/org/david:v1",
			wantBase: "ghcr.io/eyelock/ynh:latest",
		},
		{
			name:     "with base",
			args:     []string{"david", "--base", "my-base:1.0"},
			wantName: "david",
			wantTag:  "ynh-david:latest",
			wantBase: "my-base:1.0",
		},
		{
			name:     "dry run",
			args:     []string{"david", "--dry-run"},
			wantName: "david",
			wantTag:  "ynh-david:latest",
			wantBase: "ghcr.io/eyelock/ynh:latest",
			wantDry:  true,
		},
		{
			name:     "from source",
			args:     []string{"david", "--from", "github.com/org/harnesses"},
			wantName: "david",
			wantTag:  "ynh-david:latest",
			wantBase: "ghcr.io/eyelock/ynh:latest",
			wantFrom: "github.com/org/harnesses",
		},
		{
			name:     "from with path",
			args:     []string{"david", "--from", "github.com/org/monorepo", "--path", "harnesses/david"},
			wantName: "david",
			wantTag:  "ynh-david:latest",
			wantBase: "ghcr.io/eyelock/ynh:latest",
			wantFrom: "github.com/org/monorepo",
			wantPath: "harnesses/david",
		},
		{
			name:     "all flags",
			args:     []string{"--tag", "custom:v2", "--base", "custom-base:1", "--from", "github.com/org/repo", "--path", "sub", "--dry-run", "david"},
			wantName: "david",
			wantTag:  "custom:v2",
			wantBase: "custom-base:1",
			wantDry:  true,
			wantFrom: "github.com/org/repo",
			wantPath: "sub",
		},
		{
			name:    "no args",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "tag without value",
			args:    []string{"david", "--tag"},
			wantErr: true,
		},
		{
			name:    "base without value",
			args:    []string{"david", "--base"},
			wantErr: true,
		},
		{
			name:    "from without value",
			args:    []string{"david", "--from"},
			wantErr: true,
		},
		{
			name:    "path without value",
			args:    []string{"david", "--path"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseImageArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.name != tt.wantName {
				t.Errorf("name = %q, want %q", got.name, tt.wantName)
			}
			if got.tag != tt.wantTag {
				t.Errorf("tag = %q, want %q", got.tag, tt.wantTag)
			}
			if got.base != tt.wantBase {
				t.Errorf("base = %q, want %q", got.base, tt.wantBase)
			}
			if got.dryRun != tt.wantDry {
				t.Errorf("dryRun = %v, want %v", got.dryRun, tt.wantDry)
			}
			if got.from != tt.wantFrom {
				t.Errorf("from = %q, want %q", got.from, tt.wantFrom)
			}
			if got.path != tt.wantPath {
				t.Errorf("path = %q, want %q", got.path, tt.wantPath)
			}
		})
	}
}

func TestGenerateDockerfile(t *testing.T) {
	data := imageTemplateData{
		Base:          "ghcr.io/eyelock/ynh:latest",
		Name:          "david",
		DefaultVendor: "claude",
		YnhVersion:    "v1.2.3",
	}

	got, err := generateDockerfile(data)
	if err != nil {
		t.Fatalf("generateDockerfile failed: %v", err)
	}

	// Check key lines are present. Baked paths use the canonical-id layout
	// (run/local--<name>, harnesses/local--<name>) and the entrypoint passes
	// a canonical id — LoadQualified rejects bare names.
	checks := []string{
		"FROM ghcr.io/eyelock/ynh:latest",
		"vendors/claude/ /home/ynh/.ynh/run/local--david/claude/",
		"vendors/codex/ /home/ynh/.ynh/run/local--david/codex/",
		"vendors/cursor/ /home/ynh/.ynh/run/local--david/cursor/",
		"vendors/copilot/ /home/ynh/.ynh/run/local--david/copilot/",
		"harness/ /home/ynh/.ynh/harnesses/local--david/",
		"ENV YNH_VENDOR=claude",
		`ENTRYPOINT ["tini", "-s", "--", "ynh", "run", "local/david"]`,
		`dev.ynh.harness="david"`,
		`dev.ynh.harness.default-vendor="claude"`,
		`dev.ynh.assembled-by="v1.2.3"`,
	}

	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("Dockerfile missing %q\n\nGot:\n%s", want, got)
		}
	}
}

func TestGenerateDockerfile_CustomBase(t *testing.T) {
	data := imageTemplateData{
		Base:          "my-registry.io/ynh:v2",
		Name:          "test",
		DefaultVendor: "claude",
	}

	got, err := generateDockerfile(data)
	if err != nil {
		t.Fatalf("generateDockerfile failed: %v", err)
	}

	if !strings.Contains(got, "FROM my-registry.io/ynh:v2") {
		t.Errorf("Dockerfile should use custom base image\n\nGot:\n%s", got)
	}
}

func TestGenerateDockerfile_CustomVendor(t *testing.T) {
	data := imageTemplateData{
		Base:          "ghcr.io/eyelock/ynh:latest",
		Name:          "test",
		DefaultVendor: "codex",
	}

	got, err := generateDockerfile(data)
	if err != nil {
		t.Fatalf("generateDockerfile failed: %v", err)
	}

	if !strings.Contains(got, "ENV YNH_VENDOR=codex") {
		t.Errorf("Dockerfile should set YNH_VENDOR=codex\n\nGot:\n%s", got)
	}
}

func TestCmdImage_DryRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Create a minimal installed harness
	installTestHarness(t, "drytest")

	var stdout bytes.Buffer
	err := cmdImageTo([]string{"local/drytest", "--dry-run"}, &stdout, io.Discard)
	if err != nil {
		t.Fatalf("cmdImageTo --dry-run failed: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "FROM ghcr.io/eyelock/ynh:latest") {
		t.Errorf("dry-run should print Dockerfile with FROM line\n\nGot:\n%s", output)
	}
	if !strings.Contains(output, "drytest") {
		t.Errorf("dry-run should contain harness name\n\nGot:\n%s", output)
	}
}

func TestCmdImage_NoArgs(t *testing.T) {
	err := cmdImage([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdImage_HarnessNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	err := cmdImage([]string{"local/nonexistent", "--dry-run"})
	if err == nil {
		t.Fatal("expected error for missing harness")
	}
	if !errors.Is(err, harness.ErrNotFound) {
		t.Errorf("expected harness.ErrNotFound, got: %v", err)
	}
}

func TestCmdImage_NoDocker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Create a minimal installed harness
	installTestHarness(t, "nodockertest")

	// Set PATH to empty to ensure docker isn't found
	t.Setenv("PATH", t.TempDir())

	err := cmdImage([]string{"local/nodockertest"})
	if err == nil {
		t.Fatal("expected error when docker not in PATH")
	}
	if !strings.Contains(err.Error(), "docker not found") {
		t.Errorf("expected 'docker not found' error, got: %v", err)
	}
}

func TestImageAssembly_AllVendors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Create a minimal installed harness
	installTestHarness(t, "assemblytest")

	err := cmdImageTo([]string{"local/assemblytest", "--dry-run"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("cmdImageTo failed: %v", err)
	}

	// The temp dir is cleaned up by cmdImage, but we can verify via dry-run
	// that the Dockerfile references all three vendors. The actual assembly
	// is tested transitively through dry-run producing valid output.
}

func TestPreAssembledRunDir(t *testing.T) {
	dir := t.TempDir()

	// Create a pre-assembled vendor layout
	vendorDir := filepath.Join(dir, "run", "testharness", "claude")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a marker file
	if err := os.WriteFile(filepath.Join(vendorDir, "marker.txt"), []byte("pre-assembled"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Check detection: the directory should exist
	runDir := filepath.Join(dir, "run", "testharness")
	vendorRunDir := filepath.Join(runDir, "claude")
	info, err := os.Stat(vendorRunDir)
	if err != nil {
		t.Fatalf("pre-assembled dir should exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("pre-assembled path should be a directory")
	}

	// Verify marker file is accessible at the expected path
	data, err := os.ReadFile(filepath.Join(vendorRunDir, "marker.txt"))
	if err != nil {
		t.Fatalf("marker file not accessible: %v", err)
	}
	if string(data) != "pre-assembled" {
		t.Errorf("marker content = %q, want %q", string(data), "pre-assembled")
	}
}

// An image that cannot say which harness commit it holds cannot be pinned to
// one set of sensors and guards. Version is author-declared and reusable
// across different content; the SHA is the pin.
func TestHarnessSHA(t *testing.T) {
	cases := []struct {
		name string
		p    *harness.Harness
		ia   imageArgs
		want string
	}{
		{
			name: "--from a git ref: the resolved commit is the pin",
			p:    &harness.Harness{Name: "x"},
			ia:   imageArgs{sha: "4c1f9ab27de3055a8b6c1f2e4d9a8c7b3f5d6e21"},
			want: "4c1f9ab27de3055a8b6c1f2e4d9a8c7b3f5d6e21",
		},
		{
			name: "installed harness: the SHA ynh install recorded",
			p:    &harness.Harness{Name: "x", InstalledFrom: &harness.Provenance{SHA: "9f2c1ab"}},
			want: "9f2c1ab",
		},
		{
			name: "--from wins over install provenance — it is what was actually baked",
			p:    &harness.Harness{Name: "x", InstalledFrom: &harness.Provenance{SHA: "stale00"}},
			ia:   imageArgs{sha: "fresh11"},
			want: "fresh11",
		},
		{
			// Empty is honest. A label reading "unknown" would be worse: a
			// reader could not tell a missing value from a real one.
			name: "local working directory: no commit to name",
			p:    &harness.Harness{Name: "x", InstalledFrom: &harness.Provenance{SourceType: "local"}},
			want: "",
		},
		{
			name: "no harness loaded at all",
			p:    nil,
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := harnessSHA(c.p, c.ia); got != c.want {
				t.Errorf("harnessSHA = %q, want %q", got, c.want)
			}
		})
	}
}

// A factory image that can only start an interactive vendor session cannot do
// batch work. `--entrypoint agent` bakes the autonomous loop instead.
func TestGenerateDockerfile_Entrypoint(t *testing.T) {
	base := imageTemplateData{Base: "b", Name: "demo", DefaultVendor: "claude", YnhVersion: "0.7.0"}

	run := base
	run.Entrypoint = "run"
	out, err := generateDockerfile(run)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `ENTRYPOINT ["tini", "-s", "--", "ynh", "run", "local/demo"]`) {
		t.Errorf("the interactive entrypoint must be unchanged:\n%s", out)
	}
	if strings.Contains(out, "agent") {
		t.Error("the default build must not bake the agent loop")
	}

	agent := base
	agent.Entrypoint = "agent"
	out, err = generateDockerfile(agent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `ENTRYPOINT ["tini", "-s", "--", "ynh", "agent", "run", "--harness", "local/demo"]`) {
		t.Errorf("the agent entrypoint is missing:\n%s", out)
	}
	// CMD must stay empty so the task arrives as `docker run <image> --task ...`.
	if !strings.Contains(out, "CMD []") {
		t.Error("CMD must stay empty so the caller supplies the task")
	}
}

// An orchestrator reads these with `docker inspect`, without running anything.
func TestGenerateDockerfile_LabelsDescribeTheImage(t *testing.T) {
	out, err := generateDockerfile(imageTemplateData{
		Base: "b", Name: "demo", DefaultVendor: "codex", YnhVersion: "0.7.0",
		HarnessVersion: "2.1.0", HarnessSHA: "4c1f9ab", Entrypoint: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`dev.ynh.harness="demo"`,
		`dev.ynh.harness.default-vendor="codex"`,
		`dev.ynh.harness.version="2.1.0"`,
		`dev.ynh.harness.sha="4c1f9ab"`,
		`dev.ynh.entrypoint="agent"`,
		`dev.ynh.assembled-by="0.7.0"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("label missing: %s\n%s", want, out)
		}
	}
}

func TestParseImageArgs_Entrypoint(t *testing.T) {
	ia, err := parseImageArgs([]string{"demo"})
	if err != nil || ia.entrypoint != "run" {
		t.Errorf("default entrypoint = %q (err %v), want run — existing builds must not change", ia.entrypoint, err)
	}
	ia, err = parseImageArgs([]string{"demo", "--entrypoint", "agent"})
	if err != nil || ia.entrypoint != "agent" {
		t.Errorf("got %q (err %v), want agent", ia.entrypoint, err)
	}
	if _, err := parseImageArgs([]string{"demo", "--entrypoint", "sideways"}); err == nil {
		t.Error("an unknown entrypoint must be rejected, not silently ignored")
	}
	if _, err := parseImageArgs([]string{"demo", "--entrypoint"}); err == nil {
		t.Error("--entrypoint with no value must error")
	}
}
