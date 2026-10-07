package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/telemetry"
)

// telemetryOptions tunes the spool. Production uses the format's defaults;
// tests shrink the caps to reach them.
var telemetryOptions telemetry.Options

// runOutcomes is ynh's outcome vocabulary for a run, one word per exit code
// (docs/agent.md, "Exit codes").
var runOutcomes = map[int]string{
	agent.ExitConverged:        "converged",
	agent.ExitRefused:          "refused",
	agent.ExitIterationCap:     "turn_cap",
	agent.ExitTokenBudget:      "token_budget",
	agent.ExitWallClock:        "wall_clock",
	agent.ExitStuck:            "stuck",
	agent.ExitTamper:           "tamper",
	agent.ExitPlanIterationCap: "plan_iteration_cap",
	agent.ExitWorkerError:      "worker_error",
	agent.ExitResumeError:      "resume_error",
	agent.ExitGateError:        "gate_error",
	agent.ExitUserAborted:      "user_aborted",
	agent.ExitInterrupted:      "interrupted",
}

// runOutcome names a run's exit code.
func runOutcome(code int) string {
	if o, ok := runOutcomes[code]; ok {
		return o
	}
	return "unknown"
}

// Every attribute below is an id, a name, an enum or a count (contract rule
// 10). The task, the prompt, the run's error text and every path (worktree,
// session directory, --resume, --emit-jsonl) are deliberately absent.

// runStartAttributes describes the run as it was asked for, before it starts.
func runStartAttributes(opts agent.RunOptions) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	add := func(k attribute.Key, v string) {
		if v != "" {
			attrs = append(attrs, k.String(v))
		}
	}
	// Only a canonical id: anything else is refused before it loads, and a
	// value that is not an id could be a path.
	if namespace.Classify(opts.HarnessName) == namespace.RefID {
		add(telemetry.AttrHarnessName, opts.HarnessName)
	}
	add(telemetry.AttrFocus, opts.Focus)
	add(telemetry.AttrProfile, opts.Profile)
	add(telemetry.AttrVendor, opts.Backend)
	add(telemetry.AttrModelRequested, opts.Model)
	add(telemetry.AttrEffortRequested, opts.Effort)
	if opts.Resume != "" {
		attrs = append(attrs, telemetry.AttrResumed.Bool(true))
	}
	return attrs
}

// runEndAttributes describes the run as it ended, from its result.
func runEndAttributes(r *agent.RunResult) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		telemetry.AttrExitCode.Int(r.ExitCode),
		telemetry.AttrTurns.Int(r.Consumed.Turns),
	}
	add := func(k attribute.Key, v string) {
		if v != "" {
			attrs = append(attrs, k.String(v))
		}
	}
	add(telemetry.AttrSessionID, r.SessionID)
	add(telemetry.AttrVendor, r.Backend)
	add(telemetry.AttrModel, r.Model)
	add(telemetry.AttrModelRequested, r.ModelRequested)
	add(telemetry.AttrEffort, r.Effort)
	add(telemetry.AttrEffortRequested, r.EffortRequested)
	add(telemetry.AttrBoundBy, r.BoundBy)
	if h := r.Harness; h != nil {
		add(telemetry.AttrHarnessName, h.Name)
		add(telemetry.AttrHarnessVersion, h.Version)
		add(telemetry.AttrHarnessCommit, h.SHA)
	}
	// Token counts only as the backend reported them, never a zero nobody
	// measured. gen_ai.usage.input_tokens includes cached tokens, which ynh
	// keeps beside its input count rather than in it.
	c := r.Consumed
	if c.InputTokens != nil {
		input := *c.InputTokens
		if c.CacheReadTokens != nil {
			input += *c.CacheReadTokens
			attrs = append(attrs, telemetry.AttrCacheReadTokens.Int64(*c.CacheReadTokens))
		}
		if c.CacheCreationTokens != nil {
			input += *c.CacheCreationTokens
			attrs = append(attrs, telemetry.AttrCacheCreationTokens.Int64(*c.CacheCreationTokens))
		}
		attrs = append(attrs, telemetry.AttrInputTokens.Int64(input))
	}
	if c.OutputTokens != nil {
		attrs = append(attrs, telemetry.AttrOutputTokens.Int64(*c.OutputTokens))
	}
	if c.CostUSD != nil {
		attrs = append(attrs, telemetry.AttrCostUSD.Float64(*c.CostUSD))
	}
	return attrs
}

// runTelemetry gives the agent loop the run's trace context and spool for
// each process it starts, naming the spans from ynh's constants, and the
// run's relay when the telemetry relay setting is on.
type runTelemetry struct {
	run   *telemetry.Run
	relay *runRelay // nil when the setting is off
}

func (r runTelemetry) WorkerEnv() []string { return r.run.WorkerEnv() }

func (r runTelemetry) RelayEndpoint(backend string) string { return r.relay.endpoint(backend) }

