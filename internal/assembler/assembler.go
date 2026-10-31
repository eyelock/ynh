package assembler

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

// LayoutProvider describes the vendor layout information that the assembler
// needs. Consumers define their own narrow interface rather than depending on
// the full vendor.Adapter.
type LayoutProvider interface {
	// ConfigDir returns the vendor's config directory name (e.g. ".claude").
	ConfigDir() string
	// ArtifactDirs maps artifact types to their directory names within the config dir.
	ArtifactDirs() map[string]string
	// InstructionsFile returns the filename for project-level instructions.
	InstructionsFile() string
}

// ArtifactTransform rewrites a single artifact file's name and content before
// it is written to the target directory. Used by vendors whose plugin format
// differs from the raw source layout (e.g. Cursor's .mdc rules with frontmatter).
// An error refuses the artifact, and with it the assembly: a vendor returns
// one when the file cannot be carried over without losing what it says.
type ArtifactTransform func(artifactType, name string, data []byte) (string, []byte, error)

// ArtifactTransformer is an optional capability a LayoutProvider may implement
// to rewrite an artifact file's name/content during copy.
type ArtifactTransformer interface {
	TransformArtifact(artifactType, name string, data []byte) (string, []byte, error)
}

// ArtifactWarner is an optional capability an ArtifactTransformer may add: the
// warnings for what its transform of one artifact drops, such as the tools
// list of an agent Codex writes as a custom agent. Assembly prints each one.
type ArtifactWarner interface {
	ArtifactWarnings(artifactType, name string, data []byte) []string
}

// warnOut receives assembly's warnings. Tests swap it to read them.
var warnOut io.Writer = os.Stderr

// Assemble creates a temporary directory with vendor-specific config layout
// populated from resolved Git content.
func Assemble(adapter LayoutProvider, content []resolver.ResolvedContent) (string, error) {
	workDir, err := os.MkdirTemp("", "ynh-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	if err := assembleInto(workDir, adapter, content); err != nil {
		return "", err
	}
	return workDir, nil
}

// AssembleTo populates a specific directory with vendor-specific config layout.
// The directory is cleaned and recreated. Use this for deterministic paths that
// survive process replacement (syscall.Exec).
func AssembleTo(dir string, adapter LayoutProvider, content []resolver.ResolvedContent) error {
	// Clean previous run
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("cleaning run dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating run dir: %w", err)
	}

	return assembleInto(dir, adapter, content)
}

func assembleInto(workDir string, adapter LayoutProvider, content []resolver.ResolvedContent) error {
	configDir := filepath.Join(workDir, adapter.ConfigDir())
	artifactDirs := adapter.ArtifactDirs()

	// Create all artifact directories
	for _, dir := range artifactDirs {
		if err := os.MkdirAll(filepath.Join(configDir, dir), 0o755); err != nil {
			return err
		}
	}

	// Warnings are kept per file written, keyed by its path in the config
	// dir: a later source (the root after its includes) that writes the same
	// file replaces the earlier copy, and its warnings replace that copy's,
	// so each agent staged warns once, about the copy that was staged.
	var transform ArtifactTransform
	var warned []string               // target paths, in first-written order
	warnings := map[string][]string{} // target path -> that copy's warnings
	if t, ok := adapter.(ArtifactTransformer); ok {
		transform = t.TransformArtifact
		if w, ok := adapter.(ArtifactWarner); ok {
			transform = func(artifactType, name string, data []byte) (string, []byte, error) {
				newName, newData, err := t.TransformArtifact(artifactType, name, data)
				if err != nil {
					return newName, newData, err
				}
				key := artifactType + "/" + newName
				if _, seen := warnings[key]; !seen {
					warned = append(warned, key)
				}
				warnings[key] = w.ArtifactWarnings(artifactType, name, data)
				return newName, newData, nil
			}
		}
	}
	defer func() {
		for _, key := range warned {
			for _, w := range warnings[key] {
				_, _ = fmt.Fprintf(warnOut, "  warning: %s\n", w)
			}
		}
	}()

	// Copy content into the right places
	for _, rc := range content {
		if len(rc.Paths) == 0 {
			// No pick list - include everything that matches artifact types
			if err := CopyAllArtifacts(rc.BasePath, configDir, artifactDirs, transform); err != nil {
				return err
			}
		} else {
			for _, picked := range rc.Paths {
				if err := CopyPicked(rc.BasePath, picked, configDir, artifactDirs, transform); err != nil {
					return err
				}
			}
		}
	}

	// Compose the sources' instructions into the vendor-specific project
	// instructions file.
	instructionsFile := adapter.InstructionsFile()
	if instructionsFile != "" {
		if composed := ComposeInstructions(content); len(composed) > 0 {
			dst := filepath.Join(workDir, instructionsFile)
			if err := os.WriteFile(dst, composed, 0o644); err != nil {
				return fmt.Errorf("writing instructions: %w", err)
			}
		}
	}

	return nil
}

