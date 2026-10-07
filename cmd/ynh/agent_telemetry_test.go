package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/telemetry"
)

const testTraceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// convergingClaude puts a fake claude first on PATH that answers each user
// message with one metered turn, and records its environment in the file it
// returns.
func convergingClaude(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")
	script := "#!/bin/sh\nenv > '" + envFile + "'\n" +
		"while IFS= read -r line; do\n" +
		"  case \"$line\" in\n" +
		"    *'\"type\":\"user\"'*)\n" +
		"      echo '{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"claude-stub-1\"}'\n" +
		"      echo '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"done\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5,\"cache_read_input_tokens\":3,\"cache_creation_input_tokens\":2},\"total_cost_usd\":0.01}'\n" +
		"      ;;\n" +
		"  esac\n" +
		"done\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	shadowVendorCLIs(t, dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return envFile
}

// spoolRecords reads every record in a spool folder: span names and event
// names, each with its trace id, span id and parent span id.
type spoolRecord struct {
	kind, name, traceID, spanID, parentID string
	attrs                                 map[string]any
}

func readSpoolRecords(t *testing.T, dir string) []spoolRecord {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []spoolRecord
	attrsOf := func(list []any) map[string]any {
		m := map[string]any{}
		for _, item := range list {
			kv := item.(map[string]any)
			for _, v := range kv["value"].(map[string]any) {
				m[kv["key"].(string)] = v
			}
		}
		return m
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			var req struct {
				ResourceSpans []struct {
					ScopeSpans []struct{ Spans []map[string]any }
				}
				ResourceLogs []struct {
					ScopeLogs []struct{ LogRecords []map[string]any }
				}
			}
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				t.Fatalf("%s: a line is not a complete JSON request: %v", e.Name(), err)
			}
			for _, rs := range req.ResourceSpans {
				for _, ss := range rs.ScopeSpans {
					for _, s := range ss.Spans {
						a, _ := s["attributes"].([]any)
						r := spoolRecord{kind: "span", attrs: attrsOf(a)}
						r.name, _ = s["name"].(string)
						r.traceID, _ = s["traceId"].(string)
						r.spanID, _ = s["spanId"].(string)
						r.parentID, _ = s["parentSpanId"].(string)
						out = append(out, r)
					}
				}
			}
			for _, rl := range req.ResourceLogs {
				for _, sl := range rl.ScopeLogs {
					for _, l := range sl.LogRecords {
						a, _ := l["attributes"].([]any)
						r := spoolRecord{kind: "event", attrs: attrsOf(a)}
						r.name, _ = l["eventName"].(string)
						r.traceID, _ = l["traceId"].(string)
						r.spanID, _ = l["spanId"].(string)
						out = append(out, r)
					}
				}
			}
		}
	}
	return out
}

// normalisedResult is the run result without the fields that differ between
// any two runs.
func normalisedResult(t *testing.T, stdout []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(stdout, &m); err != nil {
		t.Fatalf("no run result: %v\n%s", err, stdout)
	}
	delete(m, "session_id")
	if c, ok := m["consumed"].(map[string]any); ok {
		delete(c, "wall_ms")
	}
	if ss, ok := m["sensors"].([]any); ok {
		for _, s := range ss {
			if sm, ok := s.(map[string]any); ok {
				delete(sm, "duration_ms")
			}
		}
	}
	return m
}

