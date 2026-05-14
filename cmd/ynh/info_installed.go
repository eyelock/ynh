// `ynh info <name> --installed` surfaces the recorded install provenance
// for a harness — what file was installed from where, at what time, and
// (for forks) the upstream provenance. The on-disk shape lives at
// ~/.ynh/harnesses/<id-fsname>/.ynh-plugin/installed.json (for tree-form
// installs) or at the pointer file (for local/source installs); this
// flag unifies the read so consumers don't have to know the topology.
//
// Previously a standalone `ynh installed <name>` command; folded into
// `ynh info` for surface coherence (one verb per harness lookup).
package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
)

// installedEnvelope wraps the install record with the wire-protocol
// capabilities + ynh release version. Mirrors infoEnvelope / listEnvelope.
type installedEnvelope struct {
	Capabilities string          `json:"capabilities"`
	YnhVersion   string          `json:"ynh_version"`
	ID           string          `json:"id"`
	Installed    json.RawMessage `json:"installed"`
}

func loadInstalledRecord(name string) (*harness.Harness, any, string, error) {
	h, err := harness.LoadByID(name)
	if err != nil {
		return nil, nil, errCodeNotFound, fmt.Errorf("harness %q is not installed", name)
	}
	ins, err := harness.LoadInstalledRecord(name, h)
	if err != nil {
		return h, nil, errCodeIOError, fmt.Errorf("reading install record: %v", err)
	}
	if ins == nil {
		return h, nil, errCodeNotFound, fmt.Errorf("no install record for harness %q (pre-migration install?)", name)
	}
	return h, ins, "", nil
}

func printInstalledText(stdout, stderr io.Writer, name string) error {
	_, ins, code, err := loadInstalledRecord(name)
	if err != nil {
		return cliError(stderr, false, code, err.Error())
	}
	_, _ = fmt.Fprintf(stdout, "id:           %s\n", name)
	insBytes, mErr := json.MarshalIndent(ins, "", "  ")
	if mErr != nil {
		return mErr
	}
	_, _ = fmt.Fprintf(stdout, "installed:\n%s\n", string(insBytes))
	return nil
}

func printInstalledJSON(stdout, stderr io.Writer, name string) error {
	_, ins, code, err := loadInstalledRecord(name)
	if err != nil {
		return cliError(stderr, true, code, err.Error())
	}
	insBytes, mErr := json.Marshal(ins)
	if mErr != nil {
		return fmt.Errorf("encoding installed record: %w", mErr)
	}
	env := installedEnvelope{
		Capabilities: config.CapabilitiesVersion,
		YnhVersion:   config.Version,
		ID:           name,
		Installed:    insBytes,
	}
	data, mErr := json.MarshalIndent(env, "", "  ")
	if mErr != nil {
		return fmt.Errorf("encoding installed envelope: %w", mErr)
	}
	_, err = fmt.Fprintln(stdout, string(data))
	return err
}
