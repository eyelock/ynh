package main

import (
	"io"
	"os"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/telemetry"
)

// runDestination is where `ynh run`'s vendor telemetry would go. `ynh run`
// writes no records of its own, so there is no Telemetry to ask.
type runDestination struct {
	dest telemetry.Destination
	dir  string
}

func (d runDestination) Destination() telemetry.Destination { return d.dest }
func (d runDestination) SpoolDir() string                   { return d.dir }

// startRunRelay starts the telemetry relay for a `ynh run` when the setting is
// on (--telemetry-relay, YNH_TELEMETRY_RELAY or config.json) and the vendor is
// one ynh configures under `ynh run` (agent.SupportsRunRelay), then points the
// vendor at it through ynh's own environment, which the vendor child inherits.
// The conditions and the notes are those of `ynh agent run`
// (docs/telemetry.md). It returns the function that stops the relay, safe to
// defer and to call when no relay started, and the vendor arguments that
// point a vendor configured on its command line (Codex) at the relay.
func startRunRelay(backend string, flag bool, stderr io.Writer) (func(), []string) {
	if !telemetryRelaySetting(flag, stderr) {
		return func() {}, nil
	}
	dest, dir := telemetry.ChooseDestination()
	r := &runRelay{
		tel:       runDestination{dest: dest, dir: dir},
		stderr:    stderr,
		supported: agent.SupportsRunRelay,
	}
	endpoint := r.endpoint(backend)
	if endpoint == "" {
		return func() {}, nil
	}
	for _, name := range agent.RunRelayUnset(backend) {
		_ = os.Unsetenv(name)
	}
	for _, kv := range agent.RunRelayEnv(backend, endpoint) {
		_ = os.Setenv(kv[0], kv[1])
	}
	return r.stop, agent.RunRelayArgs(backend, endpoint)
}
