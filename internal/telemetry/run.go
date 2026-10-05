package telemetry

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Run is one `ynh agent run` as a unit of work: a span, with a started event
// when it begins and a finished event when it ends.
type Run struct {
	t     *Telemetry
	ctx   context.Context
	span  trace.Span
	start []attribute.KeyValue
}

// StartRun joins the trace named by TRACEPARENT and TRACESTATE, or starts
// one, opens the run's span and emits ynh.run.started. The event is flushed
// to disk before StartRun returns, so a run killed a moment later still
// shows as a start with no finish.
//
// attrs describe the run as requested; they go on the event and the span.
func (t *Telemetry) StartRun(attrs ...attribute.KeyValue) *Run {
	carrier := propagation.MapCarrier{
		"traceparent": os.Getenv("TRACEPARENT"),
		"tracestate":  os.Getenv("TRACESTATE"),
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), carrier)
	ctx, span := t.tracer.Start(ctx, SpanRun,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...))
	r := &Run{t: t, ctx: ctx, span: span, start: attrs}
	r.emit(EventRunStarted, attrs)
	t.Flush()
	return r
}

// WorkerEnv is the trace context for the vendor CLI the run starts:
// TRACEPARENT naming the run's span, and TRACESTATE when there is one. It is
// empty when telemetry is off, so the worker's environment is then exactly
// what it was without telemetry.
func (r *Run) WorkerEnv() []string {
	if !r.t.Active() {
		return nil
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(r.ctx, carrier)
	var env []string
	if v := carrier.Get("traceparent"); v != "" {
		env = append(env, "TRACEPARENT="+v)
	}
	if v := carrier.Get("tracestate"); v != "" {
		env = append(env, "TRACESTATE="+v)
	}
	return env
}

// Finish ends the run with its outcome. succeeded sets the span's status to
// ok; otherwise it is an error described by the outcome alone, never by the
// run's error text, which can carry paths. attrs go on the span, and on
// ynh.run.finished beside the started event's, so the finished event stands
// on its own; where both name a key, the end's value wins.
func (r *Run) Finish(outcome string, succeeded bool, attrs ...attribute.KeyValue) {
	attrs = append([]attribute.KeyValue{AttrOutcome.String(outcome)}, attrs...)
	r.span.SetAttributes(attrs...)
	if succeeded {
		r.span.SetStatus(codes.Ok, "")
	} else {
		r.span.SetStatus(codes.Error, outcome)
	}
	merged := attribute.NewSet(append(append([]attribute.KeyValue{}, r.start...), attrs...)...)
	r.emit(EventRunFinished, merged.ToSlice())
	r.span.End()
}

func (r *Run) emit(name string, attrs []attribute.KeyValue) {
	var rec otellog.Record
	now := time.Now()
	rec.SetEventName(name)
	rec.SetTimestamp(now)
	rec.SetObservedTimestamp(now)
	rec.SetSeverity(otellog.SeverityInfo)
	rec.SetSeverityText("INFO")
	rec.AddAttributes(attrs...)
	r.t.logger.Emit(r.ctx, rec)
}
