package spool

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// spoolFiles lists the files in dir, sorted.
func spoolFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// readLines decodes every line of every file in dir. Each line must be a
// complete JSON object.
func readLines(t *testing.T, dir string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, name := range spoolFiles(t, dir) {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Fatalf("%s: line is not JSON: %v\n%s", name, err, sc.Text())
			}
			out = append(out, m)
		}
		_ = f.Close()
	}
	return out
}

// dig walks a decoded JSON value by keys and array indexes.
func dig(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("at %q: not an object: %v", k, v)
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k >= len(a) {
				t.Fatalf("at [%d]: not an array of that length: %v", k, v)
			}
			v = a[k]
		}
	}
	return v
}

// attrMap flattens an OTLP attribute list to key -> value object.
func attrMap(t *testing.T, v any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	list, _ := v.([]any)
	for _, item := range list {
		kv := item.(map[string]any)
		out[kv["key"].(string)] = kv["value"].(map[string]any)
	}
	return out
}

func TestExporters_WriteOTLPJSON(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(Options{Dir: dir, Service: "svc", InstanceID: "inst"})
	res := resource.NewSchemaless(attribute.String("service.name", "svc"))
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res),
		sdktrace.WithSyncer(NewTraceExporter(w)))
	lp := sdklog.NewLoggerProvider(sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewSimpleProcessor(NewLogExporter(w))))

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	parentID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: parentID, TraceFlags: trace.FlagsSampled, Remote: true,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
	ctx, span := tp.Tracer("scope", trace.WithInstrumentationVersion("1.2.3")).Start(ctx, "unit")
	span.SetAttributes(
		attribute.String("s", "v"),
		attribute.Int64("i", 42),
		attribute.Float64("f", 1.5),
		attribute.Bool("b", true),
		attribute.StringSlice("ss", []string{"a", "b"}),
	)
	span.AddEvent("ev", trace.WithAttributes(attribute.Int("n", 1)))
	span.SetStatus(codes.Error, "outcome")

	var rec otellog.Record
	rec.SetEventName("unit.started")
	rec.SetSeverity(otellog.SeverityInfo)
	rec.SetTimestamp(time.Unix(1700000000, 5))
	rec.SetBody(attribute.StringValue("body"))
	rec.AddAttributes(attribute.Int64("count", 7))
	lp.Logger("scope").Emit(ctx, rec)

	span.End()
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lp.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Close()

	if got := spoolFiles(t, dir); len(got) != 1 || got[0] != "svc-inst-000001.jsonl" {
		t.Fatalf("files = %v, want one closed file svc-inst-000001.jsonl", got)
	}
	lines := readLines(t, dir)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (one log request, one trace request)", len(lines))
	}

	logs, spans := lines[0], lines[1]
	if _, ok := logs["resourceLogs"]; !ok {
		logs, spans = spans, logs
	}

	rec0 := dig(t, logs, "resourceLogs", 0, "scopeLogs", 0, "logRecords", 0).(map[string]any)
	if rec0["eventName"] != "unit.started" || rec0["traceId"] != traceID.String() ||
		rec0["spanId"] != span.SpanContext().SpanID().String() {
		t.Errorf("log record = %v, want the event name and the span's ids", rec0)
	}
	if rec0["timeUnixNano"] != "1700000000000000005" || rec0["severityNumber"] != float64(9) {
		t.Errorf("time %v severity %v, want a decimal string and 9", rec0["timeUnixNano"], rec0["severityNumber"])
	}
	if got := attrMap(t, rec0["attributes"])["count"]["intValue"]; got != "7" {
		t.Errorf("int attribute = %v, want the string \"7\"", got)
	}
	if dig(t, logs, "resourceLogs", 0, "scopeLogs", 0, "scope", "name") != "scope" {
		t.Error("log scope name missing")
	}

	sp := dig(t, spans, "resourceSpans", 0, "scopeSpans", 0, "spans", 0).(map[string]any)
	if sp["traceId"] != traceID.String() || sp["parentSpanId"] != parentID.String() {
		t.Errorf("span ids = %v / %v, want the remote parent's trace and span", sp["traceId"], sp["parentSpanId"])
	}
	if sp["flags"] != float64(0x301) {
		t.Errorf("flags = %v, want sampled with a remote parent (0x301)", sp["flags"])
	}
	if sp["kind"] != float64(1) || sp["name"] != "unit" {
		t.Errorf("kind %v name %v, want internal (1) and unit", sp["kind"], sp["name"])
	}
	if st := sp["status"].(map[string]any); st["code"] != float64(2) || st["message"] != "outcome" {
		t.Errorf("status = %v, want OTLP error (2) with the outcome", st)
	}
	if _, ok := sp["startTimeUnixNano"].(string); !ok {
		t.Errorf("startTimeUnixNano = %v, want a string", sp["startTimeUnixNano"])
	}
	attrs := attrMap(t, sp["attributes"])
	if attrs["i"]["intValue"] != "42" || attrs["f"]["doubleValue"] != 1.5 || attrs["b"]["boolValue"] != true ||
		attrs["s"]["stringValue"] != "v" {
		t.Errorf("span attributes = %v", attrs)
	}
	if got := dig(t, attrs["ss"], "arrayValue", "values", 1, "stringValue"); got != "b" {
		t.Errorf("string slice = %v", attrs["ss"])
	}
	if dig(t, sp, "events", 0, "name") != "ev" {
		t.Error("span event missing")
	}
	if dig(t, spans, "resourceSpans", 0, "scopeSpans", 0, "scope", "version") != "1.2.3" {
		t.Error("scope version missing")
	}
	resAttrs := attrMap(t, dig(t, spans, "resourceSpans", 0, "resource", "attributes"))
	if resAttrs["service.name"]["stringValue"] != "svc" {
		t.Errorf("resource = %v", resAttrs)
	}
	if st := w.Stats(); st != (Stats{}) {
		t.Errorf("stats = %+v, want nothing dropped or failed", st)
	}
}

