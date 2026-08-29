package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// imageTemplateData holds the data passed to the harness Dockerfile template.
type imageTemplateData struct {
	Base          string // e.g. ghcr.io/eyelock/ynh:latest
	Name          string // harness name
	DefaultVendor string // e.g. "claude"
	YnhVersion    string // version of ynh that assembled the image
	// HarnessVersion and HarnessSHA make the image self-describing. Version is
	// an author-declared string that can be reused across different content;
	// the SHA is what pins the image to one set of sensors and guards. An
	// orchestrator reads both with `docker inspect`, without running anything.
	HarnessVersion string
	HarnessSHA     string
	// Entrypoint is "run" (interactive vendor session) or "agent" (the
	// autonomous loop). A factory image that can only start an interactive
	// session cannot do batch work.
	Entrypoint string
}

// imageDockerfileTmpl is the Dockerfile template for harness images.
// It layers pre-assembled vendor layouts on top of the base ynh image.
//
// All baked paths use the canonical-id layout: the harness source lands at
// the schema-2 install path (harnesses/local--<name>) and vendor layouts at
// the id-keyed run dirs (run/local--<name>/<vendor>), so the entrypoint's
// "ynh run local/<name>" resolves them directly. The entrypoint must be a
// canonical id — LoadQualified hard-rejects bare names.
var imageDockerfileTmpl = template.Must(template.New("Dockerfile").Parse(`FROM {{.Base}}

# Pre-assembled vendor layouts (all four, ready to use)
COPY --link --chown=ynh:ynh vendors/claude/ /home/ynh/.ynh/run/local--{{.Name}}/claude/
COPY --link --chown=ynh:ynh vendors/codex/ /home/ynh/.ynh/run/local--{{.Name}}/codex/
COPY --link --chown=ynh:ynh vendors/cursor/ /home/ynh/.ynh/run/local--{{.Name}}/cursor/
COPY --link --chown=ynh:ynh vendors/copilot/ /home/ynh/.ynh/run/local--{{.Name}}/copilot/

# Harness source (metadata for ynh run)
COPY --link --chown=ynh:ynh harness/ /home/ynh/.ynh/harnesses/local--{{.Name}}/

# Default vendor (override: docker run -e YNH_VENDOR=codex)
ENV YNH_VENDOR={{.DefaultVendor}}

{{if eq .Entrypoint "agent"}}# Autonomous loop. Pass the task as CMD:
#   docker run <image> --task "fix the failing test"
ENTRYPOINT ["tini", "-s", "--", "ynh", "agent", "run", "--harness", "local/{{.Name}}"]
{{else}}# Interactive vendor session. Pass the prompt as CMD.
ENTRYPOINT ["tini", "-s", "--", "ynh", "run", "local/{{.Name}}"]
{{end}}CMD []

LABEL dev.ynh.harness="{{.Name}}" \
      dev.ynh.harness.default-vendor="{{.DefaultVendor}}" \
      dev.ynh.harness.version="{{.HarnessVersion}}" \
      dev.ynh.harness.sha="{{.HarnessSHA}}" \
      dev.ynh.entrypoint="{{.Entrypoint}}" \
      dev.ynh.assembled-by="{{.YnhVersion}}"
`))

// imageArgs holds parsed flags for the image command.
type imageArgs struct {
	name       string
	tag        string
	base       string
	dryRun     bool
	from       string
	path       string
	entrypoint string
	// sha is the commit resolved from --from, filled in during the build
	// rather than parsed from a flag.
	sha string
}

// parseImageArgs parses flags for the image command.
func parseImageArgs(args []string) (imageArgs, error) {
	var ia imageArgs
	ia.base = "ghcr.io/eyelock/ynh:latest"
	ia.entrypoint = "run"

	var remaining []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--tag":
			if i+1 >= len(args) {
				return ia, fmt.Errorf("--tag requires a value")
			}
			i++
			ia.tag = args[i]
		case "--base":
			if i+1 >= len(args) {
				return ia, fmt.Errorf("--base requires a value")
			}
			i++
			ia.base = args[i]
		case "--entrypoint":
			if i+1 >= len(args) {
				return ia, fmt.Errorf("--entrypoint requires a value")
			}
			i++
			switch args[i] {
			case "run", "agent":
				ia.entrypoint = args[i]
			default:
				return ia, fmt.Errorf("unknown --entrypoint %q (want run or agent)", args[i])
			}
		case "--dry-run":
			ia.dryRun = true
		case "--from":
			if i+1 >= len(args) {
				return ia, fmt.Errorf("--from requires a value")
			}
			i++
			ia.from = args[i]
		case "--path":
			if i+1 >= len(args) {
				return ia, fmt.Errorf("--path requires a value")
			}
			i++
			ia.path = args[i]
		default:
			remaining = append(remaining, args[i])
		}
	}

	if len(remaining) < 1 {
		return ia, fmt.Errorf("usage: ynh image <name> [--tag <tag>] [--base <image>] [--from <source>] [--path <subdir>] [--entrypoint run|agent] [--dry-run]")
	}
	ia.name = remaining[0]

	if ia.tag == "" {
		ia.tag = "ynh-" + ia.name + ":latest"
	}

	return ia, nil
}

// generateDockerfile renders the harness Dockerfile template.
func generateDockerfile(data imageTemplateData) (string, error) {
	var buf bytes.Buffer
	if err := imageDockerfileTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering Dockerfile template: %w", err)
	}
	return buf.String(), nil
}

// cmdImage builds a Docker image with a harness baked in.
func cmdImage(args []string) error {
	return cmdImageTo(args, os.Stdout, os.Stderr)
}

