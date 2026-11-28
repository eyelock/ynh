package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/telemetry"
	"github.com/eyelock/ynh/internal/vendor"
)

// runDestination is where `ynh run`'s vendor telemetry would go. `ynh run`
// does not use the launch record's Telemetry for the relay, so the destination
// is chosen here.
type runDestination struct {
	dest telemetry.Destination
	dir  string
}

func (d runDestination) Destination() telemetry.Destination { return d.dest }
func (d runDestination) SpoolDir() string                   { return d.dir }
func (d runDestination) OperatorOTLP() bool                 { return d.dest == telemetry.DestinationOTLP }

// telemetryRelayEndpointEnv names the standing relay endpoint setting's
// variable.
const telemetryRelayEndpointEnv = "YNH_TELEMETRY_RELAY_ENDPOINT"

// standingRelayEndpoint resolves the standing relay endpoint: the
// --telemetry-relay-endpoint flag, else YNH_TELEMETRY_RELAY_ENDPOINT, else
// "telemetry_relay_endpoint" in config.json, else "" (off). It must be plain
// http on a loopback address; anything else is a note, and off, so a
// vendor's telemetry is never sent off the host by this setting. An
// unreadable config.json leaves it off, as for the relay switch.
func standingRelayEndpoint(flag string, stderr io.Writer) string {
	raw, from := flag, "--telemetry-relay-endpoint"
	if raw == "" {
		if v, ok := os.LookupEnv(telemetryRelayEndpointEnv); ok {
			raw, from = v, telemetryRelayEndpointEnv
		}
	}
	if raw == "" {
		cfg, err := config.Load()
		if err != nil {
			return ""
		}
		raw, from = cfg.TelemetryRelayEndpoint, `"telemetry_relay_endpoint"`
	}
	if raw == "" {
		return ""
	}
	endpoint, err := telemetry.ParseRelayEndpoint(raw)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "ynh: %s: %v; the vendor's telemetry is not pointed at it\n", from, err)
		return ""
	}
	return endpoint
}

