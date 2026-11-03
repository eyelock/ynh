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
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	spool "github.com/eyelock/ynr/spoolexporter"
)

// Destination is where telemetry goes, chosen once at process start.
type Destination string

const (
	// DestinationNone writes nothing: no spool and no OTLP endpoint.
	DestinationNone Destination = "none"
	// DestinationSpool writes OTLP JSON lines into a spool folder.
	DestinationSpool Destination = "spool"
	// DestinationOTLP is an operator's OTEL_EXPORTER_OTLP_* endpoint, sent to
	// over OTLP/HTTP with JSON bodies. A setting that exporter cannot honour
	// (grpc, custom certificates, compression) writes nothing and says so.
	DestinationOTLP Destination = "otlp"
)

// FlushTimeout bounds every flush to disk, including the one on exit.
const FlushTimeout = 2 * time.Second

// ChooseDestination applies the contract's order: the operator's
// OTEL_EXPORTER_OTLP_* (an endpoint is set, or a setting the exporter cannot
// honour, which is still the operator's choice), then YNR_SPOOL, then the laptop default
// $XDG_STATE_HOME/ynr/spool/local if that folder exists, then nothing. It
// returns the spool folder, made absolute, for DestinationSpool.
func ChooseDestination() (Destination, string) {
	if _, configured, unsupported := spool.HTTPOptionsFromEnv(os.Getenv); configured || unsupported != "" {
		return DestinationOTLP, ""
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
	// Labels are the run's labels, already validated (internal/labels).
	// Each becomes the resource attribute ynh.label.<key>, on every record
	// ynh writes, so ynr can group by them (ynr ADR-006 rule 10).
	Labels map[string]string
}

// Telemetry holds the providers for one process. It is safe for concurrent
// use: the spool can appear, and telemetry start, while a run is under way.
type Telemetry struct {
	version string
	stderr  io.Writer
	opts    Options
	errors  atomic.Int64
	// abandoned is set once a flush has run out of time: the destination is
	// stuck, so later flushes give up at once rather than wait again, and the
	// process exits within one FlushTimeout in all.
	abandoned atomic.Bool

	mu       sync.Mutex
	dest     Destination
	spoolDir string
	tracer   trace.Tracer
	logger   otellog.Logger
	tp       *sdktrace.TracerProvider
	lp       *sdklog.LoggerProvider
	writer   *spool.Writer
	sender   *spool.HTTPSender
	run      *Run
	stop     chan struct{}
	stopped  bool
}

// Setup chooses a destination and builds the providers for it. It never
// fails: anything that goes wrong leaves telemetry off.
//
// stderr receives one note when the operator's OTLP settings include one the
// exporter cannot honour (naming the setting, never a value), since the spool
// would otherwise appear to have been skipped for no reason.
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
		stderr:  stderr,
		opts:    opts,
		dest:    dest,
		tracer:  tracenoop.NewTracerProvider().Tracer(ScopeName),
		logger:  lognoop.NewLoggerProvider().Logger(ScopeName),
	}
	switch dest {
	case DestinationOTLP:
		// The operator chose; ynh does not look for a spool behind them.
		opts, _, unsupported := spool.HTTPOptionsFromEnv(os.Getenv)
		if unsupported != "" {
			_, _ = fmt.Fprintf(stderr, "ynh: %s; no telemetry is written for this run\n", unsupported)
		} else {
			t.activate("", spool.NewHTTPSender(opts))
		}
	case DestinationSpool:
		t.activate(dir, nil)
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

const resourceEnv = "OTEL_RESOURCE_ATTRIBUTES"

// resourceEnvMu serialises the brief rewrite of resourceEnv in activate.
var resourceEnvMu sync.Mutex

// stripLabelAttrs removes every ynh.label.* pair from an
// OTEL_RESOURCE_ATTRIBUTES value, and returns the rest and the dropped keys,
// sorted. Values are never returned.
func stripLabelAttrs(raw string) (string, []string) {
	var kept, dropped []string
	for _, pair := range strings.Split(raw, ",") {
		key, _, _ := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if k, err := url.PathUnescape(key); err == nil {
			key = k
		}
		if strings.HasPrefix(key, string(AttrLabel)+".") {
			dropped = append(dropped, key)
			continue
		}
		kept = append(kept, pair)
	}
	sort.Strings(dropped)
	return strings.Join(kept, ","), dropped
}

// LabelAttributes are the run's labels as resource attributes,
// ynh.label.<key>, sorted by key.
func LabelAttributes(labels map[string]string) []attribute.KeyValue {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]attribute.KeyValue, 0, len(keys))
	for _, k := range keys {
		out = append(out, attribute.String(string(AttrLabel)+"."+k, labels[k]))
	}
	return out
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
				t.activate(dir, nil)
				return
			}
		}
	}
}

