// Package registry carries ynh's telemetry registry, an OpenTelemetry Weaver
// registry (ynr ADR-007): manifest.yaml and the groups beside it, the names
// ynh emits under the ynh. prefix. It is embedded so `ynh telemetry registry`
// prints what the binary emits, and internal/telemetry/names_gen.go is
// generated from it.
package registry

import "embed"

// FS holds the registry's YAML files, at its root.
//
//go:embed *.yaml
var FS embed.FS
