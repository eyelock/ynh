package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/warn"
)

// baseVersionLabels are the labels the published ynh image carries its version
// in (see the Dockerfile), most specific first.
var baseVersionLabels = []string{"dev.ynh.version", "org.opencontainers.image.version"}

// dockerImageLabels returns the labels of an image already present locally, as
// the JSON object `docker image inspect` prints. It never pulls and never runs
// a container: an image that is not local is an error, and so is a missing
// docker. Replaceable so tests do not need docker.
var dockerImageLabels = func(image string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{json .Config.Labels}}", image).Output()
	if err != nil {
		return nil, fmt.Errorf("inspecting %s: %w", image, err)
	}
	return out, nil
}

// baseYnhVersion reads the ynh version a base image carries from its labels.
// Empty means unknown: docker absent, image not pulled, or no version label.
func baseYnhVersion(image string) string {
	out, err := dockerImageLabels(image)
	if err != nil {
		return ""
	}
	var labels map[string]string
	if err := json.Unmarshal(out, &labels); err != nil {
		return ""
	}
	for _, key := range baseVersionLabels {
		if v := strings.TrimSpace(labels[key]); v != "" {
			return v
		}
	}
	return ""
}

// baseVersionWarning explains that the image will run an older ynh than the
// one building it, or returns "" when it will not or that cannot be told.
//
// A builder version that is not a release (a dev build such as
// "dev-feat-x-abc1234-dirty") counts as newer than every release, so building
// from a branch on a released base warns. A base version that is not a
// release is unknown and stays silent.
func baseVersionWarning(base, builder string) (warn.Warning, bool) {
	baseVer := baseYnhVersion(base)
	b, ok := parseRelease(baseVer)
	if !ok {
		return warn.Warning{}, false
	}
	if y, ok := parseRelease(builder); ok && compareRelease(b, y) >= 0 {
		return warn.Warning{}, false
	}
	return warn.Warning{
		Message: fmt.Sprintf("base image %s carries ynh %s, older than this ynh (%s). "+
			"The image will run ynh %s, which may not read this harness: older releases do not read "+
			"manifests in .agents/harness/.", base, baseVer, builder, baseVer),
		Fix: "pass --base <image> to build on a newer ynh image",
	}, true
}

// trustGateRelease is the first ynh release whose run gate checks a baked
// trust record. A base image older than this starts a baked harness with no
// check at all.
var trustGateRelease = release{core: [3]int{0, 18, 0}}

// baseTrustGateWarning explains that a base image's ynh predates the trust
// gate, so it runs the harness baked into the image with no check and no
// warning, or returns false when the base is new enough or its version cannot
// be told (a base that is not a local image, or whose version is not a
// release). It warns rather than refuses: the base is the builder's choice,
// and what the image bakes is what the builder just accepted.
func baseTrustGateWarning(base string) (warn.Warning, bool) {
	baseVer := baseYnhVersion(base)
	b, ok := parseRelease(baseVer)
	// A pre-release of 0.18.0 counts as the gate's release: compare the core only.
	if !ok || compareRelease(release{core: b.core}, trustGateRelease) >= 0 {
		return warn.Warning{}, false
	}
	return warn.Warning{
		Message: fmt.Sprintf("base image %s carries ynh %s, which predates the trust gate (ynh 0.18.0). "+
			"The image will run the baked harness's hooks, MCP servers and sensors with no trust check and no warning.", base, baseVer),
		Fix: "pass --base <image> to build on an image whose ynh is 0.18.0 or newer",
	}, true
}

// release is a parsed semantic version.
type release struct {
	core [3]int
	pre  string
}

// parseRelease parses "1.2.3", "v1.2.3" and "1.2.3-rc.1", ignoring any
// "+build" suffix. Anything else, such as "dev", is not a release.
func parseRelease(s string) (release, bool) {
	var r release
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	s, r.pre, _ = strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return r, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return r, false
		}
		r.core[i] = n
	}
	return r, true
}

// compareRelease returns -1, 0 or 1. A pre-release sorts before its release;
// two pre-releases of the same core compare as strings, which is enough to
// tell an older base from a newer one without full semver precedence.
func compareRelease(a, b release) int {
	for i := range a.core {
		if a.core[i] != b.core[i] {
			if a.core[i] < b.core[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}
