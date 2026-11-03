package telemetry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// clearTelemetryEnv empties every variable Setup reads, for this test only.
func clearTelemetryEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "OTEL_") {
			t.Setenv(name, "")
		}
	}
	for _, name := range []string{"YNR_SPOOL", "TRACEPARENT", "TRACESTATE"} {
		t.Setenv(name, "")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// record is one span or log record from the spool, flattened.
type record struct {
	kind      string // "span" or "log"
	name      string
	traceID   string
	spanID    string
	parentID  string
	status    map[string]any
	attrs     map[string]any
	resources map[string]any
	start     time.Time
}

func flatten(list []any) map[string]any {
	out := map[string]any{}
	for _, item := range list {
		kv := item.(map[string]any)
		for _, v := range kv["value"].(map[string]any) {
			out[kv["key"].(string)] = v
		}
	}
	return out
}

// readSpool decodes every record in every file of dir.
func readSpool(t *testing.T, dir string) []record {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []record
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			var req struct {
				ResourceSpans []struct {
					Resource   struct{ Attributes []any }
					ScopeSpans []struct{ Spans []map[string]any }
				}
				ResourceLogs []struct {
					Resource  struct{ Attributes []any }
					ScopeLogs []struct{ LogRecords []map[string]any }
				}
			}
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				t.Fatalf("%s: %v", e.Name(), err)
			}
			for _, rs := range req.ResourceSpans {
				for _, ss := range rs.ScopeSpans {
					for _, s := range ss.Spans {
						r := record{kind: "span", resources: flatten(rs.Resource.Attributes)}
						r.name, _ = s["name"].(string)
						r.traceID, _ = s["traceId"].(string)
						r.spanID, _ = s["spanId"].(string)
						r.parentID, _ = s["parentSpanId"].(string)
						r.status, _ = s["status"].(map[string]any)
						if ns, err := strconv.ParseInt(fmt.Sprint(s["startTimeUnixNano"]), 10, 64); err == nil {
							r.start = time.Unix(0, ns)
						}
						attrs, _ := s["attributes"].([]any)
						r.attrs = flatten(attrs)
						out = append(out, r)
					}
				}
			}
			for _, rl := range req.ResourceLogs {
				for _, sl := range rl.ScopeLogs {
					for _, l := range sl.LogRecords {
						r := record{kind: "log", resources: flatten(rl.Resource.Attributes)}
						r.name, _ = l["eventName"].(string)
						r.traceID, _ = l["traceId"].(string)
						r.spanID, _ = l["spanId"].(string)
						attrs, _ := l["attributes"].([]any)
						r.attrs = flatten(attrs)
						out = append(out, r)
					}
				}
			}
		}
	}
	return out
}

func find(recs []record, kind, name string) *record {
	for i := range recs {
		if recs[i].kind == kind && recs[i].name == name {
			return &recs[i]
		}
	}
	return nil
}

