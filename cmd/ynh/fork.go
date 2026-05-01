package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// forkResult is the JSON shape for a successful ynh fork operation.
type forkResult struct {
	Capabilities  string             `json:"capabilities"`
	YnhVersion    string             `json:"ynh_version"`
	Name          string             `json:"name"`
	Path          string             `json:"path"`
	InstalledFrom *listInstalledFrom `json:"installed_from,omitempty"`
}

func cmdFork(args []string) error {
	return cmdForkTo(args, os.Stdout, os.Stderr)
}

func cmdForkTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
	var toPath string
	var name string
	var newName string
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		case "--to":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--to requires a value")
			}
			i++
			toPath = args[i]
		case "--name":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--name requires a value")
			}
			i++
			newName = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			if name != "" {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i]))
			}
			name = args[i]
		}
		i++
	}

	if name == "" {
		return cliError(stderr, structured, errCodeInvalidInput, "usage: ynh fork <harness-name> [--to <path>] [--name <new>]")
	}
	if newName != "" && !harness.IsValidName(newName) {
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --name value %q: must match %s", newName, harness.ValidNamePattern()))
	}

	switch format {
	case "text", "json":
		// both handled below
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}

	// Load source harness. LoadQualified accepts both bare names ("researcher")
	// and namespace-qualified refs ("researcher@org/repo") so callers can
	// disambiguate when two registries publish the same name.
	p, err := harness.LoadQualified(name)
	if err != nil {
		code := errCodeNotFound
		if !strings.Contains(err.Error(), "not found") {
			code = errCodeIOError
		}
		return cliError(stderr, structured, code, err.Error())
	}

	// installName is the name the fork registers under and writes into its
	// own plugin.json. Defaults to the source name; --name lets the user
	// fork while keeping the upstream installed (otherwise the clash check
	// would force them to uninstall first).
	installName := p.Name
	if newName != "" {
		installName = newName
	}

	// Resolve destination directory
	destDir := toPath
	if destDir == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return cliError(stderr, structured, errCodeIOError,
				fmt.Sprintf("getting working directory: %v", cwdErr))
		}
		destDir = filepath.Join(cwd, installName)
	}
	absDestDir, absErr := filepath.Abs(destDir)
	if absErr != nil {
		return cliError(stderr, structured, errCodeIOError,
			fmt.Sprintf("resolving destination path: %v", absErr))
	}

	// Refuse to overwrite an existing directory
	if _, statErr := os.Stat(absDestDir); statErr == nil {
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("destination already exists: %s", absDestDir))
	}

	// Clash check: refuse to register if a flat install already claims the
	// install name — either a pointer file or a flat tree at
	// ~/.ynh/harnesses/<installName>/. Namespaced installs of the same name
	// are fine; they remain accessible via "name@org/repo" while the fork
	// takes over the bare name. Same rule ynh install uses, applied at
	// registration time.
	if existing, err := harness.LoadPointer(installName); err == nil && existing != nil {
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("harness %q is already installed (registered at %s)", installName, existing.Source))
	}
	if _, err := os.Stat(harness.InstalledDir(installName)); err == nil {
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("harness %q is already installed (uninstall it first, or pass --name to fork under a different name)", installName))
	}

	// p.Dir is the resolved install directory — works for both flat and
	// namespaced layouts (.ynh/harnesses/<ns--repo>/<name>/).
	installDir := p.Dir

	// Copy harness files to destination
	if mkErr := os.MkdirAll(absDestDir, 0o755); mkErr != nil {
		return cliError(stderr, structured, errCodeIOError,
			fmt.Sprintf("creating destination directory: %v", mkErr))
	}
	if copyErr := assembler.CopyDir(installDir, absDestDir); copyErr != nil {
		_ = os.RemoveAll(absDestDir)
		return cliError(stderr, structured, errCodeIOError,
			fmt.Sprintf("copying harness: %v", copyErr))
	}

	// Rewrite plugin.json's name field when --name was given. Required
	// for identity coherence: Load(installName) returns a Harness whose
	// p.Name must equal installName, otherwise downstream code (run dirs,
	// launcher exec, profile lookup) gets confused. Provenance survives —
	// the upstream identity is preserved in installed_from.forked_from.
	if newName != "" {
		hj, loadErr := plugin.LoadPluginJSON(absDestDir)
		if loadErr != nil {
			_ = os.RemoveAll(absDestDir)
			return cliError(stderr, structured, errCodeIOError,
				fmt.Sprintf("reading fork manifest: %v", loadErr))
		}
		hj.Name = installName
		if saveErr := plugin.SavePluginJSON(absDestDir, hj); saveErr != nil {
			_ = os.RemoveAll(absDestDir)
			return cliError(stderr, structured, errCodeIOError,
				fmt.Sprintf("renaming fork manifest: %v", saveErr))
		}
	}

	// Build forked_from from source provenance
	ff := buildForkedFrom(p)

	// Write installed.json marking this as a local fork
	ins := &plugin.InstalledJSON{
		SourceType:  "local",
		Source:      absDestDir,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		ForkedFrom:  ff,
	}
	if saveErr := plugin.SaveInstalledJSON(absDestDir, ins); saveErr != nil {
		_ = os.RemoveAll(absDestDir)
		return cliError(stderr, structured, errCodeIOError,
			fmt.Sprintf("saving provenance: %v", saveErr))
	}

	// Register the fork in the YNH layer via a pointer file. Pointer wins
	// over tree-shaped installs in Load() so subsequent ynh run / ls / info
	// resolve to absDestDir directly — no copy under ~/.ynh/harnesses.
	ptr := &harness.Pointer{
		Name:        installName,
		SourceType:  "local",
		Source:      absDestDir,
		InstalledAt: ins.InstalledAt,
	}
	if err := harness.SavePointer(ptr); err != nil {
		_ = os.RemoveAll(absDestDir)
		return cliError(stderr, structured, errCodeIOError,
			fmt.Sprintf("registering fork: %v", err))
	}

	// Generate launcher so the fork is fully runnable as `<installName>`,
	// matching ynh install. Skip for the reserved "ynh" name to avoid
	// shadowing the binary.
	if installName != "ynh" {
		if err := generateLauncher(installName); err != nil {
			_ = harness.RemovePointer(installName)
			_ = os.RemoveAll(absDestDir)
			return cliError(stderr, structured, errCodeIOError,
				fmt.Sprintf("generating launcher: %v", err))
		}
	}

	if format == "json" {
		result := forkResult{
			Capabilities: config.CapabilitiesVersion,
			YnhVersion:   config.Version,
			Name:         installName,
			Path:         absDestDir,
			InstalledFrom: &listInstalledFrom{
				SourceType:  "local",
				Source:      absDestDir,
				InstalledAt: ins.InstalledAt,
				ForkedFrom:  buildListForkedFrom(ff),
			},
		}
		data, jsonErr := json.MarshalIndent(result, "", "  ")
		if jsonErr != nil {
			return fmt.Errorf("encoding fork result: %w", jsonErr)
		}
		_, err = fmt.Fprintln(stdout, string(data))
		return err
	}

	// Text output
	if installName != p.Name {
		_, _ = fmt.Fprintf(stdout, "Forked harness %q as %q to %s\n", p.Name, installName, absDestDir)
	} else {
		_, _ = fmt.Fprintf(stdout, "Forked harness %q to %s\n", p.Name, absDestDir)
	}
	if p.InstalledFrom != nil {
		_, _ = fmt.Fprintf(stdout, "  Source:  %s (%s)\n", p.InstalledFrom.Source, p.InstalledFrom.SourceType)
	}
	if p.Version != "" {
		_, _ = fmt.Fprintf(stdout, "  Version: %s\n", p.Version)
	}
	return nil
}

// buildForkedFrom derives the ForkedFromJSON record from a loaded source harness.
func buildForkedFrom(p *harness.Harness) *plugin.ForkedFromJSON {
	if p.InstalledFrom == nil {
		return &plugin.ForkedFromJSON{
			SourceType: "local",
			Source:     p.Dir,
			Version:    p.Version,
		}
	}
	return &plugin.ForkedFromJSON{
		SourceType:   p.InstalledFrom.SourceType,
		Source:       p.InstalledFrom.Source,
		Ref:          p.InstalledFrom.Ref,
		SHA:          p.InstalledFrom.SHA,
		Path:         p.InstalledFrom.Path,
		RegistryName: p.InstalledFrom.RegistryName,
		Version:      p.Version,
	}
}

// buildListForkedFrom converts a plugin.ForkedFromJSON to its list.go wire shape.
func buildListForkedFrom(ff *plugin.ForkedFromJSON) *listForkedFrom {
	if ff == nil {
		return nil
	}
	return &listForkedFrom{
		SourceType:   ff.SourceType,
		Source:       ff.Source,
		Ref:          ff.Ref,
		SHA:          ff.SHA,
		Path:         ff.Path,
		RegistryName: ff.RegistryName,
		Version:      ff.Version,
	}
}
