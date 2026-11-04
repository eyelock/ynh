package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/telemetry/weaver"
	"github.com/eyelock/ynh/telemetry/registry"
)

// cmdTelemetry is `ynh telemetry registry`: the names ynh emits in
// OpenTelemetry, as ynr asks every tool to print them (ynr ADR-007). The
// registry is embedded in the binary; its tool name and version are what
// service.name and service.version say on every record ynh writes. The JSON
// shape is ynf's (`ynf telemetry registry --format json`), which ynr reads.
func cmdTelemetry(args []string) error { return cmdTelemetryTo(args, os.Stdout, os.Stderr) }

func cmdTelemetryTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)
	if len(args) == 0 || args[0] != "registry" {
		return cliError(stderr, structured, errCodeInvalidInput, "usage: ynh telemetry registry [--format text|json]")
	}
	format := "text"
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("unknown flag: %s", args[i]))
			}
			return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("unexpected argument: %s", args[i]))
		}
	}
	if format != "text" && format != "json" {
		return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("--format %q: want text or json", format))
	}
	r, err := weaver.Load(registry.FS, config.Version)
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, fmt.Sprintf("reading the telemetry registry: %v", err))
	}
	if format == "json" {
		return writeJSON(stdout, r)
	}
	_, err = io.WriteString(stdout, registryText(r))
	return err
}

// registryText is the registry for a person: the tool, the conventions it
// follows, and each name with its type, kind or instrument.
func registryText(r *weaver.Registry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s, following %s %s\n\n", r.Tool, r.Version, r.Semconv.Name, r.Semconv.Version)
	b.WriteString("attributes\n")
	for _, at := range r.Attributes {
		fmt.Fprintf(&b, "  %s (%s)\n", at.ID, at.Type)
	}
	b.WriteString("standard attributes\n")
	for _, id := range r.Standard {
		fmt.Fprintf(&b, "  %s\n", id)
	}
	b.WriteString("spans\n")
	for _, s := range r.Spans {
		fmt.Fprintf(&b, "  %s (%s)\n", s.Name, s.Kind)
	}
	b.WriteString("events\n")
	for _, e := range r.Events {
		fmt.Fprintf(&b, "  %s\n", e.Name)
	}
	b.WriteString("metrics\n")
	if len(r.Metrics) == 0 {
		b.WriteString("  none\n")
	}
	for _, m := range r.Metrics {
		fmt.Fprintf(&b, "  %s (%s, %s)\n", m.Name, m.Instrument, m.Unit)
	}
	return b.String()
}