// Cleanup removes an assembled directory.
func Cleanup(workDir string) {
	_ = os.RemoveAll(workDir)
}

// CopyPicked copies a specific path from the repo into the right artifact directory.
// picked is like "skills/commit" or "agents/code-reviewer.md".
// targetBaseDir is where artifact type directories live (e.g., workDir/.claude/ for runtime,
// or pluginRoot/ for export).
func CopyPicked(repoBase string, picked string, targetBaseDir string, artifactDirs map[string]string, transform ArtifactTransform) error {
	// A folder at the include's top level whose SKILL.md is at its root is
	// a skill, picked by its name alone (harness.TopLevelSkill). It used to
	// be an error, so no pick that resolved before resolves differently.
	if harness.TopLevelSkill(repoBase, picked) {
		targetDir, ok := artifactDirs["skills"]
		if !ok {
			return fmt.Errorf("unknown artifact type %q in pick: %s", "skills", picked)
		}
		name := filepath.Clean(picked)
		return copyArtifactDir("skills", filepath.Join(repoBase, name), filepath.Join(targetBaseDir, targetDir, name))
	}

	// Determine which artifact type this belongs to
	parts := strings.SplitN(picked, "/", 2)
	if len(parts) < 2 {
		return fmt.Errorf("pick path must be in format 'type/name', or name a top-level folder with a SKILL.md at its root: %s", picked)
	}

	artifactType := parts[0]
	targetDir, ok := artifactDirs[artifactType]
	if !ok {
		return fmt.Errorf("unknown artifact type %q in pick: %s", artifactType, picked)
	}

	// parts[1] lands in the copy destination below, so a pick containing ".."
	// or an absolute path writes outside the target tree. Confirmed, not
	// theorised: with a deep source (a cached repo) and a shallow destination
	// (`ynd export -o ./out`), the two climbs are asymmetric and the write
	// escapes. It is only self-cancelling when both trees sit at equal depth.
	//
	// Guarded here, at the sink, because every ingestion route reaches it:
	// marketplace install, fork, compose, export and a hand-edited manifest.
	// harness.ValidatePicks is a stronger allowlist but runs only in
	// `ynh include`, so it does not cover a manifest that arrives any other
	// way. This mirrors the inline guard already on sensor Reference.Path.
	// Both halves are checked, because they feed different sinks and a pick
	// can be safe in one and not the other. `picked` builds the source under
	// repoBase; `parts[1]` builds the destination under targetBase/targetDir.
	// "skills/../outside" cleans to a local path, so checking only `picked`
	// misses it, while its parts[1] of "../outside" climbs out of the
	// artifact directory.
	for _, seg := range []string{filepath.FromSlash(picked), filepath.FromSlash(parts[1])} {
		if !filepath.IsLocal(seg) {
			return fmt.Errorf("pick %q must stay inside the harness and inside its "+
				"artifact directory: no %q segments, no leading separator", picked, "..")
		}
	}

	src := filepath.Join(repoBase, picked)

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("pick path not found: %s (%w)%s", picked, err, pickHint(repoBase, picked))
	}

	if info.IsDir() {
		return copyArtifactDir(artifactType, src, filepath.Join(targetBaseDir, targetDir, parts[1]))
	}
	return copyArtifactFile(artifactType, src, targetBaseDir, targetDir, parts[1], transform)
}

// pickMaxDepth bounds how far pickHint looks below the include root. Artifact
// trees are shallow; a deeper search only finds vendored copies.
const pickMaxDepth = 6

// pickHint explains a missing pick when the same-named entry exists elsewhere
// in the include, which is the usual cause: the include's `path` points at the
// wrong directory. It returns "" when there is nothing useful to say, and
// otherwise a leading newline followed by where the entry is and, when the
// pick is a suffix of that location, the --path that would make it resolve.
func pickHint(repoBase, picked string) string {
	sep := string(filepath.Separator)
	base := filepath.Base(filepath.FromSlash(picked))
	suffix := sep + filepath.FromSlash(picked)
	var first, suffixed string
	_ = filepath.WalkDir(repoBase, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(repoBase, p)
		if rerr != nil || rel == "." {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" ||
			strings.Count(rel, sep) >= pickMaxDepth) {
			return filepath.SkipDir
		}
		if d.Name() != base {
			return nil
		}
		if first == "" {
			first = rel
		}
		if suffixed == "" && strings.HasSuffix(sep+rel, suffix) {
			suffixed = rel
		}
		return nil
	})
	if first == "" {
		return ""
	}
	if suffixed != "" {
		path := strings.TrimPrefix(strings.TrimSuffix(sep+suffixed, suffix), sep)
		return fmt.Sprintf("\n%q exists at %q inside this include; if the include's path is wrong, run:\n"+
			"  ynh include update <harness> <url> --path %s", base, filepath.ToSlash(suffixed), filepath.ToSlash(path))
	}
	return fmt.Sprintf("\n%q exists at %q inside this include; the pick must be relative to the include's path",
		base, filepath.ToSlash(first))
}

