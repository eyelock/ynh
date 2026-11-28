//go:build unix

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withImageLabels(t *testing.T, labels map[string]string) {
	t.Helper()
	old := dockerImageLabels
	t.Cleanup(func() { dockerImageLabels = old })
	dockerImageLabels = func(string) ([]byte, error) { return json.Marshal(labels) }
}

func runTrustShowImage(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errb strings.Builder
	err := cmdTrustShow(args, &out, &errb)
	return out.String(), errb.String(), err
}

// The labels print without a container; a label the image lacks is "not set".
func TestTrustShowImage_PrintsLabelsAndNotSet(t *testing.T) {
	withImageLabels(t, map[string]string{
		"dev.ynh.harness": "reviewer", "dev.ynh.harness.trust-digest": "sha256:abcd", "dev.ynh.harness.sha": "3f4b",
	})
	out, _, err := runTrustShowImage(t, "--image", "acme/reviewer:1.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acme/reviewer:1.0", "harness:       reviewer", "trust digest:  sha256:abcd", "trust via:     not set", "source:        not set", "commit:        3f4b"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q not in:\n%s", want, out)
		}
	}
}

func TestTrustShowImage_JSONHasNullForMissingLabels(t *testing.T) {
	withImageLabels(t, map[string]string{"dev.ynh.harness": "reviewer", "dev.ynh.harness.trust-digest": "sha256:abcd"})
	out, _, err := runTrustShowImage(t, "--image", "acme/reviewer:1.0", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	labels := got["labels"].(map[string]any)
	if labels["harness"] != "reviewer" || labels["trust_digest"] != "sha256:abcd" || labels["trust_via"] != nil || labels["source"] != nil || labels["sha"] != nil {
		t.Errorf("labels = %v", labels)
	}
	if _, has := got["trust"]; has {
		t.Error("trust is present without --full")
	}
}

// Without --full nothing runs: a docker that fails on `run` is never asked to.
func TestTrustShowImage_NeverStartsAContainerWithoutFull(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\ncase \"$1\" in\n  image) echo '{\"dev.ynh.harness\":\"reviewer\"}' ;;\n  run) echo 'docker run must not be called' >&2; exit 9 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	useRealDockerLabels(t)
	if _, _, err := runTrustShowImage(t, "--image", "acme/reviewer:1.0"); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "run") {
		t.Errorf("docker was asked to run: %s", calls)
	}
	if !strings.Contains(string(calls), "image inspect") {
		t.Errorf("docker calls = %s, want image inspect", calls)
	}
}

// --full runs the image's own ynh with no network and no mounts, and lists
// what it executes, a baked a2a delegate included.
func TestTrustShowImage_FullRunsTheImagesOwnYnhWithNoNetworkAndNoMounts(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	trust := `{"harness":"local/reviewer","digest":"sha256:abcd","executables":[{"sensor":"lint","field":"command","value":"make lint"}],"state":"accepted","accepted_via":"image","auto_approve":"edits","auto_approve_from":"harness"}`
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\ncase \"$1\" in\n  image) echo '{\"dev.ynh.harness\":\"reviewer\",\"dev.ynh.harness.trust-digest\":\"sha256:abcd\"}' ;;\n  run) echo '" + trust + "' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	useRealDockerLabels(t)
	out, _, err := runTrustShowImage(t, "--image", "acme/reviewer:1.0", "--full")
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{"--network none", "--entrypoint ynh", "--pull never", "trust show local/reviewer --format json"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("%q not in docker's calls: %s", want, calls)
		}
	}
	for _, bad := range []string{" -v ", "--volume", "--mount"} {
		if strings.Contains(string(calls), bad) {
			t.Errorf("docker run has a mount (%s): %s", bad, calls)
		}
	}
	if !strings.Contains(out, "make lint") || !strings.Contains(out, "What the image executes") {
		t.Errorf("--full did not list what the image executes:\n%s", out)
	}
	// And as JSON the image's own block is carried.
	jout, _, err := runTrustShowImage(t, "--image", "acme/reviewer:1.0", "--full", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Trust struct {
			Harness string `json:"harness"`
			State   string `json:"state"`
		} `json:"trust"`
	}
	if err := json.Unmarshal([]byte(jout), &rep); err != nil || rep.Trust.Harness != "local/reviewer" || rep.Trust.State != "accepted" {
		t.Errorf("json = %s (%v)", jout, err)
	}
}

func TestTrustShowImage_Refusals(t *testing.T) {
	withImageLabels(t, map[string]string{"org.opencontainers.image.version": "1"})
	if _, _, err := runTrustShowImage(t, "--full"); err == nil || !strings.Contains(err.Error(), "--image") {
		t.Errorf("--full alone: %v", err)
	}
	if _, _, err := runTrustShowImage(t, "--image", "x", "--full"); err == nil || !strings.Contains(err.Error(), "dev.ynh.harness") {
		t.Errorf("a non-harness image: %v", err)
	}
	if _, _, err := runTrustShowImage(t, "local/h", "--image", "x"); err == nil {
		t.Error("an image and a harness name together")
	}
	old := dockerImageLabels
	t.Cleanup(func() { dockerImageLabels = old })
	dockerImageLabels = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	if _, _, err := runTrustShowImage(t, "--image", "absent:1"); err == nil || !strings.Contains(err.Error(), "never pulls") {
		t.Errorf("an absent image: %v", err)
	}
}

// useRealDockerLabels puts back the code that runs `docker image inspect`, for
// a test that stands a fake docker on PATH.
func useRealDockerLabels(t *testing.T) {
	t.Helper()
	old := dockerImageLabels
	t.Cleanup(func() { dockerImageLabels = old })
	dockerImageLabels = execDockerImageLabels
}