// cmdImageTo is the testable core of cmdImage, writing output to the given writers.
func cmdImageTo(args []string, stdout, stderr io.Writer) error {
	ia, err := parseImageArgs(args)
	if err != nil {
		return err
	}

	if err := config.EnsureDirs(); err != nil {
		return err
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Load harness
	var p *harness.Harness
	var harnessSrcDir string

	if ia.from != "" {
		// Build from source (Git or local)
		resolved, err := resolveInstallSource(ia.from, ia.path, cfg)
		if err != nil {
			return fmt.Errorf("resolving source: %w", err)
		}

		source := ia.from
		pathFlag := ia.path
		if resolved.gitURL != "" {
			source = resolved.gitURL
			if resolved.path != "" {
				pathFlag = resolved.path
			}
		}

		if isLocalPath(source) {
			absPath, err := filepath.Abs(source)
			if err != nil {
				return err
			}
			harnessSrcDir = absPath
		} else {
			if err := cfg.CheckRemoteSource(source); err != nil {
				return err
			}
			result, err := resolver.EnsureRepo(source, resolved.ref)
			if err != nil {
				return fmt.Errorf("resolving %s: %w", source, err)
			}
			if err := verifyResolvedSHA(result.Path, resolved.sha); err != nil {
				return err
			}
			ia.sha = resolved.sha
			harnessSrcDir = result.Path
		}

		if pathFlag != "" {
			harnessSrcDir = filepath.Join(harnessSrcDir, pathFlag)
			if _, err := os.Stat(harnessSrcDir); os.IsNotExist(err) {
				return fmt.Errorf("path %q not found in source", pathFlag)
			}
		}

		p, err = harness.LoadDir(harnessSrcDir)
		if err != nil {
			return fmt.Errorf("loading harness from source: %w", err)
		}
	} else {
		// Load from installed harnesses
		p, err = harness.LoadQualified(ia.name)
		if err != nil {
			return err
		}
		harnessSrcDir = p.Dir
	}

	// Create temp build context
	tmpDir, err := os.MkdirTemp("", "ynh-image-*")
	if err != nil {
		return fmt.Errorf("creating build context: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Copy harness source into build context
	harnessDst := filepath.Join(tmpDir, "harness")
	if err := assembler.CopyDir(harnessSrcDir, harnessDst); err != nil {
		return fmt.Errorf("copying harness source: %w", err)
	}

	// Resolve includes (with remote source allow-list check)
	resolved, err := resolver.Resolve(p, cfg)
	if err != nil {
		return fmt.Errorf("resolving includes: %w", err)
	}

	// Check delegates against remote source allow-list
	for _, del := range p.DelegatesTo {
		if err := cfg.CheckRemoteSource(del.Git); err != nil {
			return fmt.Errorf("delegate %q: %w", del.Git, err)
		}
	}

	// Extract ResolvedContent
	var content []resolver.ResolvedContent
	for _, r := range resolved {
		content = append(content, r.Content)
	}
	localContent := resolver.ResolvedContent{
		BasePath: harnessSrcDir,
	}
	content = append(content, localContent)

	// Assemble vendor layouts for all vendors
	vendorsDir := filepath.Join(tmpDir, "vendors")
	for _, name := range vendor.Available() {
		adapter, err := vendor.Get(name)
		if err != nil {
			return fmt.Errorf("getting vendor %q: %w", name, err)
		}

		vendorDir := filepath.Join(vendorsDir, adapter.Name())
		if err := assembler.AssembleTo(vendorDir, adapter, content); err != nil {
			return fmt.Errorf("assembling %s layout: %w", adapter.Name(), err)
		}

		if err := assembler.AssembleDelegates(vendorDir, adapter, p.DelegatesTo); err != nil {
			return fmt.Errorf("assembling %s delegates: %w", adapter.Name(), err)
		}
	}

	// Determine default vendor
	defaultVendor := p.DefaultVendor
	if defaultVendor == "" {
		defaultVendor = "claude"
	}

	// Generate Dockerfile
	data := imageTemplateData{
		Base:           ia.base,
		Name:           p.Name,
		DefaultVendor:  defaultVendor,
		YnhVersion:     config.Version,
		HarnessVersion: p.Version,
		HarnessSHA:     harnessSHA(p, ia),
		Entrypoint:     ia.entrypoint,
	}

	dockerfile, err := generateDockerfile(data)
	if err != nil {
		return err
	}

	if ia.dryRun {
		_, _ = fmt.Fprint(stdout, dockerfile)
		return nil
	}

	// Write Dockerfile to build context
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return fmt.Errorf("writing Dockerfile: %w", err)
	}

	// Verify docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH: install Docker to build harness images")
	}

	// Build image
	_, _ = fmt.Fprintf(stderr, "Building harness image %s...\n", ia.tag)
	cmd := exec.Command("docker", "build", "-t", ia.tag, tmpDir)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	_, _ = fmt.Fprintf(stderr, "\nHarness image built: %s\n", ia.tag)
	_, _ = fmt.Fprintf(stderr, "Run: docker run --rm -v $(pwd):/workspace -e ANTHROPIC_API_KEY %s -- \"your prompt\"\n", ia.tag)
	return nil
}

// harnessSHA resolves the commit the baked harness came from.
//
// Two paths reach an image build. `--from <git>` resolves a ref to a SHA
// before cloning, and that SHA is the pin. An installed harness carries its
// own in installed.json, recorded by `ynh install`.
//
// Empty is honest and expected: a harness built from a local working directory
// has no commit to name, and a label reading "unknown" would be worse than an
// absent one, because a reader cannot tell a missing value from a real one.
func harnessSHA(p *harness.Harness, ia imageArgs) string {
	if ia.sha != "" {
		return ia.sha
	}
	if p != nil && p.InstalledFrom != nil {
		return p.InstalledFrom.SHA
	}
	return ""
}