func TestChooseDestination(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		mkLaptop bool // create $XDG_STATE_HOME/ynr/spool/local
		relXDG   bool // XDG_STATE_HOME is relative, so HOME/.local/state applies
		want     Destination
		wantDir  string // "spool", "laptop" or ""
	}{
		{name: "nothing", want: DestinationNone},
		{name: "laptop default absent", want: DestinationNone},
		{name: "laptop default present", mkLaptop: true, want: DestinationSpool, wantDir: "laptop"},
		{name: "YNR_SPOOL", env: map[string]string{"YNR_SPOOL": "{spool}"}, mkLaptop: true, want: DestinationSpool, wantDir: "spool"},
		{name: "OTLP endpoint wins over the spool", env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318", "YNR_SPOOL": "{spool}"}, want: DestinationOTLP},
		{name: "any OTLP exporter setting", env: map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://x"}, want: DestinationOTLP},
		{name: "an empty OTLP setting is unset", env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": ""}, want: DestinationNone},
		{name: "relative XDG_STATE_HOME is ignored", relXDG: true, mkLaptop: true, want: DestinationSpool, wantDir: "laptop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTelemetryEnv(t)
			state := t.TempDir()
			spoolDir := filepath.Join(t.TempDir(), "spool")
			t.Setenv("XDG_STATE_HOME", state)
			if tt.relXDG {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_STATE_HOME", "relative/state")
				state = filepath.Join(home, ".local", "state")
			}
			laptop := filepath.Join(state, "ynr", "spool", "local")
			if tt.mkLaptop {
				if err := os.MkdirAll(laptop, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for k, v := range tt.env {
				t.Setenv(k, strings.ReplaceAll(v, "{spool}", spoolDir))
			}
			got, dir := ChooseDestination()
			wantDir := map[string]string{"spool": spoolDir, "laptop": laptop}[tt.wantDir]
			if got != tt.want || dir != wantDir {
				t.Errorf("ChooseDestination() = %q, %q; want %q, %q", got, dir, tt.want, wantDir)
			}
		})
	}
}

// An OTLP endpoint is the operator's choice: the run's spans and logs are
// posted to it as OTLP/JSON, with the operator's headers, and nothing is
// written to the spool, even one that is configured.
func TestSetup_OTLPExports(t *testing.T) {
	clearTelemetryEnv(t)
	type hit struct {
		path, auth, ctype string
		body              []byte
	}
	var mu sync.Mutex
	var hits []hit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits = append(hits, hit{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
		mu.Unlock()
	}))
	defer srv.Close()
	spoolDir := t.TempDir()
	t.Setenv("YNR_SPOOL", spoolDir)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer s3cret")
	var stderr bytes.Buffer
	tel := Setup("1.0.0", Options{}, &stderr)
	run := tel.StartRun()
	run.Finish("converged", true)
	tel.Shutdown()
	if !tel.Active() || tel.Destination() != DestinationOTLP {
		t.Errorf("active=%v destination=%q, want active otlp", tel.Active(), tel.Destination())
	}
	if tel.SpoolDir() != "" {
		t.Errorf("SpoolDir() = %q, want none", tel.SpoolDir())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
	mu.Lock()
	defer mu.Unlock()
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.path] = true
		if h.auth != "Bearer s3cret" || !strings.HasPrefix(h.ctype, "application/json") {
			t.Errorf("%s: Authorization=%q Content-Type=%q", h.path, h.auth, h.ctype)
		}
		if !strings.Contains(string(h.body), `"service.name"`) || !strings.Contains(string(h.body), `"ynh"`) {
			t.Errorf("%s body lacks the ynh resource: %s", h.path, h.body)
		}
	}
	if !seen["/v1/traces"] || !seen["/v1/logs"] {
		t.Errorf("posted to %v, want /v1/traces and /v1/logs", seen)
	}
	if recs := readSpool(t, spoolDir); len(recs) != 0 {
		t.Errorf("wrote %d records to the spool", len(recs))
	}
}

// A setting the exporter cannot honour is the operator's choice too: one
// note naming it, never a header value, and nothing written anywhere.
func TestSetup_OTLPUnsupported(t *testing.T) {
	for _, env := range []map[string]string{
		{"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"},
		{"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf"},
		{"OTEL_EXPORTER_OTLP_CERTIFICATE": "/ca.pem"},
		{"OTEL_EXPORTER_OTLP_COMPRESSION": "gzip"},
	} {
		clearTelemetryEnv(t)
		spoolDir := t.TempDir()
		t.Setenv("YNR_SPOOL", spoolDir)
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
		t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer s3cret")
		for k, v := range env {
			t.Setenv(k, v)
		}
		var stderr bytes.Buffer
		tel := Setup("1.0.0", Options{}, &stderr)
		run := tel.StartRun()
		run.Finish("converged", true)
		tel.Shutdown()
		if tel.Active() || tel.Destination() != DestinationOTLP {
			t.Errorf("%v: active=%v destination=%q, want inactive otlp", env, tel.Active(), tel.Destination())
		}
		if n := strings.Count(stderr.String(), "\n"); n != 1 || !strings.Contains(stderr.String(), "OTEL_EXPORTER_OTLP_") {
			t.Errorf("%v: stderr = %q, want one note naming the setting", env, stderr.String())
		}
		if strings.Contains(stderr.String(), "s3cret") {
			t.Errorf("%v: the note prints a header value: %q", env, stderr.String())
		}
		if recs := readSpool(t, spoolDir); len(recs) != 0 {
			t.Errorf("%v: wrote %d records to the spool", env, len(recs))
		}
	}
}

// Settings that name no endpoint ask for nothing: the spool order goes on.
func TestChooseDestination_OTLPWithoutEndpoint(t *testing.T) {
	clearTelemetryEnv(t)
	spoolDir := t.TempDir()
	t.Setenv("YNR_SPOOL", spoolDir)
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "5000")
	if got, _ := ChooseDestination(); got != DestinationSpool {
		t.Errorf("ChooseDestination() = %q, want spool", got)
	}
}