// Telemetry never changes a run: the result, stderr and exit code are the
// same with no destination, with a spool, with an unwritable spool and with
// a spool too small for a record. With none, nothing is written anywhere,
// the worker's environment carries no trace context or spool, and the
// sensor processes inherit ynh's environment untouched. With a spool, the
// worker and each sensor process get the run's trace and the spool folder.
func TestCmdAgentRun_TelemetryChangesNothing(t *testing.T) {
	scenarios := []struct {
		name     string
		args     []string
		worker   string // "converge" or "fail"
		sensor   bool   // run against a harness whose sensor records its environment
		wantCode int
	}{
		{name: "converges", args: []string{"--task", "t", "--no-plan", "--harness", "local/probe"}, worker: "converge", sensor: true, wantCode: 0},
		{name: "worker error", args: []string{"--task", "t", "--no-plan"}, worker: "fail", wantCode: agent.ExitWorkerError},
		{name: "refused", args: []string{"--task", "t", "--backend", "gemini"}, worker: "converge", wantCode: agent.ExitRefused},
	}
	// "relay off" has ynr on PATH and the relay setting off; "relay, no
	// ynr" has the setting on and no ynr. Neither may change the run: the
	// second adds one note on stderr and nothing else.
	modes := []string{"absent", "spool", "unwritable spool", "full spool", "relay off", "relay, no ynr"}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			var baseline map[string]any
			var baselineStderr string
			for _, mode := range modes {
				t.Run(mode, func(t *testing.T) {
					home := t.TempDir()
					t.Setenv("YNH_HOME", home)
					t.Setenv("YNH_AGENT_SESSION", "")
					sensorEnvFile := filepath.Join(t.TempDir(), "sensor-env")
					if sc.sensor {
						// The loop's `ynh check` is this test binary, run as ynh.
						t.Setenv(execMainEnv, "1")
						// A real ynh process migrates an unstamped home first.
						if err := migration.WriteSchemaVersion(home, migration.CurrentSchemaVersion); err != nil {
							t.Fatal(err)
						}
						installListTestHarness(t, home, "probe", `{"name":"probe","version":"0.1.0","default_vendor":"claude",`+
							`"sensors":{"probe":{"category":"maintainability","source":{"command":"env > '`+sensorEnvFile+`'"},"output":{"format":"text"}}}}`)
					}
					state := t.TempDir()
					t.Setenv("XDG_STATE_HOME", state)
					t.Setenv("TRACEPARENT", testTraceParent)
					var envFile string
					if sc.worker == "converge" {
						envFile = convergingClaude(t)
					} else {
						vendorShims(t, "Not logged in")
					}

					spoolDir := filepath.Join(t.TempDir(), "spool")
					var relay stubRelay
					saved := telemetryOptions
					t.Cleanup(func() { telemetryOptions = saved })
					switch mode {
					case "spool":
						t.Setenv("YNR_SPOOL", spoolDir)
					case "unwritable spool":
						if os.Geteuid() == 0 {
							t.Skip("root can write anywhere")
						}
						parent := filepath.Dir(spoolDir)
						if err := os.Chmod(parent, 0o500); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
						t.Setenv("YNR_SPOOL", spoolDir)
					case "full spool":
						telemetryOptions = telemetry.Options{MaxFileBytes: 64, MaxBytes: 64}
						t.Setenv("YNR_SPOOL", spoolDir)
					case "relay off":
						relay = installStubYnr(t, "serve")
						t.Setenv("YNR_SPOOL", spoolDir)
					case "relay, no ynr":
						hideFromPath(t, "ynr")
						t.Setenv(telemetryRelayEnv, "1")
						t.Setenv("YNR_SPOOL", spoolDir)
					}

					args := append([]string{"--format", "json", "--worktree", t.TempDir()}, sc.args...)
					var stdout, stderr bytes.Buffer
					start := time.Now()
					err := cmdAgentRun(args, &stdout, &stderr, strings.NewReader(""))
					elapsed := time.Since(start)

					if got := processExitCode(err); got != sc.wantCode {
						t.Fatalf("exit %d, want %d (err %v)", got, sc.wantCode, err)
					}
					if elapsed > 2*telemetry.FlushTimeout+5*time.Second {
						t.Errorf("run took %v", elapsed)
					}
					result := normalisedResult(t, stdout.Bytes())
					if result["worktree"] != nil {
						delete(result, "worktree")
					}
					gotStderr := stderr.String()
					// A refused run starts no worker, so never asks for the relay.
					if mode == "relay, no ynr" && sc.wantCode != agent.ExitRefused {
						const note = "ynh: the telemetry relay is on, but ynr is not on PATH; the run continues without claude's own telemetry\n"
						if !strings.Contains(gotStderr, note) {
							t.Errorf("stderr = %q, want the relay's one note", gotStderr)
						}
						gotStderr = strings.Replace(gotStderr, note, "", 1)
					}
					if relay.dir != "" && relay.started() {
						t.Errorf("the relay started with the setting off")
					}
					if baseline == nil {
						baseline, baselineStderr = result, gotStderr
					} else {
						got, _ := json.Marshal(result)
						want, _ := json.Marshal(baseline)
						if !bytes.Equal(got, want) {
							t.Errorf("result differs from the run without telemetry:\n got %s\nwant %s", got, want)
						}
						if gotStderr != baselineStderr {
							t.Errorf("stderr = %q, want %q as without telemetry", gotStderr, baselineStderr)
						}
					}

					var workerEnv, sensorEnv string
					if envFile != "" {
						if data, rerr := os.ReadFile(envFile); rerr == nil {
							workerEnv = string(data)
						}
					}
					if sc.sensor {
						data, rerr := os.ReadFile(sensorEnvFile)
						if rerr != nil {
							t.Fatalf("the sensor never ran: %v", rerr)
						}
						sensorEnv = string(data)
					}
					switch mode {
					case "absent":
						if entries, _ := os.ReadDir(state); len(entries) != 0 {
							t.Errorf("state home holds %v, want nothing written", entries)
						}
						if strings.Contains(workerEnv, "TRACEPARENT") || strings.Contains(workerEnv, "YNR_SPOOL") {
							t.Errorf("worker got telemetry environment with telemetry off:\n%s", workerEnv)
						}
						// The sensor process inherits ynh's environment as it
						// always has: the caller's own TRACEPARENT, unchanged,
						// and nothing added.
						if sc.sensor && (!strings.Contains(sensorEnv, "TRACEPARENT="+testTraceParent+"\n") || strings.Contains(sensorEnv, "YNR_SPOOL")) {
							t.Errorf("sensor environment changed with telemetry off:\n%s", sensorEnv)
						}
					case "spool":
						recs := readSpoolRecords(t, spoolDir)
						var span, check *spoolRecord
						names := map[string]bool{}
						for i := range recs {
							names[recs[i].name] = true
							switch {
							case recs[i].kind == "span" && recs[i].name == telemetry.SpanRun:
								span = &recs[i]
							case recs[i].kind == "span" && recs[i].name == telemetry.SpanCheck:
								check = &recs[i]
							}
						}
						if span == nil || !names[telemetry.EventRunStarted] || !names[telemetry.EventRunFinished] {
							t.Fatalf("spool records %+v, want started, finished and the run span", recs)
						}
						if span.traceID != "4bf92f3577b34da6a3ce929d0e0e4736" || span.parentID != "00f067aa0ba902b7" {
							t.Errorf("run span %s parent %s, want a child of TRACEPARENT", span.traceID, span.parentID)
						}
						wantOutcome := runOutcome(sc.wantCode)
						if span.attrs[string(telemetry.AttrOutcome)] != wantOutcome ||
							span.attrs[string(telemetry.AttrExitCode)] != strconv.Itoa(sc.wantCode) {
							t.Errorf("span attributes %v, want outcome %s and exit code %d", span.attrs, wantOutcome, sc.wantCode)
						}
						if envFile != "" && sc.wantCode == 0 {
							for _, want := range []string{"TRACEPARENT=00-" + span.traceID + "-" + span.spanID + "-01", "YNR_SPOOL=" + spoolDir} {
								if !strings.Contains(workerEnv, want+"\n") {
									t.Errorf("worker env lacks %s:\n%s", want, workerEnv)
								}
							}
						}
						if sc.sensor {
							if check == nil || check.parentID != span.spanID || check.traceID != span.traceID {
								t.Fatalf("check span %+v, want a child of the run span", check)
							}
							for _, want := range []string{"TRACEPARENT=00-" + check.traceID + "-" + check.spanID + "-01", "YNR_SPOOL=" + spoolDir} {
								if !strings.Contains(sensorEnv, want+"\n") {
									t.Errorf("sensor env lacks %s:\n%s", want, sensorEnv)
								}
							}
						}
					}
				})
			}
		})
	}
}

