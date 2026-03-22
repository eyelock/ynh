package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/persona"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// imageTemplateData holds the data passed to the persona Dockerfile template.
type imageTemplateData struct {
	Base          string // e.g. ghcr.io/eyelock/ynh:latest
	Name          string // persona name
	DefaultVendor string // e.g. "claude"
	YnhVersion    string // version of ynh that assembled the image
}

// imageDockerfileTmpl is the Dockerfile template for persona images.
// It layers pre-assembled vendor layouts on top of the base ynh image.
var imageDockerfileTmpl = template.Must(template.New("Dockerfile").Parse(`FROM {{.Base}}

# Pre-assembled vendor layouts (all three, ready to use)
COPY --link --chown=ynh:ynh vendors/claude/ /home/ynh/.ynh/run/{{.Name}}/claude/
COPY --link --chown=ynh:ynh vendors/codex/ /home/ynh/.ynh/run/{{.Name}}/codex/
COPY --link --chown=ynh:ynh vendors/cursor/ /home/ynh/.ynh/run/{{.Name}}/cursor/

# Persona source (metadata for ynh run)
COPY --link --chown=ynh:ynh persona/ /home/ynh/.ynh/personas/{{.Name}}/

# Default vendor (override: docker run -e YNH_VENDOR=codex)
ENV YNH_VENDOR={{.DefaultVendor}}

# Baked entrypoint — just pass the prompt as CMD
ENTRYPOINT ["tini", "-s", "--", "ynh", "run", "{{.Name}}"]
CMD []

LABEL dev.ynh.persona="{{.Name}}" \
      dev.ynh.persona.default-vendor="{{.DefaultVendor}}" \
      dev.ynh.assembled-by="{{.YnhVersion}}"
`))

// imageArgs holds parsed flags for the image command.
type imageArgs struct {
	name   string
	tag    string
	base   string
	dryRun bool
	from   string
	path   string
}

// parseImageArgs parses flags for the image command.
func parseImageArgs(args []string) (imageArgs, error) {
	var ia imageArgs
	ia.base = "ghcr.io/eyelock/ynh:latest"

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
		return ia, fmt.Errorf("usage: ynh image <name> [--tag <tag>] [--base <image>] [--from <source>] [--path <subdir>] [--dry-run]")
	}
	ia.name = remaining[0]

	if ia.tag == "" {
		ia.tag = "ynh-" + ia.name + ":latest"
	}

	return ia, nil
}

// generateDockerfile renders the persona Dockerfile template.
func generateDockerfile(data imageTemplateData) (string, error) {
	var buf bytes.Buffer
	if err := imageDockerfileTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering Dockerfile template: %w", err)
	}
	return buf.String(), nil
}

// cmdImage builds a Docker image with a persona baked in.
func cmdImage(args []string) error {
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

	// Load persona
	var p *persona.Persona
	var personaSrcDir string

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
			personaSrcDir = absPath
		} else {
			if err := cfg.CheckRemoteSource(source); err != nil {
				return err
			}
			result, err := resolver.EnsureRepo(source, "")
			if err != nil {
				return fmt.Errorf("resolving %s: %w", source, err)
			}
			personaSrcDir = result.Path
		}

		if pathFlag != "" {
			personaSrcDir = filepath.Join(personaSrcDir, pathFlag)
			if _, err := os.Stat(personaSrcDir); os.IsNotExist(err) {
				return fmt.Errorf("path %q not found in source", pathFlag)
			}
		}

		p, err = persona.LoadPluginDir(personaSrcDir)
		if err != nil {
			return fmt.Errorf("loading persona from source: %w", err)
		}
	} else {
		// Load from installed personas
		p, err = persona.Load(ia.name)
		if err != nil {
			return fmt.Errorf("persona %q not found: %w", ia.name, err)
		}
		personaSrcDir = persona.InstalledDir(ia.name)
	}

	// Create temp build context
	tmpDir, err := os.MkdirTemp("", "ynh-image-*")
	if err != nil {
		return fmt.Errorf("creating build context: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Copy persona source into build context
	personaDst := filepath.Join(tmpDir, "persona")
	if err := assembler.CopyDir(personaSrcDir, personaDst); err != nil {
		return fmt.Errorf("copying persona source: %w", err)
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
		BasePath: personaSrcDir,
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
		Base:          ia.base,
		Name:          p.Name,
		DefaultVendor: defaultVendor,
		YnhVersion:    config.Version,
	}

	dockerfile, err := generateDockerfile(data)
	if err != nil {
		return err
	}

	if ia.dryRun {
		fmt.Print(dockerfile)
		return nil
	}

	// Write Dockerfile to build context
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return fmt.Errorf("writing Dockerfile: %w", err)
	}

	// Verify docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH: install Docker to build persona images")
	}

	// Build image
	fmt.Fprintf(os.Stderr, "Building persona image %s...\n", ia.tag)
	cmd := exec.Command("docker", "build", "-t", ia.tag, tmpDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nPersona image built: %s\n", ia.tag)
	fmt.Fprintf(os.Stderr, "Run: docker run --rm -v $(pwd):/workspace -e ANTHROPIC_API_KEY %s -- \"your prompt\"\n", ia.tag)
	return nil
}
