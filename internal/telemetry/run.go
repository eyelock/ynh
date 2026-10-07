package telemetry

import (
	"context"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Run is one `ynh agent run` as a unit of work: a span, with a started event
// when it begins and a finished event when it ends. It is safe for
// concurrent use.
type Run struct {
	t         *Telemetry
	parent    context.Context
	startTime time.Time
	start     []attribute.KeyValue

	mu       sync.Mutex
	ctx      context.Context
	span     trace.Span
	finished bool
}

// StartRun joins the trace named by TRACEPARENT and TRACESTATE, or starts
// one, opens the run's span and emits ynh.run.started. The event is flushed
// to disk before StartRun returns, so a run killed a moment later still
// shows as a start with no finish.
//
// attrs describe the run as requested; they go on the event and the span.
//
// When telemetry starts later, because the spool appeared mid-run, the span
// and the started event are written then, with the run's real start time.
func (t *Telemetry) StartRun(attrs ...attribute.KeyValue) *Run {
	carrier := propagation.MapCarrier{
		"traceparent": os.Getenv("TRACEPARENT"),
		"tracestate":  os.Getenv("TRACESTATE"),
	}
	r := &Run{
		t:         t,
		parent:    propagation.TraceContext{}.Extract(context.Background(), carrier),
		startTime: time.Now(),
		start:     attrs,
	}
	t.mu.Lock()
	t.run = r
	t.mu.Unlock()
	r.activate()
	return r
}

// activate opens the span and emits the started event on the providers in
// force now. Called at start, and again if telemetry starts mid-run.
func (r *Run) activate() {
	st := r.t.state()
	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return
	}
	r.ctx, r.span = st.tracer.Start(r.parent, SpanRun,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithTimestamp(r.startTime),
		trace.WithAttributes(r.start...))
	emit(r.ctx, st.logger, EventRunStarted, r.startTime, r.start)
	r.mu.Unlock()
	r.t.Flush()
}

// WorkerEnv is the environment for the vendor CLI the run starts:
// TRACEPARENT naming the run's span, TRACESTATE when there is one, and
// YNR_SPOOL naming the spool folder, so the worker's own children inherit
// it. It is empty when telemetry is off, so the worker's environment is then
// exactly what it was without telemetry.
func (r *Run) WorkerEnv() []string {
	st := r.t.state()
	if !st.active {
		return nil
	}
	r.mu.Lock()
	ctx := r.ctx
	r.mu.Unlock()
	return childEnv(ctx, st.spoolDir)
}

// StartCall opens a child span of the run, named name, for one call out to
// another process, and returns that process's environment: TRACEPARENT for
// the call's span, TRACESTATE, and YNR_SPOOL. end closes the span with the
// call's outcome; ok false marks a call that could not run. With telemetry
// off it returns no environment and an end that does nothing.
func (r *Run) StartCall(name string, attrs ...attribute.KeyValue) ([]string, func(outcome string, ok bool)) {
	st := r.t.state()
	if !st.active {
		return nil, func(string, bool) {}
	}
	r.mu.Lock()
	parent := r.ctx
	r.mu.Unlock()
	ctx, span := st.tracer.Start(parent, name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...))
	return childEnv(ctx, st.spoolDir), func(outcome string, ok bool) {
		if outcome != "" {
			span.SetAttributes(AttrCallOutcome.String(outcome))
		}
		if ok {
			span.SetStatus(codes.Ok, "")
		} else {
			span.SetStatus(codes.Error, outcome)
		}
		span.End()
	}
}

// childEnv is the trace context of ctx, and the spool folder, as environment
// entries for a child process.
func childEnv(ctx context.Context, spoolDir string) []string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	var env []string
	if v := carrier.Get("traceparent"); v != "" {
		env = append(env, "TRACEPARENT="+v)
	}
	if v := carrier.Get("tracestate"); v != "" {
		env = append(env, "TRACESTATE="+v)
	}
	if spoolDir != "" {
		env = append(env, "YNR_SPOOL="+spoolDir)
	}
	return env
}

// Finish ends the run with its outcome. succeeded sets the span's status to
// ok; otherwise it is an error described by the outcome alone, never by the
// run's error text, which can carry paths. attrs go on the span, and on
// ynh.run.finished beside the started event's, so the finished event stands
// on its own; where both name a key, the end's value wins.
func (r *Run) Finish(outcome string, succeeded bool, attrs ...attribute.KeyValue) {
	st := r.t.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finished = true
	attrs = append([]attribute.KeyValue{AttrOutcome.String(outcome)}, attrs...)
	r.span.SetAttributes(attrs...)
	if succeeded {
		r.span.SetStatus(codes.Ok, "")
	} else {
		r.span.SetStatus(codes.Error, outcome)
	}
	merged := attribute.NewSet(append(append([]attribute.KeyValue{}, r.start...), attrs...)...)
	emit(r.ctx, st.logger, EventRunFinished, time.Now(), merged.ToSlice())
	r.span.End()
}

func emit(ctx context.Context, logger otellog.Logger, name string, at time.Time, attrs []attribute.KeyValue) {
	var rec otellog.Record
	rec.SetEventName(name)
	rec.SetTimestamp(at)
	rec.SetObservedTimestamp(time.Now())
	rec.SetSeverity(otellog.SeverityInfo)
	rec.SetSeverityText("INFO")
	rec.AddAttributes(attrs...)
	logger.Emit(ctx, rec)
}