// CopyAllArtifacts scans the repo for known artifact type directories and copies them.
// targetBaseDir is where artifact type directories live (e.g., workDir/.claude/ for runtime,
// or pluginRoot/ for export).
func CopyAllArtifacts(repoBase string, targetBaseDir string, artifactDirs map[string]string, transform ArtifactTransform) error {
	for artifactType, targetDir := range artifactDirs {
		srcDir := filepath.Join(repoBase, artifactType)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}

		entries, err := os.ReadDir(srcDir)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			src := filepath.Join(srcDir, entry.Name())

			if entry.IsDir() {
				if err := copyArtifactDir(artifactType, src, filepath.Join(targetBaseDir, targetDir, entry.Name())); err != nil {
					return err
				}
			} else {
				if err := copyArtifactFile(artifactType, src, targetBaseDir, targetDir, entry.Name(), transform); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// copyArtifactFile copies a single artifact file, applying transform (if non-nil)
// to rewrite its name/content before writing.
func copyArtifactFile(artifactType, src, targetBaseDir, targetDir, name string, transform ArtifactTransform) error {
	if transform == nil {
		return CopyFile(src, filepath.Join(targetBaseDir, targetDir, name))
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	newName, newData, err := transform(artifactType, name, data)
	if err != nil {
		return fmt.Errorf("%s/%s: %w", artifactType, name, err)
	}
	dst := filepath.Join(targetBaseDir, targetDir, newName)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, newData, info.Mode())
}

// CopyFile copies a single file from src to dst, creating parent directories as needed.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}

// CopyDir recursively copies src to dst, skipping .git directories.
//
// A symlink is recreated as a symlink with its link text preserved, whether it
// points at a file, a directory or nothing. It is never followed, so a link
// out of the tree cannot pull content from outside it, and a dangling link is
// not an error. This is what a pnpm or npm workspace needs: its node_modules
// is full of links to directories (#599).
//
// A link is preserved verbatim, so a relative link that climbs out of the
// copied directory will dangle at the destination. Use this for copies of a
// harness source. Output that must be self-contained (vendor layouts,
// marketplace plugins, preview output) uses CopyDirDeref instead.
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return copyEntry(path, target, d.Type())
	})
}

// CopyDirDeref recursively copies src to dst, skipping .git directories, and
// follows every symlink: a file link is copied as its target's contents and a
// link to a directory as that directory's contents. The result is
// self-contained, with no link left to dangle.
//
// A link that resolves to one of its own ancestors is refused with an error
// naming the link, rather than recursed into forever. A dangling link is an
// error naming it: the output cannot be made self-contained without the
// target, and the previous behaviour was already a hard failure.
func CopyDirDeref(src, dst string) error {
	return copyDirDerefSkipping(src, dst)
}

// skillAuthoringDirs are directories directly inside a skill that are
// authoring-time material in the Agent Skills layout: only SKILL.md and the
// files it references are used at runtime, so assembly and export leave them
// behind.
var skillAuthoringDirs = []string{"evals"}

// copyArtifactDir copies one artifact directory. A skill is copied without
// its authoring-time directories (see skillAuthoringDirs); any other artifact
// directory is copied whole.
func copyArtifactDir(artifactType, src, dst string) error {
	if artifactType == "skills" {
		return copyDirDerefSkipping(src, dst, skillAuthoringDirs...)
	}
	return CopyDirDeref(src, dst)
}

// copyDirDerefSkipping is CopyDirDeref that leaves out the named directories
// directly inside src, and only there: a deeper directory of the same name is
// kept.
func copyDirDerefSkipping(src, dst string, skipTop ...string) error {
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	return copyDeref(src, dst, []string{real}, skipTop)
}

func copyDeref(src, dst string, ancestors []string, skipTop []string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		isLink := e.Type()&fs.ModeSymlink != 0
		if e.IsDir() && e.Name() == ".git" {
			continue
		}
		if slices.Contains(skipTop, e.Name()) {
			if skipInfo, err := os.Stat(from); err == nil && skipInfo.IsDir() {
				continue
			}
		}
		info, err := os.Stat(from)
		if err != nil {
			if isLink {
				return fmt.Errorf("dangling symlink %s: %w", from, err)
			}
			return err
		}
		if !info.IsDir() {
			if err := copyEntry(from, to, 0); err != nil {
				return err
			}
			continue
		}
		real, err := filepath.EvalSymlinks(from)
		if err != nil {
			return err
		}
		if slices.Contains(ancestors, real) {
			return fmt.Errorf("symlink cycle at %s: it points to %s, which contains it", from, real)
		}
		if err := copyDeref(from, to, append(ancestors, real), nil); err != nil {
			return err
		}
	}
	return nil
}