// With no destination, nothing is written, nothing is printed and the worker
// gets no trace context, even when the caller passed one.
func TestSetup_NoDestinationIsNoOp(t *testing.T) {
	clearTelemetryEnv(t)
	t.Setenv("TRACEPARENT", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	state := os.Getenv("XDG_STATE_HOME")
	var stderr bytes.Buffer
	tel := Setup("1.0.0", Options{}, &stderr)
	run := tel.StartRun()
	if env := run.WorkerEnv(); env != nil {
		t.Errorf("WorkerEnv() = %v, want none", env)
	}
	env, end := run.StartCall(SpanCheck)
	end("pass", true)
	if env != nil {
		t.Errorf("StartCall env = %v, want none", env)
	}
	run.Finish("converged", true)
	tel.Shutdown()
	if tel.Active() || stderr.Len() != 0 {
		t.Errorf("active=%v stderr=%q, want inactive and silent", tel.Active(), stderr.String())
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Errorf("state home has %v (%v), want nothing created", entries, err)
	}
}

func TestRun_SpoolRecords(t *testing.T) {
	const (
		traceID  = "4bf92f3577b34da6a3ce929d0e0e4736"
		parentID = "00f067aa0ba902b7"
	)
	tests := []struct {
		name        string
		traceparent string
		tracestate  string
		outcome     string
		succeeded   bool
		wantParent  bool
		wantStatus  float64 // OTLP: 1 ok, 2 error
	}{
		{name: "joins the caller's trace", traceparent: "00-" + traceID + "-" + parentID + "-01", tracestate: "vendor=x",
			outcome: "converged", succeeded: true, wantParent: true, wantStatus: 1},
		{name: "starts its own trace", outcome: "converged", succeeded: true, wantStatus: 1},
		{name: "an unparsable traceparent starts a trace", traceparent: "garbage", outcome: "converged", succeeded: true, wantStatus: 1},
		{name: "a failed run is an error", outcome: "worker_error", wantStatus: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTelemetryEnv(t)
			spoolDir := filepath.Join(t.TempDir(), "runs", "r1")
			t.Setenv("YNR_SPOOL", spoolDir)
			t.Setenv("TRACEPARENT", tt.traceparent)
			t.Setenv("TRACESTATE", tt.tracestate)
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=test,service.name=impostor")

			var stderr bytes.Buffer
			tel := Setup("9.9.9", Options{}, &stderr)
			if !tel.Active() || tel.Destination() != DestinationSpool {
				t.Fatalf("active=%v destination=%q, want the spool", tel.Active(), tel.Destination())
			}
			run := tel.StartRun(AttrRunFocus.String("review"))

			// The started event is on disk before StartRun returns.
			started := find(readSpool(t, spoolDir), "log", EventRunStarted)
			if started == nil {
				t.Fatal("ynh.run.started was not written when StartRun returned")
			}

			env := run.WorkerEnv()
			run.Finish(tt.outcome, tt.succeeded, AttrRunTurns.Int(3))
			tel.Shutdown()

			recs := readSpool(t, spoolDir)
			span := find(recs, "span", SpanRun)
			finished := find(recs, "log", EventRunFinished)
			if span == nil || finished == nil {
				t.Fatalf("records = %+v, want the run span and ynh.run.finished", recs)
			}
			if tt.wantParent {
				if span.traceID != traceID || span.parentID != parentID {
					t.Errorf("span trace %s parent %s, want %s / %s", span.traceID, span.parentID, traceID, parentID)
				}
			} else if span.parentID != "" || span.traceID == traceID {
				t.Errorf("span trace %s parent %q, want a new root", span.traceID, span.parentID)
			}
			for _, r := range []*record{started, finished} {
				if r.traceID != span.traceID || r.spanID != span.spanID {
					t.Errorf("%s carries %s/%s, want the run span %s/%s", r.name, r.traceID, r.spanID, span.traceID, span.spanID)
				}
			}
			wantEnv := "TRACEPARENT=00-" + span.traceID + "-" + span.spanID + "-01"
			if len(env) == 0 || env[0] != wantEnv {
				t.Errorf("WorkerEnv() = %v, want %s first", env, wantEnv)
			}
			if tt.tracestate != "" && (len(env) < 2 || env[1] != "TRACESTATE="+tt.tracestate) {
				t.Errorf("WorkerEnv() = %v, want the TRACESTATE passed on", env)
			}
			if env[len(env)-1] != "YNR_SPOOL="+spoolDir {
				t.Errorf("WorkerEnv() = %v, want the spool folder last", env)
			}
			if span.status["code"] != tt.wantStatus {
				t.Errorf("status = %v, want code %v", span.status, tt.wantStatus)
			}
			if !tt.succeeded && span.status["message"] != tt.outcome {
				t.Errorf("status message = %v, want the outcome alone", span.status["message"])
			}
			if span.attrs[string(AttrRunOutcome)] != tt.outcome || span.attrs[string(AttrRunFocus)] != "review" ||
				span.attrs[string(AttrRunTurns)] != "3" {
				t.Errorf("span attributes = %v", span.attrs)
			}
			if finished.attrs[string(AttrRunOutcome)] != tt.outcome || finished.attrs[string(AttrRunFocus)] != "review" {
				t.Errorf("finished attributes = %v", finished.attrs)
			}
			res := span.resources
			if res["service.name"] != "ynh" || res["service.version"] != "9.9.9" ||
				res["deployment.environment.name"] != "test" || res["service.instance.id"] == nil {
				t.Errorf("resource = %v, want ynh's identity plus OTEL_RESOURCE_ATTRIBUTES", res)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want nothing", stderr.String())
			}
			if tel.Errors() != 0 || tel.Dropped() != 0 {
				t.Errorf("errors=%d dropped=%d, want none", tel.Errors(), tel.Dropped())
			}
		})
	}
}