// A `kill -9` mid-run leaves the started event, and every line written
// before it is complete.
func TestAgentRun_KilledRunKeepsStartedEvent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals and shell stub")
	}
	stubDir := t.TempDir()
	pidFile := filepath.Join(stubDir, "pid")
	script := "#!/bin/sh\necho $$ > '" + pidFile + "'\nexec sleep 60\n"
	if err := os.WriteFile(filepath.Join(stubDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	shadowVendorCLIs(t, stubDir)
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, perr := strconv.Atoi(strings.TrimSpace(string(data))); perr == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	spoolDir := filepath.Join(t.TempDir(), "spool")

	cmd := exec.Command(os.Args[0], "agent", "run", "--task", "t", "--no-plan", "--worktree", t.TempDir())
	cmd.Env = append(os.Environ(),
		execMainEnv+"=1",
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"YNH_HOME="+t.TempDir(),
		"YNH_AGENT_SESSION=",
		"YNR_SPOOL="+spoolDir,
		"TRACEPARENT="+testTraceParent,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Wait until the worker is running: the run is then mid-flight.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("the worker never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	recs := readSpoolRecords(t, spoolDir)
	var started, finished, spans int
	for _, r := range recs {
		switch {
		case r.name == telemetry.EventRunStarted:
			started++
			if r.traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
				t.Errorf("started event in trace %s, want the caller's", r.traceID)
			}
		case r.name == telemetry.EventRunFinished:
			finished++
		case r.kind == "span":
			spans++
		}
	}
	if started != 1 || finished != 0 || spans != 0 {
		t.Errorf("started=%d finished=%d spans=%d, want a start with no finish", started, finished, spans)
	}
}

func TestRunOutcome(t *testing.T) {
	// Every documented exit code has a word, and the words are distinct.
	codes := []int{0, 1, 10, 11, 12, 13, 14, 15, 20, 21, 22, 30, 31}
	seen := map[string]int{}
	for _, c := range codes {
		o := runOutcome(c)
		if o == "unknown" {
			t.Errorf("exit code %d has no outcome", c)
		}
		if prev, dup := seen[o]; dup {
			t.Errorf("exit codes %d and %d share the outcome %q", prev, c, o)
		}
		seen[o] = c
	}
	if got := runOutcome(99); got != "unknown" {
		t.Errorf("runOutcome(99) = %q, want unknown", got)
	}
}

func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }

func TestRunAttributes(t *testing.T) {
	const secret = "s3cr3t-content"
	full := &agent.RunResult{
		ExitCode:        agent.ExitTokenBudget,
		Reason:          "token budget exceeded in /home/me/" + secret,
		SessionID:       "abc123",
		SessionDir:      "/home/me/sessions/" + secret,
		Worktree:        "/home/me/work/" + secret,
		Backend:         "claude",
		Model:           "claude-opus-x",
		ModelRequested:  "opus",
		Effort:          "high",
		EffortRequested: "medium",
		BoundBy:         "tokens",
		Harness:         &agent.RunHarness{Name: "github.com/example-org/h/x", Version: "1.2.0", SHA: "deadbeef"},
		Consumed: agent.RunConsumed{
			Turns: 4, Tokens: 150, InputTokens: i64(100), OutputTokens: i64(50),
			CacheReadTokens: i64(30), CacheCreationTokens: i64(20), CostUSD: f64(0.25), WallMS: 1000,
		},
		ChangedFiles: []string{secret + ".go"},
		BaseCommit:   "cafe",
	}
	tests := []struct {
		name   string
		result *agent.RunResult
		want   map[string]any
		absent []attribute.Key
	}{
		{
			name:   "everything reported",
			result: full,
			want: map[string]any{
				"process.exit.code":                        int64(11),
				"ynh.run.turns":                            int64(4),
				"ynh.run.session_id":                       "abc123",
				"ynh.run.vendor":                           "claude",
				"gen_ai.response.model":                    "claude-opus-x",
				"gen_ai.request.model":                     "opus",
				"ynh.run.effort":                           "high",
				"ynh.run.effort.requested":                 "medium",
				"ynh.run.bound_by":                         "tokens",
				"ynh.harness.name":                         "github.com/example-org/h/x",
				"ynh.harness.version":                      "1.2.0",
				"ynh.harness.commit":                       "deadbeef",
				"gen_ai.usage.input_tokens":                int64(150),
				"gen_ai.usage.output_tokens":               int64(50),
				"gen_ai.usage.cache_read.input_tokens":     int64(30),
				"gen_ai.usage.cache_creation.input_tokens": int64(20),
				"ynh.run.cost_usd":                         0.25,
			},
		},
		{
			name:   "nothing reported is absent, not zero",
			result: &agent.RunResult{ExitCode: 1},
			want:   map[string]any{"process.exit.code": int64(1), "ynh.run.turns": int64(0)},
			absent: []attribute.Key{telemetry.AttrInputTokens, telemetry.AttrOutputTokens, telemetry.AttrCostUSD,
				telemetry.AttrModel, telemetry.AttrHarnessName, telemetry.AttrBoundBy},
		},
		{
			name:   "input without cache counts",
			result: &agent.RunResult{Consumed: agent.RunConsumed{InputTokens: i64(7), OutputTokens: i64(1)}},
			want:   map[string]any{"gen_ai.usage.input_tokens": int64(7)},
			absent: []attribute.Key{telemetry.AttrCacheReadTokens, telemetry.AttrCacheCreationTokens},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]any{}
			for _, kv := range runEndAttributes(tt.result) {
				got[string(kv.Key)] = kv.Value.AsInterface()
				if strings.Contains(kv.Value.String(), secret) || strings.Contains(kv.Value.String(), "/home/") {
					t.Errorf("%s = %q carries content or a path (contract rule 10)", kv.Key, kv.Value.String())
				}
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s = %v (%T), want %v (%T)", k, got[k], got[k], v, v)
				}
			}
			for _, k := range tt.absent {
				if _, ok := got[string(k)]; ok {
					t.Errorf("%s = %v, want it absent", k, got[string(k)])
				}
			}
		})
	}
}

