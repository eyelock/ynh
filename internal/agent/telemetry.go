package agent

import "os"

// Kinds of call the loop makes to another process, for Telemetry.StartCall.
const (
	// CallCheck is the `ynh check` the loop runs after each turn.
	CallCheck = "check"
	// CallSensor is the `ynh sensors run` for the convergence verifier.
	CallSensor = "sensor"
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
	// StartCall opens a span for one call of kind (CallCheck or CallSensor)
	// on turn, naming sensor for CallSensor. It returns the environment for
	// the process it starts, and a function that ends the span with the
	// call's outcome; ok is false when the call itself could not run.
	StartCall(kind string, turn int, sensor string) (env []string, end func(outcome string, ok bool))
}

// startCall is Telemetry.StartCall for a Telemetry that may be nil.
func startCall(tel Telemetry, kind string, turn int, sensor string) ([]string, func(string, bool)) {
	if tel == nil {
		return nil, func(string, bool) {}
	}
	return tel.StartCall(kind, turn, sensor)
}

// childEnv is the environment for a `ynh check` or `ynh sensors run` child.
// With nothing to add it is nil, so the child inherits ynh's environment as
// it always has. Otherwise the additions come after the inherited
// environment, so they win over the operator's own TRACEPARENT or YNR_SPOOL.
func childEnv(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	return append(os.Environ(), extra...)
}