func TestEncodeValue(t *testing.T) {
	tests := []struct {
		name string
		v    attribute.Value
		want string
	}{
		{"empty", attribute.Value{}, `{}`},
		{"zero double", attribute.Float64Value(0), `{"doubleValue":0}`},
		{"NaN", attribute.Float64Value(math.NaN()), `{"doubleValue":"NaN"}`},
		{"infinity", attribute.Float64Value(math.Inf(1)), `{"doubleValue":"Infinity"}`},
		{"negative infinity", attribute.Float64Value(math.Inf(-1)), `{"doubleValue":"-Infinity"}`},
		{"empty string", attribute.StringValue(""), `{"stringValue":""}`},
		{"bytes", attribute.ByteSliceValue([]byte("hi")), `{"bytesValue":"aGk="}`},
		{"bool slice", attribute.BoolSliceValue([]bool{true}), `{"arrayValue":{"values":[{"boolValue":true}]}}`},
		{"int slice", attribute.Int64SliceValue([]int64{-1}), `{"arrayValue":{"values":[{"intValue":"-1"}]}}`},
		{"float slice", attribute.Float64SliceValue([]float64{0.5}), `{"arrayValue":{"values":[{"doubleValue":0.5}]}}`},
		{"slice", attribute.SliceValue(attribute.StringValue("x")), `{"arrayValue":{"values":[{"stringValue":"x"}]}}`},
		{"map", attribute.MapValue(attribute.Int("k", 1)), `{"kvlistValue":{"values":[{"key":"k","value":{"intValue":"1"}}]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(encodeValue(tt.v))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tt.want {
				t.Errorf("got %s, want %s", data, tt.want)
			}
		})
	}
}

func TestWriter_RotatesAtFileSize(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(Options{Dir: dir, Service: "svc", InstanceID: "inst", MaxFileBytes: 100})
	line := []byte(strings.Repeat("x", 39) + "\n") // 40 bytes: two lines per file
	for range 5 {
		w.writeLine(line, 1)
	}
	got := spoolFiles(t, dir)
	want := []string{"svc-inst-000001.jsonl", "svc-inst-000002.jsonl", "svc-inst-000003.open.jsonl"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %v, want %v", got, want)
	}
	w.Close()
	got = spoolFiles(t, dir)
	if got[2] != "svc-inst-000003.jsonl" {
		t.Errorf("after Close files = %v, want the open file renamed to .jsonl", got)
	}
	for _, name := range got {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 100 {
			t.Errorf("%s is %d bytes, over the 100-byte file cap", name, info.Size())
		}
	}
	if st := w.Stats(); st != (Stats{}) {
		t.Errorf("stats = %+v, want nothing dropped", st)
	}
}

func TestWriter_CapDropsAndCounts(t *testing.T) {
	tests := []struct {
		name        string
		lines       int
		records     int
		maxBytes    int64
		wantDropped int64
		wantOnDisk  int64
	}{
		{name: "under the cap", lines: 3, records: 2, maxBytes: 200, wantDropped: 0, wantOnDisk: 120},
		{name: "over the cap", lines: 5, records: 2, maxBytes: 100, wantDropped: 6, wantOnDisk: 80},
		{name: "a line larger than a file", lines: 1, records: 4, maxBytes: 10, wantDropped: 4, wantOnDisk: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			w := NewWriter(Options{Dir: dir, Service: "s", InstanceID: "i", MaxFileBytes: 50, MaxBytes: tt.maxBytes})
			line := []byte(strings.Repeat("x", 39) + "\n")
			for range tt.lines {
				w.writeLine(line, tt.records)
			}
			w.Close()
			if got := w.Stats().Dropped; got != tt.wantDropped {
				t.Errorf("dropped = %d, want %d", got, tt.wantDropped)
			}
			var total int64
			for _, name := range spoolFiles(t, dir) {
				info, err := os.Stat(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				total += info.Size()
			}
			if total != tt.wantOnDisk {
				t.Errorf("on disk = %d bytes, want %d", total, tt.wantOnDisk)
			}
		})
	}
}

// A closed file the reader has collected stops counting against the cap.
func TestWriter_CapFreedByReader(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(Options{Dir: dir, Service: "s", InstanceID: "i", MaxFileBytes: 40, MaxBytes: 80})
	line := []byte(strings.Repeat("x", 39) + "\n")
	w.writeLine(line, 1)
	w.writeLine(line, 1)
	w.writeLine(line, 1) // over the cap
	if got := w.Stats().Dropped; got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
	// The reader collects the first, closed file.
	if err := os.Remove(filepath.Join(dir, "s-i-000001.jsonl")); err != nil {
		t.Fatal(err)
	}
	w.writeLine(line, 1)
	if got := w.Stats().Dropped; got != 1 {
		t.Errorf("dropped = %d after the reader freed space, want still 1", got)
	}
	w.Close()
}

// An unusable folder costs counted errors and dropped records, never a
// failure or a panic.
func TestWriter_UnwritableFolder(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	parent := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	dir := filepath.Join(parent, "spool")

	w := NewWriter(Options{Dir: dir, Service: "s", InstanceID: "i"})
	exp := NewLogExporter(w)
	recs := make([]sdklog.Record, 3)
	if err := exp.Export(context.Background(), recs); err != nil {
		t.Errorf("Export returned %v, want nil whatever the spool does", err)
	}
	if err := exp.ForceFlush(context.Background()); err != nil {
		t.Errorf("ForceFlush returned %v, want nil", err)
	}
	w.Close()
	st := w.Stats()
	if st.Dropped != 3 || st.Errors == 0 {
		t.Errorf("stats = %+v, want 3 dropped and the failure counted", st)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("spool folder exists: %v", err)
	}
}

// A flush the filesystem never finishes is abandoned at SyncTimeout.
func TestWriter_SyncAndCloseAreBounded(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(Options{Dir: dir, Service: "s", InstanceID: "i", SyncTimeout: 50 * time.Millisecond})
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	w.syncFile = func(*os.File) error {
		<-block
		return nil
	}
	w.writeLine([]byte("{}\n"), 1)

	for _, op := range []struct {
		name string
		f    func()
	}{{"Sync", w.Sync}, {"Close", w.Close}} {
		start := time.Now()
		op.f()
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%s took %v, want it abandoned at the 50ms timeout", op.name, d)
		}
	}
	if got := w.Stats().Errors; got != 2 {
		t.Errorf("errors = %d, want both abandoned flushes counted", got)
	}
}

// Records arriving after shutdown are dropped and counted.
func TestExporters_AfterShutdown(t *testing.T) {
	w := NewWriter(Options{Dir: t.TempDir(), Service: "s", InstanceID: "i"})
	te, le := NewTraceExporter(w), NewLogExporter(w)
	if err := te.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := le.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := le.Export(context.Background(), make([]sdklog.Record, 2)); err != nil {
		t.Fatal(err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(te))
	_, span := tp.Tracer("t").Start(context.Background(), "s")
	span.End()
	if got := w.Stats().Dropped; got != 3 {
		t.Errorf("dropped = %d, want 3", got)
	}
	w.Close()
}

func TestFileNamePart(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ynh", "ynh"},
		{"../etc", ".._etc"},
		{"a/b", "a_b"},
		{"", "fallback"},
		{"..", "fallback"},
		{"3f2c-11", "3f2c-11"},
	}
	for _, tt := range tests {
		if got := fileNamePart(tt.in, "fallback"); got != tt.want {
			t.Errorf("fileNamePart(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