func TestRunStartAttributes(t *testing.T) {
	tests := []struct {
		name string
		opts agent.RunOptions
		want map[string]any
	}{
		{
			name: "as requested",
			opts: agent.RunOptions{HarnessName: "local/h", Focus: "review", Profile: "ci", Backend: "codex",
				Model: "gpt", Effort: "low", Task: "do the secret thing", WorktreeDir: "/home/me/w"},
			want: map[string]any{"ynh.harness.name": "local/h", "ynh.run.focus": "review", "ynh.run.profile": "ci",
				"ynh.run.vendor": "codex", "gen_ai.request.model": "gpt", "ynh.run.effort.requested": "low"},
		},
		{name: "a path is never a harness name", opts: agent.RunOptions{HarnessName: "/home/me/secret-harness"}, want: map[string]any{}},
		{name: "a resume names no path", opts: agent.RunOptions{Resume: "/home/me/session"}, want: map[string]any{"ynh.run.resumed": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]any{}
			for _, kv := range runStartAttributes(tt.opts) {
				got[string(kv.Key)] = kv.Value.AsInterface()
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tt.want)
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("got %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

// A --resume is described by the identity its checkpoint restores: the
// span and both events carry the restored harness, focus and profile.
func TestCmdAgentRun_ResumeRecordsRestoredIdentity(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("YNH_AGENT_SESSION", "")
	vendorShims(t, "a refused run must not start a worker")
	spoolDir := filepath.Join(t.TempDir(), "spool")
	t.Setenv("YNR_SPOOL", spoolDir)
	resume := t.TempDir()
	writeAgentCheckpoint(t, resume, map[string]any{
		"backend": "claude", "phase": "act", "plan_finalized": true, "pending_message": "go",
		"harness_name": "local/absent", "profile": "ci", "focus": "review",
	})

	var stdout, stderr bytes.Buffer
	err := cmdAgentRun([]string{"--format", "json", "--worktree", t.TempDir(), "--resume", resume},
		&stdout, &stderr, strings.NewReader(""))
	if processExitCode(err) != agent.ExitRefused {
		t.Fatalf("err = %v, want the run refused (its harness is not installed)", err)
	}
	want := map[string]any{
		string(telemetry.AttrHarnessName): "local/absent",
		string(telemetry.AttrFocus):       "review",
		string(telemetry.AttrProfile):     "ci",
		string(telemetry.AttrResumed):     true,
	}
	found := 0
	for _, r := range readSpoolRecords(t, spoolDir) {
		if r.name != telemetry.SpanRun && r.name != telemetry.EventRunStarted && r.name != telemetry.EventRunFinished {
			continue
		}
		found++
		for k, v := range want {
			if r.attrs[k] != v {
				t.Errorf("%s: %s = %v, want %v", r.name, k, r.attrs[k], v)
			}
		}
	}
	if found != 3 {
		t.Errorf("found %d of the run span and its two events", found)
	}
}

// Each kind of call the loop makes gets its own span name, under the run.
func TestRunTelemetry_CallSpans(t *testing.T) {
	spoolDir := filepath.Join(t.TempDir(), "spool")
	t.Setenv("YNR_SPOOL", spoolDir)
	tel := telemetry.Setup("1.0.0", telemetry.Options{}, &bytes.Buffer{})
	rt := runTelemetry{run: tel.StartRun()}
	for _, c := range []struct{ kind, sensor string }{{agent.CallCheck, ""}, {agent.CallSensor, "verifier"}} {
		_, end := rt.StartCall(c.kind, 2, c.sensor)
		end("pass", true)
	}
	rt.run.Finish("converged", true)
	tel.Shutdown()

	got := map[string]map[string]any{}
	for _, r := range readSpoolRecords(t, spoolDir) {
		if r.kind == "span" {
			got[r.name] = r.attrs
		}
	}
	if a := got[telemetry.SpanCheck]; a == nil || a[string(telemetry.AttrTurn)] != "2" {
		t.Errorf("check span attributes %v", a)
	}
	if a := got[telemetry.SpanSensorRun]; a == nil || a[string(telemetry.AttrSensorName)] != "verifier" {
		t.Errorf("sensors run span attributes %v", a)
	}
}
