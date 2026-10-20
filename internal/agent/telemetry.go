package agent

import "os"

// Kinds of call the loop makes to another process, for Telemetry.StartCall.
const (
	// CallCheck is a `ynh check` the loop runs: the gate after each turn, and
	// the convergence verifiers once the gate is green.
	CallCheck = "check"
)

// Telemetry is what the loop needs from ynh's telemetry: the environment
// that carries the run's trace context and spool folder to each process it
// starts (ynr's instrumentation contract, rule 4), and a span per call out.
//
// The environment is ynh's own, like YNH_AGENT_SESSION, not a passthrough of
// the operator's: it is added after env_passthrough, and only while
// telemetry is on. A Telemetry that returns none starts every process
// exactly as it would without telemetry.
type Telemetry interface {
	// WorkerEnv is added to the worker's environment.
	WorkerEnv() []string
	// RelayEndpoint is the OTLP/HTTP endpoint of a relay running for this
	// run, for the vendor CLI backend to export its own telemetry to, or ""
	// when there is none. It is asked once, just before the worker starts,
	// so the relay can be started only for a backend that can use it.
	RelayEndpoint(backend string) string
	// StartCall opens a span for one call of kind (CallCheck) on turn. It returns the environment for
	// the process it starts, and a function that ends the span with the
	// call's outcome; ok is false when the call itself could not run.
	StartCall(kind string, turn int) (env []string, end func(outcome string, ok bool))
}

// startCall is Telemetry.StartCall for a Telemetry that may be nil.
func startCall(tel Telemetry, kind string, turn int) ([]string, func(string, bool)) {
	if tel == nil {
		return nil, func(string, bool) {}
	}
	return tel.StartCall(kind, turn)
}

// childEnv is the environment for a `ynh check` child.
// With nothing to add it is nil, so the child inherits ynh's environment as
// it always has. Otherwise the additions come after the inherited
// environment, so they win over the operator's own TRACEPARENT or YNR_SPOOL.
func childEnv(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	return append(os.Environ(), extra...)
}
