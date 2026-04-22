package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// infoEntry is the JSON shape for `ynh info` structured output.
// Identity fields at the top, provenance next, then the raw manifest.
type infoEntry struct {
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Description   string             `json:"description,omitempty"`
	DefaultVendor string             `json:"default_vendor"`
	Path          string             `json:"path"`
	InstalledFrom *listInstalledFrom `json:"installed_from,omitempty"`
	Manifest      json.RawMessage    `json:"manifest"`
}

func cmdInfo(args []string) error {
	return cmdInfoTo(args, os.Stdout, os.Stderr)
}

func cmdInfoTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
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
		return cliError(stderr, structured, errCodeInvalidInput, "usage: ynh info <harness-name>")
	}

	switch format {
	case "text":
		return printInfoText(stdout, name)
	case "json":
		return printInfoJSON(stdout, stderr, name)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func printInfoText(w io.Writer, name string) error {
	p, err := harness.Load(name)
	if err != nil {
		return err
	}

	vendorName := p.DefaultVendor
	if vendorName == "" {
		vendorName = "-"
	}

	_, _ = fmt.Fprintf(w, "Name:         %s\n", p.Name)
	_, _ = fmt.Fprintf(w, "Vendor:       %s\n", vendorName)

	if p.InstalledFrom != nil {
		_, _ = fmt.Fprintf(w, "Installed:    %s\n", p.InstalledFrom.InstalledAt)
		_, _ = fmt.Fprintf(w, "Source:       %s (%s)\n", p.InstalledFrom.Source, p.InstalledFrom.SourceType)
		if p.InstalledFrom.Path != "" {
			_, _ = fmt.Fprintf(w, "Path:         %s\n", p.InstalledFrom.Path)
		}
		if p.InstalledFrom.RegistryName != "" {
			_, _ = fmt.Fprintf(w, "Registry:     %s\n", p.InstalledFrom.RegistryName)
		}
	} else {
		_, _ = fmt.Fprintf(w, "Installed:    -\n")
		_, _ = fmt.Fprintf(w, "Source:       -\n")
	}

	// Local artifacts
	arts, _ := harness.ScanArtifacts(name)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Artifacts:")
	if arts.Total() == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		if len(arts.Skills) > 0 {
			_, _ = fmt.Fprintf(w, "  skills:    %s\n", strings.Join(arts.Skills, ", "))
		}
		if len(arts.Agents) > 0 {
			_, _ = fmt.Fprintf(w, "  agents:    %s\n", strings.Join(arts.Agents, ", "))
		}
		if len(arts.Rules) > 0 {
			_, _ = fmt.Fprintf(w, "  rules:     %s\n", strings.Join(arts.Rules, ", "))
		}
		if len(arts.Commands) > 0 {
			_, _ = fmt.Fprintf(w, "  commands:  %s\n", strings.Join(arts.Commands, ", "))
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Includes:")
	if len(p.Includes) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for _, inc := range p.Includes {
			line := "  " + inc.Git
			if inc.Path != "" {
				line += "  path=" + inc.Path
			}
			if inc.Ref != "" {
				line += "  ref=" + inc.Ref
			}
			if len(inc.Pick) > 0 {
				line += "  pick=[" + strings.Join(inc.Pick, ", ") + "]"
			}
			_, _ = fmt.Fprintln(w, line)
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Delegates to:")
	if len(p.DelegatesTo) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for _, del := range p.DelegatesTo {
			line := "  " + del.Git
			if del.Path != "" {
				line += "  path=" + del.Path
			}
			if del.Ref != "" {
				line += "  ref=" + del.Ref
			}
			_, _ = fmt.Fprintln(w, line)
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Hooks:")
	if len(p.Hooks) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for event, entries := range p.Hooks {
			for _, entry := range entries {
				line := "  " + event + ": " + entry.Command
				if entry.Matcher != "" {
					line += "  (matcher=" + entry.Matcher + ")"
				}
				_, _ = fmt.Fprintln(w, line)
			}
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "MCP Servers:")
	if len(p.MCPServers) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for sname, server := range p.MCPServers {
			if server.Command != "" {
				_, _ = fmt.Fprintf(w, "  %s: %s\n", sname, server.Command)
			} else if server.URL != "" {
				_, _ = fmt.Fprintf(w, "  %s: %s\n", sname, server.URL)
			}
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Profiles:")
	if len(p.Profiles) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for pname, profile := range p.Profiles {
			var parts []string
			if len(profile.Hooks) > 0 {
				var events []string
				for event := range profile.Hooks {
					events = append(events, event)
				}
				parts = append(parts, "hooks: "+strings.Join(events, ", "))
			}
			if len(profile.MCPServers) > 0 {
				var servers []string
				for sn := range profile.MCPServers {
					servers = append(servers, sn)
				}
				parts = append(parts, "mcp_servers: "+strings.Join(servers, ", "))
			}
			if len(parts) == 0 {
				_, _ = fmt.Fprintf(w, "  %s\n", pname)
			} else {
				_, _ = fmt.Fprintf(w, "  %s    %s\n", pname, strings.Join(parts, "    "))
			}
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Focus:")
	if len(p.Focuses) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for fname, focus := range p.Focuses {
			profileLabel := "(default)"
			if focus.Profile != "" {
				profileLabel = "profile=" + focus.Profile
			}
			_, _ = fmt.Fprintf(w, "  %s    %s    %q\n", fname, profileLabel, focus.Prompt)
		}
	}

	return nil
}

func printInfoJSON(stdout, stderr io.Writer, name string) error {
	p, err := harness.Load(name)
	if err != nil {
		code := errCodeNotFound
		if !strings.Contains(err.Error(), "not found") {
			code = errCodeIOError
		}
		return cliError(stderr, true, code, err.Error())
	}

	// Read the raw manifest (plugin.json for 0.2+, .harness.json for 0.1)
	installDir := harness.InstalledDir(name)
	manifestPath := filepath.Join(installDir, plugin.PluginDir, plugin.PluginFile)
	if _, err := os.Stat(manifestPath); err != nil {
		manifestPath = filepath.Join(installDir, plugin.HarnessFile)
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return cliError(stderr, true, errCodeIOError,
			fmt.Sprintf("reading manifest: %v", err))
	}

	// Compact the raw JSON to normalise whitespace
	var compacted json.RawMessage
	if err := json.Unmarshal(raw, &compacted); err != nil {
		return cliError(stderr, true, errCodeIOError,
			fmt.Sprintf("parsing manifest: %v", err))
	}

	entry := infoEntry{
		Name:          p.Name,
		Version:       p.Version,
		Description:   p.Description,
		DefaultVendor: p.DefaultVendor,
		Path:          harness.InstalledDir(name),
		Manifest:      compacted,
	}

	if p.InstalledFrom != nil {
		entry.InstalledFrom = &listInstalledFrom{
			SourceType:   p.InstalledFrom.SourceType,
			Source:       p.InstalledFrom.Source,
			Path:         p.InstalledFrom.Path,
			RegistryName: p.InstalledFrom.RegistryName,
			InstalledAt:  p.InstalledFrom.InstalledAt,
		}
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding info: %w", err)
	}
	_, err = fmt.Fprintln(stdout, string(data))
	return err
}
