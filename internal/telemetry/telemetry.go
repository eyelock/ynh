// Package telemetry sets up OpenTelemetry for `ynh agent run`, the one ynh
// command that lives long enough to report, under the sibling
// instrumentation contract (ynr ADR-006). See docs/telemetry.md.
//
// Nothing here may change what a run does. Setup never fails, every flush is
// bounded, and with no destination it returns no-op providers and the run
// is exactly as it was without telemetry.
package telemetry

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	otellog "go.opentelemetry.io/otel/log"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/eyelock/ynh/internal/telemetry/spool"
)

// Destination is where telemetry goes, chosen once at process start.
type Destination string

const (
	// DestinationNone writes nothing: no spool and no OTLP endpoint.
	DestinationNone Destination = "none"
	// DestinationSpool writes OTLP JSON lines into a spool folder.
	DestinationSpool Destination = "spool"
	// DestinationOTLP is an operator's OTEL_EXPORTER_OTLP_* endpoint. This
	// build does not export over OTLP, so it writes nothing and says so.
	DestinationOTLP Destination = "otlp"
)

// FlushTimeout bounds every flush to disk, including the one on exit.
const FlushTimeout = 2 * time.Second

// otlpEnvPrefix marks the operator's OTLP exporter settings.
const otlpEnvPrefix = "OTEL_EXPORTER_OTLP_"

// ChooseDestination applies the contract's order: the operator's
// OTEL_EXPORTER_OTLP_*, then YNR_SPOOL, then the laptop default
// $XDG_STATE_HOME/ynr/spool/local if that folder exists, then nothing. It
// returns the spool folder for DestinationSpool.
func ChooseDestination() (Destination, string) {
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, otlpEnvPrefix) && value != "" {
			return DestinationOTLP, ""
		}
	}
	if dir := os.Getenv("YNR_SPOOL"); dir != "" {
		return DestinationSpool, dir
	}
	if dir := defaultSpool(); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return DestinationSpool, dir
		}
	}
	return DestinationNone, ""
}

// defaultSpool is $XDG_STATE_HOME/ynr/spool/local. The XDG specification
// ignores a relative XDG_STATE_HOME, and so does this.
func defaultSpool() string {
	state := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(state) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "ynr", "spool", "local")
}

// Options tunes the spool. Zero values take the spool format's defaults;
// tests use them to make a cap reachable.
type Options struct {
	MaxFileBytes int64
	MaxBytes     int64
}

// Telemetry holds the providers for one process.
type Telemetry struct {
	Destination Destination

	tracer trace.Tracer
	logger otellog.Logger
	tp     *sdktrace.TracerProvider
	lp     *sdklog.LoggerProvider
	writer *spool.Writer
	errors atomic.Int64
}

// Setup chooses a destination and builds the providers for it. It never
// fails: anything that goes wrong leaves telemetry off.
//
// stderr receives one note when the operator's OTLP settings are present,
// since this build cannot honour them and the spool would otherwise appear
// to have been skipped for no reason.
func Setup(version string, opts Options, stderr io.Writer) *Telemetry {
	dest, dir := ChooseDestination()
	t := &Telemetry{
		Destination: dest,
		tracer:      tracenoop.NewTracerProvider().Tracer(ScopeName),
		logger:      lognoop.NewLoggerProvider().Logger(ScopeName),
	}
	switch dest {
	case DestinationOTLP:
		_, _ = fmt.Fprintln(stderr, "ynh: OTEL_EXPORTER_OTLP_* is set, but this ynh cannot export over OTLP yet; no telemetry is written for this run")
		return t
	case DestinationNone:
		return t
	}

	instanceID := newInstanceID()
	// The operator's OTEL_RESOURCE_ATTRIBUTES are honoured, but ynh's own
	// identity wins: service.name and service.version say which registry
	// describes these records.
	res, err := resource.New(context.Background(),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithAttributes(
			semconv.ServiceName("ynh"),
			semconv.ServiceVersion(version),
			semconv.ServiceInstanceID(instanceID),
		),
	)
	if err != nil {
		// A partial resource is still returned; an unparsable
		// OTEL_RESOURCE_ATTRIBUTES must not cost the run its telemetry.
		t.errors.Add(1)
	}

	t.writer = spool.NewWriter(spool.Options{
		Dir:          dir,
		Service:      "ynh",
		InstanceID:   instanceID,
		MaxFileBytes: opts.MaxFileBytes,
		MaxBytes:     opts.MaxBytes,
		SyncTimeout:  FlushTimeout,
	})
	// SDK errors are counted, never printed: telemetry must not change what
	// the operator sees.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) { t.errors.Add(1) }))

	t.tp = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(spool.NewTraceExporter(t.writer),
			sdktrace.WithBatchTimeout(time.Second),
			sdktrace.WithExportTimeout(FlushTimeout)),
	)
	t.lp = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(spool.NewLogExporter(t.writer),
			sdklog.WithExportInterval(time.Second),
			sdklog.WithExportTimeout(FlushTimeout))),
	)
	t.tracer = t.tp.Tracer(ScopeName, trace.WithInstrumentationVersion(version))
	t.logger = t.lp.Logger(ScopeName, otellog.WithInstrumentationVersion(version))
	return t
}

// Active reports whether anything is written.
func (t *Telemetry) Active() bool { return t.tp != nil }

// Errors counts what went wrong in telemetry: SDK errors plus the spool's
// failed and abandoned writes. None of them is ever reported to the run.
func (t *Telemetry) Errors() int64 {
	n := t.errors.Load()
	if t.writer != nil {
		n += t.writer.Stats().Errors
	}
	return n
}

// Dropped counts records the spool could not take.
func (t *Telemetry) Dropped() int64 {
	if t.writer == nil {
		return 0
	}
	return t.writer.Stats().Dropped
}

// Flush writes everything pending and flushes it to disk, within
// FlushTimeout.
func (t *Telemetry) Flush() {
	if !t.Active() {
		return
	}
	t.bounded(func(ctx context.Context) {
		_ = t.tp.ForceFlush(ctx)
		_ = t.lp.ForceFlush(ctx)
		t.writer.Sync()
	})
}

// Shutdown flushes, closes the spool file and stops the providers, within
// FlushTimeout. Past it, whatever is left is abandoned.
func (t *Telemetry) Shutdown() {
	if !t.Active() {
		return
	}
	t.bounded(func(ctx context.Context) {
		_ = t.tp.Shutdown(ctx)
		_ = t.lp.Shutdown(ctx)
		t.writer.Close()
	})
}

// bounded runs f, returning at FlushTimeout whether or not it has finished.
func (t *Telemetry) bounded(f func(ctx context.Context)) {
	ctx, cancel := context.WithTimeout(context.Background(), FlushTimeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		f(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.errors.Add(1)
	}
}

// newInstanceID returns a random UUID (version 4) for service.instance.id.
func newInstanceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
