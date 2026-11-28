package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// `ynh trust show --image <ref>` reads the trust labels of an image on the host
// that would run it, without starting a container (ynh #1025, I2). The image's
// baked record is the build host's acceptance; the running host can verify it,
// never must. With --full it also runs the image's own ynh to list what the
// image executes, in a container with no network and no mounts.

// The labels an image carries about its trust (docs/docker.md).
const (
	imageLabelHarness     = "dev.ynh.harness"
	imageLabelTrustDigest = "dev.ynh.harness.trust-digest"
	imageLabelTrustVia    = "dev.ynh.harness.trust-via"
	imageLabelSource      = "dev.ynh.harness.source"
	imageLabelSHA         = "dev.ynh.harness.sha"
)

// imageLabelNotSet is how an absent label is shown in text: an image built
// before a label existed has none, which is not an error.
const imageLabelNotSet = "not set"

// trustImageLabels is the labels block of `trust show --image --format json`.
// A label the image does not carry is null.
type trustImageLabels struct {
	Harness     *string `json:"harness"`
	TrustDigest *string `json:"trust_digest"`
	TrustVia    *string `json:"trust_via"`
	Source      *string `json:"source"`
	SHA         *string `json:"sha"`
}

// trustImageReport is `ynh trust show --image <ref> --format json`.
type trustImageReport struct {
	Image  string           `json:"image"`
	Labels trustImageLabels `json:"labels"`
	// Trust is the image's own `ynh trust show local/<name>` block, with --full.
	Trust json.RawMessage `json:"trust,omitempty"`
}

// dockerRunTrustShow runs the image's own ynh to show the trust of the harness
// it carries: no network, no mounts, the entrypoint replaced, and never a pull.
// Replaceable so tests do not need docker.
var dockerRunTrustShow = func(image, harnessName string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--pull", "never", "--network", "none",
		"--entrypoint", "ynh", image, "trust", "show", "local/"+harnessName, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		msg := ""
		var ee *exec.ExitError
		if asExitError(err, &ee) {
			msg = ": " + strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("running the image's own ynh to show its trust: %w%s", err, msg)
	}
	return out, nil
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// labelPtr returns a pointer to the label's value, or nil when it is absent or
// empty.
func labelPtr(labels map[string]string, key string) *string {
	if v := strings.TrimSpace(labels[key]); v != "" {
		return &v
	}
	return nil
}

func labelText(p *string) string {
	if p == nil {
		return imageLabelNotSet
	}
	return *p
}

// cmdTrustShowImage is `ynh trust show --image <ref> [--full] [--format ...]`.
func cmdTrustShowImage(ref string, full bool, format string, structured bool, stdout, stderr io.Writer) error {
	raw, err := dockerImageLabels(ref)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound,
			fmt.Sprintf("%v\n  Fix: the image must be present locally; ynh trust show --image never pulls it", err))
	}
	var labels map[string]string
	if err := json.Unmarshal(raw, &labels); err != nil {
		return cliError(stderr, structured, errCodeIOError, fmt.Sprintf("reading the labels of %s: %v", ref, err))
	}
	rep := trustImageReport{Image: ref, Labels: trustImageLabels{
		Harness:     labelPtr(labels, imageLabelHarness),
		TrustDigest: labelPtr(labels, imageLabelTrustDigest),
		TrustVia:    labelPtr(labels, imageLabelTrustVia),
		Source:      labelPtr(labels, imageLabelSource),
		SHA:         labelPtr(labels, imageLabelSHA),
	}}

	var status trustStatus
	if full {
		if rep.Labels.Harness == nil {
			return cliError(stderr, structured, errCodeInvalidInput,
				fmt.Sprintf("%s carries no %s label, so it is not a harness image ynh built, and --full cannot name the harness to show", ref, imageLabelHarness))
		}
		out, err := dockerRunTrustShow(ref, *rep.Labels.Harness)
		if err != nil {
			return cliError(stderr, structured, errCodeIOError, err.Error())
		}
		if err := json.Unmarshal(out, &status); err != nil {
			return cliError(stderr, structured, errCodeIOError, fmt.Sprintf("reading the image's trust show output: %v", err))
		}
		rep.Trust = json.RawMessage(strings.TrimSpace(string(out)))
	}

	if format == "json" {
		return writeJSON(stdout, rep)
	}
	_, _ = fmt.Fprintf(stdout, "Image: %s\n", ref)
	_, _ = fmt.Fprintf(stdout, "  harness:       %s\n", labelText(rep.Labels.Harness))
	_, _ = fmt.Fprintf(stdout, "  trust digest:  %s\n", labelText(rep.Labels.TrustDigest))
	_, _ = fmt.Fprintf(stdout, "  trust via:     %s\n", labelText(rep.Labels.TrustVia))
	_, _ = fmt.Fprintf(stdout, "  source:        %s\n", labelText(rep.Labels.Source))
	_, _ = fmt.Fprintf(stdout, "  commit:        %s\n", labelText(rep.Labels.SHA))
	if !full {
		_, _ = fmt.Fprintln(stdout, "\nThese are the build host's labels, which the image's gate never reads. --full lists what the image executes.")
		return nil
	}
	_, _ = fmt.Fprintln(stdout, "\nWhat the image executes, from the image's own ynh (no network, no mounts):")
	return printTrustDetail(stdout, status)
}

// splitTrustImageArgs takes --image <ref> and --full out of args, leaving the
// rest for parseTrustArgs.
func splitTrustImageArgs(args []string) (rest []string, image string, full bool, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--image":
			if i+1 >= len(args) {
				return nil, "", false, fmt.Errorf("--image requires an image reference")
			}
			image = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--image="):
			image = strings.TrimPrefix(args[i], "--image=")
		case args[i] == "--full":
			full = true
		default:
			rest = append(rest, args[i])
		}
	}
	if full && image == "" {
		return nil, "", false, fmt.Errorf("--full is for an image: use ynh trust show --image <ref> --full")
	}
	return rest, image, full, nil
}
