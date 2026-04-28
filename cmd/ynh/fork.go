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
		return cliError(stderr, structured, errCodeInvalidInput, "usage: ynh fork <harness-name> [--to <path>]")
	}

	switch format {
	case "text", "json":
		// both handled below
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}

	// Load source harness
	p, err := harness.Load(name)
	if err != nil {
		code := errCodeNotFound
		if !strings.Contains(err.Error(), "not found") {
			code = errCodeIOError
		}
		return cliError(stderr, structured, code, err.Error())
	}

	// Resolve destination directory
	destDir := toPath
	if destDir == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return cliError(stderr, structured, errCodeIOError,
				fmt.Sprintf("getting working directory: %v", cwdErr))
		}
		destDir = filepath.Join(cwd, name)
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

	installDir := harness.InstalledDir(name)

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

	if format == "json" {
		result := forkResult{
			Capabilities: config.CapabilitiesVersion,
			YnhVersion:   config.Version,
			Name:         p.Name,
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
	_, _ = fmt.Fprintf(stdout, "Forked harness %q to %s\n", p.Name, absDestDir)
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
			Source:     harness.InstalledDir(p.Name),
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