// activate builds the providers and brings a run already under way onto
// them. They write to the spool folder dir, or, when sender is not nil, post
// to the operator's OTLP endpoint and write no spool.
func (t *Telemetry) activate(dir string, sender *spool.HTTPSender) {
	instanceID := newInstanceID()
	// The operator's OTEL_RESOURCE_ATTRIBUTES are honoured, but ynh's own
	// identity wins: service.name and service.version say which registry
	// describes these records.
	attrs := []attribute.KeyValue{
		semconv.ServiceName("ynh"),
		semconv.ServiceVersion(t.version),
		semconv.ServiceInstanceID(instanceID),
	}
	// The labels are ynh's too. Only these were validated (ynr ADR-006 rule
	// 10), so every ynh.label.* the operator put in OTEL_RESOURCE_ATTRIBUTES,
	// under the same key or another, is dropped: it could be a path or a
	// 17th label. Dropped, not refused: the variable is the operator's
	// environment, not a ynh flag, and telemetry never fails a run.
	attrs = append(attrs, LabelAttributes(t.opts.Labels)...)
	raw, hadEnv := os.LookupEnv(resourceEnv)
	kept, dropped := stripLabelAttrs(raw)
	if len(dropped) > 0 {
		if t.stderr != nil {
			_, _ = fmt.Fprintf(t.stderr, "ynh: dropped %s from OTEL_RESOURCE_ATTRIBUTES: run labels are set with --label, YNH_LABELS, config.json or the manifest\n", strings.Join(dropped, ", "))
		}
		// The SDK merges the process environment into every provider's
		// resource, so the operator's labels would come back. Hide them while
		// the providers are built, and put the variable back as it was.
		resourceEnvMu.Lock()
		_ = os.Setenv(resourceEnv, kept)
		defer func() {
			if hadEnv {
				_ = os.Setenv(resourceEnv, raw)
			} else {
				_ = os.Unsetenv(resourceEnv)
			}
			resourceEnvMu.Unlock()
		}()
	}
	res, err := resource.New(context.Background(),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		// A partial resource is still returned; an unparsable
		// OTEL_RESOURCE_ATTRIBUTES must not cost the run its telemetry.
		t.errors.Add(1)
	}
	var (
		writer  *spool.Writer
		traceEx *spool.TraceExporter
		logEx   *spool.LogExporter
	)
	if sender != nil {
		traceEx, logEx = spool.NewHTTPTraceExporter(sender), spool.NewHTTPLogExporter(sender)
	} else {
		writer = spool.NewWriter(spool.Options{
			Dir:          dir,
			Service:      "ynh",
			InstanceID:   instanceID,
			MaxFileBytes: t.opts.MaxFileBytes,
			MaxBytes:     t.opts.MaxBytes,
			SyncTimeout:  FlushTimeout,
		})
		traceEx, logEx = spool.NewTraceExporter(writer), spool.NewLogExporter(writer)
	}

	t.mu.Lock()
	if t.stopped {
		// Shut down while the spool was being found: write nothing.
		t.mu.Unlock()
		if writer != nil {
			writer.Close()
		}
		return
	}
	// SDK errors are counted, never printed: telemetry must not change what
	// the operator sees.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) { t.errors.Add(1) }))
	t.dest, t.spoolDir, t.writer, t.sender = DestinationSpool, dir, writer, sender
	if sender != nil {
		t.dest = DestinationOTLP
	}
	t.tp = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceEx,
			sdktrace.WithBatchTimeout(time.Second),
			sdktrace.WithExportTimeout(FlushTimeout)),
	)
	t.lp = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logEx,
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

// SpoolDir is the spool folder telemetry writes to, absolute, or "" when
// the destination is not the spool.
func (t *Telemetry) SpoolDir() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.spoolDir
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
	w, h := t.writer, t.sender
	t.mu.Unlock()
	if w != nil {
		n += w.Stats().Errors
	}
	if h != nil {
		n += h.Stats().Errors
	}
	return n
}

// Dropped counts records the spool could not take, or the endpoint was not
// sent.
func (t *Telemetry) Dropped() int64 {
	t.mu.Lock()
	w, h := t.writer, t.sender
	t.mu.Unlock()
	var n int64
	if w != nil {
		n += w.Stats().Dropped
	}
	if h != nil {
		n += h.Stats().Dropped
	}
	return n
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
		if w != nil {
			w.Sync()
		}
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
		if w != nil {
			w.Close()
		}
	})
}

// bounded runs f, returning at FlushTimeout whether or not it has finished.
func (t *Telemetry) bounded(f func(ctx context.Context)) {
	if t.abandoned.Load() {
		return
	}
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
		t.abandoned.Store(true)
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
