package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
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
//
// The copies use --chown by user name and no --link: BuildKit cannot resolve a user name for a
// --link copy on some Docker versions ("invalid user index: -1" on 27.4), and a numeric id would be
// wrong for custom bases, whose uid is configurable.
var imageDockerfileTmpl = template.Must(template.New("Dockerfile").Parse(`FROM {{.Base}}

# Pre-assembled vendor layouts (all four, ready to use)
COPY --chown=ynh:ynh vendors/claude/ /home/ynh/.ynh/run/local--{{.Name}}/claude/
COPY --chown=ynh:ynh vendors/codex/ /home/ynh/.ynh/run/local--{{.Name}}/codex/
COPY --chown=ynh:ynh vendors/cursor/ /home/ynh/.ynh/run/local--{{.Name}}/cursor/
COPY --chown=ynh:ynh vendors/copilot/ /home/ynh/.ynh/run/local--{{.Name}}/copilot/

# Harness source (metadata for ynh run)
COPY --chown=ynh:ynh harness/ /home/ynh/.ynh/harnesses/local--{{.Name}}/

# Schema version of the layout above, so ynh does not try to migrate it, and
# the include cache (cache/), so included sensors are read from the build
COPY --chown=ynh:ynh home/ /home/ynh/.ynh/

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

// imageHome is ynh's home inside the image, where the Dockerfile template puts
// everything it bakes.
const imageHome = "/home/ynh/.ynh"

// imageHarnessDir is where the Dockerfile puts the harness source inside the
// image. It is the directory a relative include resolves against at runtime.
func imageHarnessDir(name string) string {
	return path.Join(imageHome, "harnesses", "local--"+name)
}

// profileOnlyIncludes returns the includes that profiles add to the harness's
// own, so a run with --profile finds them baked too. Profile includes only ever
// add to the base ones, so the base ones are dropped from each resolved profile.
func profileOnlyIncludes(p *harness.Harness) ([]harness.Include, error) {
	names := make([]string, 0, len(p.Profiles))
	for n := range p.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []harness.Include
	for _, n := range names {
		rp, err := harness.ResolveProfile(p, n)
		if err != nil {
			return nil, err
		}
		out = append(out, rp.Includes[len(p.Includes):]...)
	}
	return out, nil
}

// bakeIncludeCache copies the cached clone of every git include into
// homeDir/cache, under the name the image's own lookup will compute.
//
// The name is not the build-time one. The cache key is the source URL, and a
// relative source ("." or "../..") is joined to the directory of the harness
// that names it. In the image that directory is runtimeHarnessDir, so the key
// is computed from there. A remote URL is the same in both places.
//
// Every git include is baked, including one with "sensors": false. Its
// artifacts are already assembled, but a run still resolves every include
// through the cache and a miss there is a network fetch. Local includes need
// nothing: they are read from the harness directory, which is baked.
//
// Delegates are not baked: they are assembled into the vendor layouts at build
// time, and nothing reads them from the cache at runtime.
//
// The clone is copied with its .git, which assembler.CopyDir skips: the cache
// lookup requires it, and a shallow clone keeps it small.
func bakeIncludeCache(p *harness.Harness, includes []harness.Include, homeDir, runtimeHarnessDir string) error {
	b := &includeBaker{homeDir: homeDir, baked: map[string]string{}, seen: map[string]bool{}}
	return b.bake(p.Dir, runtimeHarnessDir, includes)
}

// includeBaker walks the includes of a harness and, through every included
// harness that follows its own includes, theirs: a run resolves them
// transitively, so an image that baked only the first level would fetch the
// rest at run time.
type includeBaker struct {
	homeDir string
	baked   map[string]string // cache entry name -> source clone
	seen    map[string]bool   // harness dirs already walked, which also stops a cycle
}

// bake copies the clones of includes, which belong to the harness at buildDir
// (runtimeDir is where that harness lives in the image), then descends into the
// included harnesses. An include that picks artifacts does not follow its own
// includes, so the walk does not either.
func (b *includeBaker) bake(buildDir, runtimeDir string, includes []harness.Include) error {
	for _, inc := range includes {
		var innerBuild, innerRuntime string
		switch {
		case inc.IsLocal():
			dir := inc.Local
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(buildDir, dir)
			}
			innerBuild = filepath.Join(dir, inc.Path)
			rel, err := filepath.Rel(buildDir, innerBuild)
			if err != nil || strings.HasPrefix(rel, "..") {
				// Outside the harness, so outside the baked copy: nothing to
				// walk into, and nothing the image could read.
				continue
			}
			innerRuntime = path.Join(runtimeDir, filepath.ToSlash(rel))
		case inc.Git != "":
			src, ok := resolver.LookupCache(resolver.GitSourceURL(inc.Git, buildDir), inc.Ref)
			if !ok {
				return fmt.Errorf("include %s is not in the cache after resolving", inc.Git)
			}
			name := resolver.CacheEntryName(resolver.GitSourceURL(inc.Git, runtimeDir), inc.Ref)
			if prev, dup := b.baked[name]; dup {
				if prev != src.Path {
					return fmt.Errorf("includes %s and %s would share one cache entry in the image", prev, src.Path)
				}
			} else {
				b.baked[name] = src.Path
				if err := copyTreeWithGit(src.Path, filepath.Join(b.homeDir, "cache", name)); err != nil {
					return fmt.Errorf("baking include %s: %w", inc.Git, err)
				}
			}
			innerBuild = filepath.Join(src.Path, inc.Path)
			innerRuntime = path.Join(imageHome, "cache", name, filepath.ToSlash(inc.Path))
		default:
			continue
		}
		if len(inc.Pick) > 0 || !harness.IsHarnessDir(innerBuild) || b.seen[innerBuild] {
			continue
		}
		b.seen[innerBuild] = true
		inner, err := harness.LoadDir(innerBuild)
		if err != nil {
			return fmt.Errorf("loading included harness %s: %w", innerBuild, err)
		}
		if err := b.bake(innerBuild, innerRuntime, inner.Includes); err != nil {
			return err
		}
	}
	return nil
}

// copyTreeWithGit copies src to dst, .git included. Files stay writable so the
// build context can be removed.
func copyTreeWithGit(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o755)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm()|0o200)
		}
		return nil
	})
}

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
			if err := cfg.CheckSource(source, ""); err != nil {
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
	if err := assembler.CopyTree(harnessSrcDir, harnessDst); err != nil {
		return fmt.Errorf("copying harness source: %w", err)
	}

	// Resolve includes (with remote source allow-list check)
	resolved, err := resolver.Resolve(p, cfg)
	if err != nil {
		return fmt.Errorf("resolving includes: %w", err)
	}

	// Check delegates against remote source allow-list
	for _, del := range p.DelegatesTo {
		if err := cfg.CheckSource(del.Git, p.Dir); err != nil {
			return fmt.Errorf("delegate %q: %w", del.Git, err)
		}
	}

	// Includes a profile adds are not in the resolve above. Fetch them too, so
	// they are in the cache to bake.
	profileIncs, err := profileOnlyIncludes(p)
	if err != nil {
		return err
	}
	if len(profileIncs) > 0 {
		if _, err := resolver.Resolve(&harness.Harness{Dir: p.Dir, Includes: profileIncs}, cfg); err != nil {
			return fmt.Errorf("resolving profile includes: %w", err)
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

		if _, err := assembler.AssembleDelegates(vendorDir, adapter, p.DelegatesTo, p.Dir, assembler.DelegateOptions{Config: cfg, RootName: p.Name}); err != nil {
			return fmt.Errorf("assembling %s delegates: %w", adapter.Name(), err)
		}
	}

	// Stamp the image's ynh home with the schema this ynh writes.
	homeDir := filepath.Join(tmpDir, "home")
	if err := migration.WriteSchemaVersion(homeDir, migration.CurrentSchemaVersion); err != nil {
		return fmt.Errorf("stamping schema version: %w", err)
	}

	// Bake the include cache, so included sensors are read from what the image
	// was built from and nothing is fetched at run time.
	if err := bakeIncludeCache(p, append(append([]harness.Include{}, p.Includes...), profileIncs...), homeDir, imageHarnessDir(p.Name)); err != nil {
		return err
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

	// The image runs the base's ynh, not this one. Read from a local image's
	// labels only, so this neither pulls nor runs anything, in a dry run too.
	if w := baseVersionWarning(ia.base, config.Version); w != "" {
		_, _ = fmt.Fprint(stderr, w)
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
