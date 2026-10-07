package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// The names ynh emits, in one place.
//
// DRAFT. Every ynh.* name here is provisional until ynh's Weaver registry
// exists (ynr ADR-007), when these constants will be generated from it and
// may change. Standard names come from the semantic conventions pinned below.
//
// SemconvVersion is the semantic-conventions version ynh follows for the
// standard names. It is 1.41.0, not the SDK's newer default, because 1.41.0 is
// the last version whose Go package carries the gen_ai.* names, which are
// still in development upstream.
const SemconvVersion = "1.41.0"

// Events and spans.
const (
	// EventRunStarted is emitted when `ynh agent run` begins, so a run that
	// crashes shows as a start with no finish.
	EventRunStarted = "ynh.run.started"
	// EventRunFinished is emitted when the run ends, beside its span.
	EventRunFinished = "ynh.run.finished"
	// SpanRun is the one span per `ynh agent run`.
	SpanRun = "ynh.run"
	// SpanCheck is a child of the run for each `ynh check` between turns.
	SpanCheck = "ynh.check"
	// SpanSensorRun is a child of the run for each `ynh sensors run` of the
	// convergence verifier.
	SpanSensorRun = "ynh.sensors.run"
	// ScopeName is the instrumentation scope of everything ynh emits.
	ScopeName = "github.com/eyelock/ynh"
)

// ynh's own attributes (draft).
const (
	AttrOutcome         attribute.Key = "ynh.run.outcome"
	AttrBoundBy         attribute.Key = "ynh.run.bound_by"
	AttrTurns           attribute.Key = "ynh.run.turns"
	AttrCostUSD         attribute.Key = "ynh.run.cost_usd"
	AttrSessionID       attribute.Key = "ynh.run.session_id"
	AttrVendor          attribute.Key = "ynh.run.vendor"
	AttrEffort          attribute.Key = "ynh.run.effort"
	AttrEffortRequested attribute.Key = "ynh.run.effort.requested"
	AttrFocus           attribute.Key = "ynh.run.focus"
	AttrProfile         attribute.Key = "ynh.run.profile"
	AttrResumed         attribute.Key = "ynh.run.resumed"
	AttrHarnessName     attribute.Key = "ynh.harness.name"
	AttrHarnessVersion  attribute.Key = "ynh.harness.version"
	AttrHarnessCommit   attribute.Key = "ynh.harness.commit"
	// On the spans for calls out: the turn they follow, the sensor a
	// sensors run is for, and the call's outcome (the gate's verdict or the
	// sensor's status word, or "error" when the call could not run).
	AttrTurn        attribute.Key = "ynh.run.turn"
	AttrSensorName  attribute.Key = "ynh.sensor.name"
	AttrCallOutcome attribute.Key = "ynh.call.outcome"
)

// Standard attributes, from the pinned semantic conventions.
const (
	AttrExitCode            = semconv.ProcessExitCodeKey
	AttrModelRequested      = semconv.GenAIRequestModelKey
	AttrModel               = semconv.GenAIResponseModelKey
	AttrInputTokens         = semconv.GenAIUsageInputTokensKey
	AttrOutputTokens        = semconv.GenAIUsageOutputTokensKey
	AttrCacheReadTokens     = semconv.GenAIUsageCacheReadInputTokensKey
	AttrCacheCreationTokens = semconv.GenAIUsageCacheCreationInputTokensKey
)
