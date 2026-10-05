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
	"sync"
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
// returns the spool folder, made absolute, for DestinationSpool.
func ChooseDestination() (Destination, string) {
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, otlpEnvPrefix) && value != "" {
			return DestinationOTLP, ""
		}
	}
	if dir := os.Getenv("YNR_SPOOL"); dir != "" {
		// Absolute, because children are handed this folder and run in
		// other directories.
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
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

// RecheckInterval is how often a process that found no destination looks
// again for the spool folder (ynr ADR-004).
const RecheckInterval = time.Minute

// Options tunes the spool. Zero values take the defaults; tests use them to
// make a cap reachable and a recheck quick.
type Options struct {
	MaxFileBytes    int64
	MaxBytes        int64
	RecheckInterval time.Duration
}

// Telemetry holds the providers for one process. It is safe for concurrent
// use: the spool can appear, and telemetry start, while a run is under way.
type Telemetry struct {
	version string
	opts    Options
	errors  atomic.Int64

	mu       sync.Mutex
	dest     Destination
	spoolDir string
	tracer   trace.Tracer
	logger   otellog.Logger
	tp       *sdktrace.TracerProvider
	lp       *sdklog.LoggerProvider
	writer   *spool.Writer
	run      *Run
	stop     chan struct{}
	stopped  bool
}

// Setup chooses a destination and builds the providers for it. It never
// fails: anything that goes wrong leaves telemetry off.
//
// stderr receives one note when the operator's OTLP settings are present,
// since this build cannot honour them and the spool would otherwise appear
// to have been skipped for no reason.
//
// With no destination at all, Setup keeps looking for the spool folder every
// RecheckInterval, in the background, and starts writing when it appears
// (ADR-004: a long-lived process that found no spool checks again once a
// minute). An agent run can last an hour; a `ynr serve` started after it
// still receives it. Shutdown stops the search.
func Setup(version string, opts Options, stderr io.Writer) *Telemetry {
	dest, dir := ChooseDestination()
	t := &Telemetry{
		version: version,
		opts:    opts,
		dest:    dest,
		tracer:  tracenoop.NewTracerProvider().Tracer(ScopeName),
		logger:  lognoop.NewLoggerProvider().Logger(ScopeName),
	}
	switch dest {
	case DestinationOTLP:
		// The operator chose; ynh does not look for a spool behind them.
		_, _ = fmt.Fprintln(stderr, "ynh: OTEL_EXPORTER_OTLP_* is set, but this ynh cannot export over OTLP yet; no telemetry is written for this run")
	case DestinationSpool:
		t.activate(dir)
	case DestinationNone:
		interval := opts.RecheckInterval
		if interval <= 0 {
			interval = RecheckInterval
		}
		t.stop = make(chan struct{})
		go t.recheck(interval)
	}
	return t
}

// recheck looks for the spool every interval until it appears or Shutdown.
func (t *Telemetry) recheck(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			if dest, dir := ChooseDestination(); dest == DestinationSpool {
				t.activate(dir)
				return
			}
		}
	}
}

// activate builds the providers writing to the spool folder dir, and brings
// a run already under way onto them.
func (t *Telemetry) activate(dir string) {
	instanceID := newInstanceID()
	// The operator's OTEL_RESOURCE_ATTRIBUTES are honoured, but ynh's own
	// identity wins: service.name and service.version say which registry
	// describes these records.
	res, err := resource.New(context.Background(),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithAttributes(
			semconv.ServiceName("ynh"),
			semconv.ServiceVersion(t.version),
			semconv.ServiceInstanceID(instanceID),
		),
	)
	if err != nil {
		// A partial resource is still returned; an unparsable
		// OTEL_RESOURCE_ATTRIBUTES must not cost the run its telemetry.
		t.errors.Add(1)
	}
	writer := spool.NewWriter(spool.Options{
		Dir:          dir,
		Service:      "ynh",
		InstanceID:   instanceID,
		MaxFileBytes: t.opts.MaxFileBytes,
		MaxBytes:     t.opts.MaxBytes,
		SyncTimeout:  FlushTimeout,
	})

	t.mu.Lock()
	if t.stopped {
		// Shut down while the spool was being found: write nothing.
		t.mu.Unlock()
		return
	}
	// SDK errors are counted, never printed: telemetry must not change what
	// the operator sees.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) { t.errors.Add(1) }))
	t.dest, t.spoolDir, t.writer = DestinationSpool, dir, writer
	t.tp = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(spool.NewTraceExporter(writer),
			sdktrace.WithBatchTimeout(time.Second),
			sdktrace.WithExportTimeout(FlushTimeout)),
	)
	t.lp = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(spool.NewLogExporter(writer),
			sdklog.WithExportInterval(time.Second),
			sdklog.WithExportTimeout(FlushTimeout))),
	)
	t.tracer = t.tp.Tracer(ScopeName, trace.WithInstrumentationVersion(t.version))
	t.logger = t.lp.Logger(ScopeName, otellog.WithInstrumentationVersion(t.version))
	run := t.run
	t.mu.Unlock()

	if run != nil {
		run.activate()
	}
}

// Destination reports where telemetry is going now.
func (t *Telemetry) Destination() Destination {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dest
}

// Active reports whether anything is written.
func (t *Telemetry) Active() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tp != nil
}

// state is a consistent snapshot of the providers.
type state struct {
	tracer   trace.Tracer
	logger   otellog.Logger
	spoolDir string
	active   bool
}

func (t *Telemetry) state() state {
	t.mu.Lock()
	defer t.mu.Unlock()
	return state{tracer: t.tracer, logger: t.logger, spoolDir: t.spoolDir, active: t.tp != nil}
}

// Errors counts what went wrong in telemetry: SDK errors plus the spool's
// failed and abandoned writes. None of them is ever reported to the run.
func (t *Telemetry) Errors() int64 {
	n := t.errors.Load()
	t.mu.Lock()
	w := t.writer
	t.mu.Unlock()
	if w != nil {
		n += w.Stats().Errors
	}
	return n
}

// Dropped counts records the spool could not take.
func (t *Telemetry) Dropped() int64 {
	t.mu.Lock()
	w := t.writer
	t.mu.Unlock()
	if w == nil {
		return 0
	}
	return w.Stats().Dropped
}

// Flush writes everything pending and flushes it to disk, within
// FlushTimeout.
func (t *Telemetry) Flush() {
	t.mu.Lock()
	tp, lp, w := t.tp, t.lp, t.writer
	t.mu.Unlock()
	if tp == nil {
		return
	}
	t.bounded(func(ctx context.Context) {
		_ = tp.ForceFlush(ctx)
		_ = lp.ForceFlush(ctx)
		w.Sync()
	})
}

// Shutdown stops looking for a spool, then flushes, closes the spool file
// and stops the providers, within FlushTimeout. Past it, whatever is left is
// abandoned.
func (t *Telemetry) Shutdown() {
	t.mu.Lock()
	if !t.stopped {
		t.stopped = true
		if t.stop != nil {
			close(t.stop)
		}
	}
	tp, lp, w := t.tp, t.lp, t.writer
	t.mu.Unlock()
	if tp == nil {
		return
	}
	t.bounded(func(ctx context.Context) {
		_ = tp.Shutdown(ctx)
		_ = lp.Shutdown(ctx)
		w.Close()
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