// A spool that cannot take the records, unwritable or full, costs counted
// drops, never a delay past the flush limit or anything on stderr.
func TestRun_SpoolThatCannotTakeRecords(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	tests := []struct {
		name       string
		readOnly   bool
		opts       Options
		wantErrors bool
	}{
		{name: "unwritable folder", readOnly: true, wantErrors: true},
		{name: "a cap smaller than a record", opts: Options{MaxFileBytes: 64, MaxBytes: 128}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTelemetryEnv(t)
			parent := t.TempDir()
			if tt.readOnly {
				if err := os.Chmod(parent, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
			}
			spoolDir := filepath.Join(parent, "spool")
			t.Setenv("YNR_SPOOL", spoolDir)

			var stderr bytes.Buffer
			start := time.Now()
			tel := Setup("1.0.0", tt.opts, &stderr)
			run := tel.StartRun()
			run.Finish("converged", true)
			tel.Shutdown()
			if d := time.Since(start); d > FlushTimeout {
				t.Errorf("took %v, want well within the %v flush limit", d, FlushTimeout)
			}
			if tel.Dropped() != 3 {
				t.Errorf("dropped = %d, want all 3 records (two events, one span)", tel.Dropped())
			}
			if got := tel.Errors() > 0; got != tt.wantErrors {
				t.Errorf("errors = %d, want counted failures: %v", tel.Errors(), tt.wantErrors)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want nothing", stderr.String())
			}
			if entries, err := os.ReadDir(spoolDir); err == nil && len(entries) != 0 {
				t.Errorf("spool holds %d files, want none", len(entries))
			}
		})
	}
}