// startRunRelay points a `ynh run` vendor's own telemetry at a relay, by one
// of two routes, then through ynh's own environment (which the vendor child
// inherits) and the vendor arguments it returns.
//
//   - A standing relay endpoint is configured (--telemetry-relay-endpoint,
//     YNH_TELEMETRY_RELAY_ENDPOINT or config.json): the vendor is pointed at
//     it and no relay starts. The endpoint is the operator's opt-in, so the
//     relay switch is not needed.
//   - Otherwise, when the relay switch is on (--telemetry-relay,
//     YNH_TELEMETRY_RELAY or config.json), ynh starts its own per-launch relay
//     for the vendor's lifetime.
//
// Either way it applies only to a vendor ynh configures under `ynh run`
// (agent.SupportsRunRelay), and an operator's OTEL_EXPORTER_OTLP_* wins over
// both. With neither route on, nothing changes and nothing starts: ynr being
// installed starts nothing (ynr ADR-006, rule 14). The conditions and the
// notes are those of `ynh agent run` (docs/telemetry.md). It returns the
// function that stops the relay, safe to defer and to call when no relay
// started, and the vendor arguments that point a vendor configured on its
// command line (Claude Code, Codex) at the relay.
func startRunRelay(backend string, flag bool, endpointFlag, sessionID string, stderr io.Writer) (func(), []string) {
	standing := standingRelayEndpoint(endpointFlag, stderr)
	if standing == "" && !telemetryRelaySetting(flag, stderr) {
		return func() {}, nil
	}
	dest, dir := telemetry.ChooseDestination()
	r := &runRelay{
		tel:       runDestination{dest: dest, dir: dir},
		stderr:    stderr,
		supported: agent.SupportsRunRelay,
		standing:  standing,
		sessionID: sessionID,
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

// launchRecord is `ynh run`'s own record of one launch, whichever vendor runs
// (docs/telemetry.md, "What a launch emits"). It is the ynh.session span and
// its started and finished events. With no destination it does nothing and
// changes nothing.
type launchRecord struct {
	tel     *telemetry.Telemetry
	session *telemetry.Session
	started bool // the vendor CLI was started
}

// startLaunchRecord opens the record and, when telemetry is on, puts the
// trace context and ynh.session.id into this process's environment, which the
// vendor CLI and every process `ynh run` starts inherit. Call end when the
// launch is over, on every path.
func startLaunchRecord(vendorName string, loaded *harness.Harness, harnessID, focus, profile string, resumed bool, labels map[string]string, stderr io.Writer) *launchRecord {
	opts := telemetryOptions
	opts.Labels = labels
	tel := telemetry.Setup(config.Version, opts, stderr)
	rec := &launchRecord{tel: tel}
	rec.session = tel.StartSession(launchAttributes(vendorName, loaded, harnessID, focus, profile, resumed)...)
	rec.session.ApplyEnv()
	return rec
}

// relaySessionID is the launch's session id for a per-launch relay to stamp
// on the vendor's records, or "" when the launch writes no record of its own
// (nothing then names the session) or there is no record at all.
func (r *launchRecord) relaySessionID() string {
	if r == nil || !r.tel.Active() {
		return ""
	}
	return r.session.ID()
}

// markStarted is called as the vendor CLI is about to start.
func (r *launchRecord) markStarted() {
	if r != nil {
		r.started = true
	}
}

// end finishes the record with how the launch ended and stops telemetry. err
// is what the launch returned.
func (r *launchRecord) end(err error) {
	outcome, code, sig := launchOutcome(err, r.started)
	attrs := []attribute.KeyValue{telemetry.AttrProcessExitCode.Int(code)}
	if sig != 0 {
		attrs = append(attrs, telemetry.AttrSessionSignal.String(signalName(sig)))
	}
	r.session.Finish(outcome, outcome == telemetry.SessionOutcomeExited, attrs...)
	r.tel.Shutdown()
}

// launchOutcome names how a launch ended, from what the launch returned and
// whether the vendor CLI was started. The code is the one `ynh run` exits
// with: the vendor's, 128+n for a signal as a shell reports it, or 1 for a
// launch ynh refused.
func launchOutcome(err error, started bool) (outcome string, code int, sig syscall.Signal) {
	var ee *vendor.ExitError
	switch {
	case err == nil:
		return telemetry.SessionOutcomeExited, 0, 0
	case errors.As(err, &ee) && ee.Signal != 0:
		return telemetry.SessionOutcomeSignalled, ee.Code, ee.Signal
	case errors.As(err, &ee):
		return telemetry.SessionOutcomeFailed, ee.Code, 0
	case started:
		// The vendor ran and ynh failed after it started.
		return telemetry.SessionOutcomeFailed, 1, 0
	default:
		return telemetry.SessionOutcomeRefused, 1, 0
	}
}

// signalName is the signal's name, such as SIGTERM.
func signalName(s syscall.Signal) string {
	names := map[syscall.Signal]string{
		syscall.SIGHUP: "SIGHUP", syscall.SIGINT: "SIGINT", syscall.SIGQUIT: "SIGQUIT",
		syscall.SIGILL: "SIGILL", syscall.SIGABRT: "SIGABRT", syscall.SIGFPE: "SIGFPE",
		syscall.SIGKILL: "SIGKILL", syscall.SIGSEGV: "SIGSEGV", syscall.SIGPIPE: "SIGPIPE",
		syscall.SIGALRM: "SIGALRM", syscall.SIGTERM: "SIGTERM", syscall.SIGBUS: "SIGBUS",
	}
	if n, ok := names[s]; ok {
		return n
	}
	return s.String()
}

// launchAttributes describes the launch as it was asked for. Every one is an
// id, a name, an enum or a flag (ynr ADR-006 rule 10): no paths, no prompt, no
// content, no tokens or cost.
func launchAttributes(vendorName string, loaded *harness.Harness, harnessID, focus, profile string, resumed bool) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	add := func(k attribute.Key, v string) {
		if v != "" {
			attrs = append(attrs, k.String(v))
		}
	}
	add(telemetry.AttrSessionVendor, vendorName)
	// Only a canonical id: a harness given by path has no id, and a value that
	// is not an id could be a path.
	if namespace.Classify(harnessID) == namespace.RefID {
		add(telemetry.AttrHarnessName, harnessID)
	}
	if loaded != nil {
		add(telemetry.AttrHarnessVersion, loaded.Version)
		if loaded.InstalledFrom != nil {
			add(telemetry.AttrHarnessCommit, loaded.InstalledFrom.SHA)
		}
	}
	add(telemetry.AttrRunFocus, focus)
	add(telemetry.AttrRunProfile, profile)
	if resumed {
		attrs = append(attrs, telemetry.AttrRunResumed.Bool(true))
	}
	return attrs
}
