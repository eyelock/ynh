package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// stubBaseLabels makes dockerImageLabels answer with the given labels JSON, or
// fail when labels is empty, for the duration of the test.
func stubBaseLabels(t *testing.T, labels string) {
	t.Helper()
	orig := dockerImageLabels
	t.Cleanup(func() { dockerImageLabels = orig })
	dockerImageLabels = func(image string) ([]byte, error) {
		if labels == "" {
			return nil, fmt.Errorf("no such image: %s", image)
		}
		return []byte(labels), nil
	}
}

func TestBaseVersionWarning(t *testing.T) {
	tests := []struct {
		name    string
		labels  string // "" means the image is not local
		builder string
		warn    bool
	}{
		{"older base warns", `{"dev.ynh.version":"0.7.0"}`, "0.8.0", true},
		{"same version is silent", `{"dev.ynh.version":"0.7.0"}`, "0.7.0", false},
		{"v prefix matches", `{"dev.ynh.version":"0.7.0"}`, "v0.7.0", false},
		{"newer base is silent", `{"dev.ynh.version":"0.9.0"}`, "0.8.0", false},
		{"dev build against a release warns", `{"dev.ynh.version":"0.7.0"}`, "dev-feat-x-abc1234-dirty", true},
		{"plain dev build warns", `{"dev.ynh.version":"0.7.0"}`, "dev", true},
		{"pre-release builder is newer than older release", `{"dev.ynh.version":"0.7.0"}`, "0.8.0-rc.1", true},
		{"release base is newer than its pre-release", `{"dev.ynh.version":"0.8.0"}`, "0.8.0-rc.1", false},
		{"OCI label is the fallback", `{"org.opencontainers.image.version":"0.7.0"}`, "0.8.0", true},
		{"image not local is silent", "", "0.8.0", false},
		{"no version label is silent", `{"other":"x"}`, "0.8.0", false},
		{"null labels are silent", `null`, "0.8.0", false},
		{"unparseable labels are silent", `not json`, "0.8.0", false},
		{"dev base is silent", `{"dev.ynh.version":"dev"}`, "0.8.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubBaseLabels(t, tt.labels)
			got := baseVersionWarning("ghcr.io/eyelock/ynh:latest", tt.builder)
			if (got != "") != tt.warn {
				t.Fatalf("warning = %q, want warn=%v", got, tt.warn)
			}
			if tt.warn {
				for _, want := range []string{"ghcr.io/eyelock/ynh:latest", "carries ynh 0.7.0", tt.builder, ".agents/harness/", "--base <image>"} {
					if !strings.Contains(got, want) {
						t.Errorf("warning missing %q:\n%s", want, got)
					}
				}
			}
		})
	}
}

func TestParseRelease(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{"1.2.3", true},
		{"v1.2.3", true},
		{"1.2.3-rc.1", true},
		{"1.2.3+build.5", true},
		{"dev", false},
		{"1.2", false},
		{"1.2.x", false},
		{"1.-2.3", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if _, ok := parseRelease(tt.in); ok != tt.ok {
				t.Errorf("parseRelease(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
		})
	}
}

func TestCompareRelease(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.7.0", "0.8.0", -1},
		{"0.10.0", "0.9.9", 1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-rc.1", "1.0.0-rc.2", -1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			a, _ := parseRelease(tt.a)
			b, _ := parseRelease(tt.b)
			if got := compareRelease(a, b); got != tt.want {
				t.Errorf("compareRelease(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestCmdImage_BaseVersionWarning checks the warning reaches stderr in a dry
// run, and that it never changes the Dockerfile on stdout.
func TestCmdImage_BaseVersionWarning(t *testing.T) {
	tests := []struct {
		name   string
		labels string
		warn   bool
	}{
		{"older base warns in a dry run", `{"dev.ynh.version":"0.0.1"}`, true},
		{"base not local is silent in a dry run", "", false},
	}
	var dockerfiles []string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			installTestHarness(t, "warntest")
			stubBaseLabels(t, tt.labels)

			var stdout, stderr bytes.Buffer
			if err := cmdImageTo([]string{"local/warntest", "--dry-run"}, &stdout, &stderr); err != nil {
				t.Fatalf("cmdImageTo --dry-run failed: %v", err)
			}
			// config.Version is "dev" under test, newer than any release.
			if got := strings.Contains(stderr.String(), "carries ynh 0.0.1"); got != tt.warn {
				t.Errorf("warning on stderr = %v, want %v\n%s", got, tt.warn, stderr.String())
			}
			if strings.Contains(stdout.String(), "warning") {
				t.Errorf("warning leaked into the Dockerfile:\n%s", stdout.String())
			}
			dockerfiles = append(dockerfiles, stdout.String())
		})
	}
	if len(dockerfiles) == 2 && dockerfiles[0] != dockerfiles[1] {
		t.Error("the warning changed the generated Dockerfile")
	}
}
