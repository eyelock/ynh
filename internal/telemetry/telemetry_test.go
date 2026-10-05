package telemetry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

// An OTLP endpoint is the operator's choice, and this build cannot honour it:
// one note, nothing written, and no spool even if one is configured.
func TestSetup_OTLPNotSupported(t *testing.T) {
	clearTelemetryEnv(t)
	spoolDir := t.TempDir()
	t.Setenv("YNR_SPOOL", spoolDir)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	var stderr bytes.Buffer
	tel := Setup("1.0.0", Options{}, &stderr)
	run := tel.StartRun()
	run.Finish("converged", true)
	tel.Shutdown()
	if tel.Active() || tel.Destination != DestinationOTLP {
		t.Errorf("active=%v destination=%q, want inactive otlp", tel.Active(), tel.Destination)
	}
	if n := strings.Count(stderr.String(), "\n"); n != 1 || !strings.Contains(stderr.String(), "OTEL_EXPORTER_OTLP_") {
		t.Errorf("stderr = %q, want one note naming OTEL_EXPORTER_OTLP_*", stderr.String())
	}
	if recs := readSpool(t, spoolDir); len(recs) != 0 {
		t.Errorf("wrote %d records to the spool", len(recs))
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
			if !tel.Active() || tel.Destination != DestinationSpool {
				t.Fatalf("active=%v destination=%q, want the spool", tel.Active(), tel.Destination)
			}
			run := tel.StartRun(AttrFocus.String("review"))

			// The started event is on disk before StartRun returns.
			started := find(readSpool(t, spoolDir), "log", EventRunStarted)
			if started == nil {
				t.Fatal("ynh.run.started was not written when StartRun returned")
			}

			env := run.WorkerEnv()
			run.Finish(tt.outcome, tt.succeeded, AttrTurns.Int(3))
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
			if tt.tracestate != "" && (len(env) != 2 || env[1] != "TRACESTATE="+tt.tracestate) {
				t.Errorf("WorkerEnv() = %v, want the TRACESTATE passed on", env)
			}
			if span.status["code"] != tt.wantStatus {
				t.Errorf("status = %v, want code %v", span.status, tt.wantStatus)
			}
			if !tt.succeeded && span.status["message"] != tt.outcome {
				t.Errorf("status message = %v, want the outcome alone", span.status["message"])
			}
			if span.attrs[string(AttrOutcome)] != tt.outcome || span.attrs[string(AttrFocus)] != "review" ||
				span.attrs[string(AttrTurns)] != "3" {
				t.Errorf("span attributes = %v", span.attrs)
			}
			if finished.attrs[string(AttrOutcome)] != tt.outcome || finished.attrs[string(AttrFocus)] != "review" {
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