func TestNewInstanceID(t *testing.T) {
	a, b := newInstanceID(), newInstanceID()
	if a == b || len(a) != 36 || a[14] != '4' {
		t.Errorf("ids %q, %q: want two distinct version-4 UUIDs", a, b)
	}
}

// Each call out is a child span of the run, and the process it starts gets
// that span's trace context and the spool folder, made absolute.
func TestRun_StartCall(t *testing.T) {
	clearTelemetryEnv(t)
	t.Chdir(t.TempDir())
	t.Setenv("YNR_SPOOL", "spool") // relative: children run elsewhere
	spoolDir, err := filepath.Abs("spool")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACEPARENT", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	tel := Setup("1.0.0", Options{}, &bytes.Buffer{})
	run := tel.StartRun()
	tests := []struct {
		name       string
		outcome    string
		ok         bool
		wantStatus float64
	}{
		{name: SpanCheck, outcome: "blocked", ok: true, wantStatus: 1},
	}
	envs := map[string][]string{}
	for _, tt := range tests {
		env, end := run.StartCall(tt.name, AttrRunTurn.Int(2))
		envs[tt.name] = env
		end(tt.outcome, tt.ok)
	}
	run.Finish("converged", true)
	tel.Shutdown()

	recs := readSpool(t, spoolDir)
	runSpan := find(recs, "span", SpanRun)
	if runSpan == nil {
		t.Fatal("no run span")
	}
	for _, tt := range tests {
		call := find(recs, "span", tt.name)
		if call == nil {
			t.Fatalf("no %s span", tt.name)
		}
		if call.traceID != runSpan.traceID || call.parentID != runSpan.spanID {
			t.Errorf("%s: trace %s parent %s, want a child of the run span %s", tt.name, call.traceID, call.parentID, runSpan.spanID)
		}
		if call.attrs[string(AttrCallOutcome)] != tt.outcome || call.attrs[string(AttrRunTurn)] != "2" || call.status["code"] != tt.wantStatus {
			t.Errorf("%s: attrs %v status %v", tt.name, call.attrs, call.status)
		}
		env := envs[tt.name]
		want := []string{"TRACEPARENT=00-" + call.traceID + "-" + call.spanID + "-01", "YNR_SPOOL=" + spoolDir}
		if strings.Join(env, " ") != strings.Join(want, " ") {
			t.Errorf("%s env = %v, want %v", tt.name, env, want)
		}
	}
}

// A run that started with no destination finds the spool when it appears,
// and records itself from its real start: the span's start time is the
// run's, and the calls after it carry the trace.
func TestRun_SpoolAppearsMidRun(t *testing.T) {
	clearTelemetryEnv(t)
	laptop := filepath.Join(os.Getenv("XDG_STATE_HOME"), "ynr", "spool", "local")

	tel := Setup("1.0.0", Options{RecheckInterval: 10 * time.Millisecond}, &bytes.Buffer{})
	before := time.Now()
	run := tel.StartRun(AttrRunFocus.String("review"))
	if tel.Active() {
		t.Fatal("active with no destination")
	}
	if err := os.MkdirAll(laptop, 0o755); err != nil {
		t.Fatal(err)
	}
	// The started event reaches the disk once the spool is found.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(laptop); err == nil && find(readSpool(t, laptop), "log", EventRunStarted) != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ynh.run.started was never written after the spool appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}
	env, end := run.StartCall(SpanCheck)
	end("pass", true)
	if len(env) != 2 || env[1] != "YNR_SPOOL="+laptop {
		t.Errorf("call env = %v, want TRACEPARENT and the laptop spool", env)
	}
	run.Finish("converged", true)
	tel.Shutdown()

	recs := readSpool(t, laptop)
	span := find(recs, "span", SpanRun)
	if span == nil || find(recs, "log", EventRunFinished) == nil || find(recs, "span", SpanCheck) == nil {
		t.Fatalf("records %+v, want the run span, the check span and both events", recs)
	}
	if span.attrs[string(AttrRunFocus)] != "review" {
		t.Errorf("run span attributes %v, want the run's start attributes", span.attrs)
	}
	if span.start.After(before.Add(time.Second)) || span.start.Before(before.Add(-time.Second)) {
		t.Errorf("run span starts at %v, want the run's start near %v", span.start, before)
	}
}