// CopyTree copies a harness source directory the way git sees it: tracked
// files plus untracked files that are not ignored. node_modules, build output
// and a gitignored .env stay behind; a new uncommitted harness file still
// arrives. Outside a git working tree, or when src itself is ignored (an
// installed copy under an ignored parent), it behaves as CopyDir.
//
// Use it for a whole harness source. Artifact copies stay on CopyDir: they
// are a directory the harness already chose, not a working tree.
func CopyTree(src, dst string) error {
	files, ok := gitVisibleFiles(src)
	if !ok {
		return CopyDir(src, dst)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, rel := range files {
		from := filepath.Join(src, rel)
		fi, err := os.Lstat(from)
		if err != nil {
			if os.IsNotExist(err) {
				continue // listed by git, deleted in the working tree
			}
			return err
		}
		if fi.IsDir() {
			continue // a submodule: git lists the directory, not its files
		}
		if err := copyEntry(from, filepath.Join(dst, rel), fi.Mode().Type()); err != nil {
			return err
		}
	}
	return nil
}

// gitVisibleFiles lists the files git considers part of the tree at dir,
// relative to dir. ok is false when dir is not in a git working tree or is
// itself ignored, in which case git would list nothing and the caller should
// copy everything instead.
func gitVisibleFiles(dir string) ([]string, bool) {
	if err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return nil, false
	}
	// Exit 0 means dir is ignored; 1 means it is not.
	if err := exec.Command("git", "-C", dir, "check-ignore", "-q", ".").Run(); err == nil {
		return nil, false
	}
	out, err := exec.Command("git", "-C", dir, "ls-files", "--cached", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return nil, false
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, filepath.FromSlash(p))
		}
	}
	return files, true
}

// copyEntry copies one non-directory entry. A symlink is recreated as a
// symlink; anything already at dst that is a file or link is replaced, and a
// link is removed rather than written through, so the copy cannot write
// outside dst.
func copyEntry(src, dst string, typ fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if existing, err := os.Lstat(dst); err == nil {
		if existing.IsDir() {
			return fmt.Errorf("copying %s: destination %s is a directory", src, dst)
		}
		if typ&fs.ModeSymlink != 0 || existing.Mode()&fs.ModeSymlink != 0 {
			if err := os.Remove(dst); err != nil {
				return err
			}
		}
	}
	if typ&fs.ModeSymlink != 0 {
		link, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(link, dst)
	}
	return CopyFile(src, dst)
}

// FindInstructionsFile returns the path to the instructions file in dir.
// Checks instructions.md first, then AGENTS.md as fallback.
// Returns empty string if neither exists.
func FindInstructionsFile(dir string) string {
	for _, name := range []string{"instructions.md", "AGENTS.md"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// ComposeInstructions builds the project instructions from every content
// source's instructions file (instructions.md, else AGENTS.md).
//
// The harness's own content (empty Label) comes first, unheaded. Each include
// that has instructions follows, in include order, as a section headed
// "## Instructions from <label>". An include with NoInstructions set
// contributes nothing, and content identical to a section already present
// appears once. When the harness has no instructions the result is the include
// sections alone, and a single unheaded source is returned byte for byte.
// Returns nil when no source has instructions.
func ComposeInstructions(content []resolver.ResolvedContent) []byte {
	type section struct {
		label string
		body  string
	}
	var own, included []section
	seen := map[string]bool{}
	add := func(rc resolver.ResolvedContent, dst *[]section) {
		if rc.NoInstructions {
			return
		}
		src := FindInstructionsFile(rc.BasePath)
		if src == "" {
			return
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return
		}
		key := strings.TrimSpace(string(data))
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		*dst = append(*dst, section{label: rc.Label, body: string(data)})
	}
	for _, rc := range content {
		if rc.Label == "" {
			add(rc, &own)
		}
	}
	for _, rc := range content {
		if rc.Label != "" {
			add(rc, &included)
		}
	}

	if len(included) == 0 && len(own) == 1 {
		return []byte(own[0].body)
	}
	var parts []string
	for _, s := range own {
		parts = append(parts, strings.TrimRight(s.body, "\n"))
	}
	for _, s := range included {
		parts = append(parts, "## Instructions from "+s.label+"\n\n"+strings.TrimRight(s.body, "\n"))
	}
	if len(parts) == 0 {
		return nil
	}
	return []byte(strings.Join(parts, "\n\n") + "\n")
}