func (r runTelemetry) StartCall(kind string, turn int, sensor string) ([]string, func(string, bool)) {
	attrs := []attribute.KeyValue{telemetry.AttrTurn.Int(turn)}
	name := telemetry.SpanCheck
	if kind == agent.CallSensor {
		name = telemetry.SpanSensorRun
		attrs = append(attrs, telemetry.AttrSensorName.String(sensor))
	}
	return r.run.StartCall(name, attrs...)
}

// The relay's bounds. Tests shorten them.
var (
	relayReadyTimeout = telemetry.RelayReadyTimeout
	relayStopTimeout  = telemetry.RelayStopTimeout
)

// telemetryRelayEnv names the telemetry relay setting's variable.
const telemetryRelayEnv = "YNH_TELEMETRY_RELAY"

// telemetryRelaySetting resolves the telemetry relay setting: the
// --telemetry-relay flag, else YNH_TELEMETRY_RELAY, else "telemetry_relay"
// in config.json, else off. The variable can also turn off what the
// configuration turns on. A value it does not recognise is a note, and off.
// An unreadable config.json leaves the setting off: telemetry never fails a
// run, and every other command reports the file.
func telemetryRelaySetting(flag bool, stderr io.Writer) bool {
	if flag {
		return true
	}
	if v, ok := os.LookupEnv(telemetryRelayEnv); ok && v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
		_, _ = fmt.Fprintf(stderr, "ynh: %s=%q is not on or off; the telemetry relay stays off\n", telemetryRelayEnv, v)
		return false
	}
	cfg, err := config.Load()
	return err == nil && cfg.TelemetryRelay
}

// runRelay is the `ynr relay` for one run. It starts when the loop is about
// to start the worker, only if the conditions hold, and is stopped once the
// worker has exited. When a condition fails, or the relay cannot start,
// the run carries on without the vendor's own telemetry, after one note.
type runRelay struct {
	tel    *telemetry.Telemetry
	stderr io.Writer
	// sandbox is the run's --sandbox. Under srt the worker cannot reach a
	// loopback relay, so none is started.
	sandbox string

	once  sync.Once
	relay *telemetry.Relay
	// sigs holds SIGINT and SIGTERM while the relay runs. The loop handles
	// them itself; this keeps a signal that arrives after the loop has
	// returned from killing ynh before it has stopped the relay.
	sigs chan os.Signal
}

// endpoint starts the relay, the first time it is asked, and returns its
// endpoint, or "" when there is none.
func (r *runRelay) endpoint(backend string) string {
	if r == nil {
		return ""
	}
	r.once.Do(func() { r.start(backend) })
	if r.relay == nil {
		return ""
	}
	return r.relay.Endpoint
}

func (r *runRelay) start(backend string) {
	skip := func(why string) {
		_, _ = fmt.Fprintf(r.stderr, "ynh: the telemetry relay is on, but %s; the run continues without %s's own telemetry\n", why, backend)
	}
	if !agent.SupportsTelemetryRelay(backend) {
		skip("ynh does not configure " + backend + "'s telemetry yet")
		return
	}
	// srt's settings can allow the relay's 127.0.0.1:<port>, but only for
	// what goes through srt's proxy, and srt sets NO_PROXY to cover
	// 127.0.0.1 and localhost inside the sandbox. A client that honours it
	// dials the relay directly, which the sandbox refuses (seatbelt on
	// macOS, a network namespace on Linux). The one direct opening,
	// network.allowLocalBinding, opens every loopback port on macOS and
	// none on Linux. So the relay is skipped rather than started
	// unreachable (see docs/telemetry.md).
	if r.sandbox == "srt" {
		skip("--sandbox srt would block the worker from reaching it")
		return
	}
	// Only into the spool. An operator's OTEL_EXPORTER_OTLP_* is their
	// choice, and the vendor reads it for itself (contract rule 1).
	switch r.tel.Destination() {
	case telemetry.DestinationOTLP:
		skip("OTEL_EXPORTER_OTLP_* is set, so the vendor's telemetry is left to it")
		return
	case telemetry.DestinationNone:
		skip("there is no spool folder (YNR_SPOOL, or $XDG_STATE_HOME/ynr/spool/local)")
		return
	}
	bin, err := exec.LookPath("ynr")
	if err != nil {
		skip("ynr is not on PATH")
		return
	}
	relay, err := telemetry.StartRelay(bin, r.tel.SpoolDir(), relayReadyTimeout)
	if err != nil {
		skip(err.Error())
		return
	}
	r.sigs = make(chan os.Signal, 1)
	signal.Notify(r.sigs, syscall.SIGINT, syscall.SIGTERM)
	r.relay = relay
}

// stop stops the relay, if one is running, within relayStopTimeout. It is
// safe to call more than once and on a nil runRelay. A relay that died
// during the run, or did not stop cleanly, is reported in one note; the
// run's result and exit code are not touched.
func (r *runRelay) stop() {
	if r == nil || r.relay == nil {
		return
	}
	if err := r.relay.Stop(relayStopTimeout); err != nil {
		_, _ = fmt.Fprintf(r.stderr, "ynh: telemetry relay: %v\n", err)
	}
	signal.Stop(r.sigs)
}