// Shutdown stops the search: a spool that appears afterwards gets nothing.
func TestSetup_ShutdownStopsRecheck(t *testing.T) {
	clearTelemetryEnv(t)
	laptop := filepath.Join(os.Getenv("XDG_STATE_HOME"), "ynr", "spool", "local")
	tel := Setup("1.0.0", Options{RecheckInterval: 5 * time.Millisecond}, &bytes.Buffer{})
	run := tel.StartRun()
	run.Finish("converged", true)
	tel.Shutdown()
	if err := os.MkdirAll(laptop, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if tel.Active() {
		t.Error("telemetry started after Shutdown")
	}
	if entries, _ := os.ReadDir(laptop); len(entries) != 0 {
		t.Errorf("spool holds %d files after Shutdown", len(entries))
	}
}

// Trace context goes only into the environment of processes ynh starts.
// No global propagator is ever installed, so no HTTP client, ynh's or a
// library's, can pick the run's trace up and send it to a third party
// (ynr ADR-006, rule 4).
func TestSetup_InstallsNoGlobalPropagator(t *testing.T) {
	clearTelemetryEnv(t)
	t.Setenv("YNR_SPOOL", t.TempDir())
	t.Setenv("TRACEPARENT", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	tel := Setup("1.0.0", Options{}, &bytes.Buffer{})
	defer tel.Shutdown()
	run := tel.StartRun()
	defer run.Finish("converged", true)
	if !tel.Active() {
		t.Fatal("telemetry is not on")
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(run.ctx, carrier)
	if len(carrier) != 0 {
		t.Errorf("the global propagator injected %v; ynh must install none", carrier)
	}
	if len(otel.GetTextMapPropagator().Fields()) != 0 {
		t.Errorf("global propagator fields %v, want none", otel.GetTextMapPropagator().Fields())
	}
}

// A hung endpoint costs the run one FlushTimeout in all, however many times
// it flushes: once a flush has run out of time, later ones give up at once.
func TestSetup_HungEndpointBoundsTheWholeRun(t *testing.T) {
	clearTelemetryEnv(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	tel := Setup("1.0.0", Options{}, io.Discard)
	run := tel.StartRun()
	start := time.Now()
	run.Finish("converged", true)
	tel.Shutdown()
	if d := time.Since(start); d > FlushTimeout+time.Second {
		t.Errorf("flush and shutdown took %v, want about %v", d, FlushTimeout)
	}
}

// A run's labels are resource attributes, ynh.label.<key>, on every record
// ynh writes. Every operator ynh.label.* in OTEL_RESOURCE_ATTRIBUTES, under
// a key ynh set or another, is dropped, since only ynh's were validated;
// other operator attributes stay. The note names the keys, never the values.
func TestLabelsAreResourceAttributes(t *testing.T) {
	clearTelemetryEnv(t)
	dir := filepath.Join(t.TempDir(), "spool")
	t.Setenv("YNR_SPOOL", dir)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "ynh.label.wheelhouse=operator,ynh.label.other=/home/me/secret,deployment.environment.name=lab")
	var note bytes.Buffer
	tel := Setup("1.0.0", Options{Labels: map[string]string{"wheelhouse": "ynw", "batch": "7"}}, &note)
	run := tel.StartRun()
	run.Finish(RunOutcomeConverged, true)
	tel.Shutdown()

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no spool files: %v", err)
	}
	seen := 0
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var req struct {
				ResourceSpans []struct{ Resource resourceDoc }
				ResourceLogs  []struct{ Resource resourceDoc }
			}
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				t.Fatal(err)
			}
			var resources []resourceDoc
			for _, r := range req.ResourceSpans {
				resources = append(resources, r.Resource)
			}
			for _, r := range req.ResourceLogs {
				resources = append(resources, r.Resource)
			}
			for _, r := range resources {
				seen++
				got := r.attrs()
				for k, want := range map[string]string{
					"ynh.label.wheelhouse":        "ynw",
					"ynh.label.batch":             "7",
					"ynh.label.other":             "",
					"deployment.environment.name": "lab",
					"service.name":                "ynh",
				} {
					if got[k] != want {
						t.Errorf("resource %s = %q, want %q", k, got[k], want)
					}
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no resource found in the spool")
	}
	if n := note.String(); !strings.Contains(n, "ynh.label.other") || strings.Contains(n, "secret") {
		t.Errorf("note = %q, want it to name the dropped keys and no value", n)
	}
}

type resourceDoc struct {
	Attributes []struct {
		Key   string
		Value struct{ StringValue string }
	}
}

func (r resourceDoc) attrs() map[string]string {
	m := map[string]string{}
	for _, a := range r.Attributes {
		m[a.Key] = a.Value.StringValue
	}
	return m
}

func TestLabelAttributes(t *testing.T) {
	if got := LabelAttributes(nil); len(got) != 0 {
		t.Errorf("no labels gave %v", got)
	}
	got := LabelAttributes(map[string]string{"b": "2", "a": "1"})
	if len(got) != 2 || string(got[0].Key) != "ynh.label.a" || got[1].Value.AsString() != "2" {
		t.Errorf("attributes = %v, want sorted ynh.label.<key>", got)
	}
}

// The operator's ynh.label.* are removed from the process environment once,
// at Setup, before the recheck goroutine exists, and stay removed: a spool
// that appears mid-run activates telemetry without touching the variable, so
// a child started in that window never sees it change, and no child ever
// inherits an unvalidated label.
func TestOperatorLabelsStrippedOnceAndSpoolAppearsMidRun(t *testing.T) {
	clearTelemetryEnv(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "ynh.label.other=/home/me/secret,deployment.environment.name=lab")
	laptop := filepath.Join(os.Getenv("XDG_STATE_HOME"), "ynr", "spool", "local")

	var note bytes.Buffer
	tel := Setup("1.0.0", Options{RecheckInterval: 5 * time.Millisecond, Labels: map[string]string{"wheelhouse": "ynw"}}, &note)
	const stripped = "deployment.environment.name=lab"
	if got := os.Getenv("OTEL_RESOURCE_ATTRIBUTES"); got != stripped {
		t.Fatalf("after Setup, OTEL_RESOURCE_ATTRIBUTES = %q, want %q", got, stripped)
	}
	if n := note.String(); !strings.Contains(n, "ynh.label.other") || strings.Contains(n, "secret") {
		t.Errorf("note = %q, want the dropped key and no value", n)
	}

	// Poll the variable from a "child" while the spool appears and
	// telemetry activates: it never changes.
	stop := make(chan struct{})
	changed := make(chan string, 1)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if got := os.Getenv("OTEL_RESOURCE_ATTRIBUTES"); got != stripped {
				select {
				case changed <- got:
				default:
				}
				return
			}
		}
	}()

	run := tel.StartRun()
	if err := os.MkdirAll(laptop, 0o755); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !tel.Active() {
		if time.Now().After(deadline) {
			t.Fatal("telemetry never started after the spool appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}
	run.Finish("converged", true)
	tel.Shutdown()
	close(stop)

	select {
	case got := <-changed:
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES changed to %q while telemetry started", got)
	default:
	}
	if got := os.Getenv("OTEL_RESOURCE_ATTRIBUTES"); got != stripped {
		t.Errorf("after the run, OTEL_RESOURCE_ATTRIBUTES = %q, want %q", got, stripped)
	}

	seen := 0
	entries, _ := os.ReadDir(laptop)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(laptop, e.Name()))
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			seen++
			if strings.Contains(line, "secret") || strings.Contains(line, "ynh.label.other") {
				t.Errorf("an operator label reached the spool: %s", line)
			}
			if !strings.Contains(line, "ynh.label.wheelhouse") || !strings.Contains(line, "deployment.environment.name") {
				t.Errorf("record lacks ynh's label or the operator's other attribute: %s", line)
			}
		}
	}
	if seen == 0 {
		t.Fatal("nothing written to the spool")
	}
}
