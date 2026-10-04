package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claude-model-sonnet.jsonl is claude-cost-fresh.jsonl's first turn with the
// model claude-sonnet-5-5: Claude Code 2.1.289 run with --model sonnet names
// claude-sonnet-5-5 in its init event, and claude-opus-5-5 without --model.

// replaceInLine replaces old with new on every fixture line containing marker.
func replaceInLine(raw, marker, old, new string) string {
	lines := strings.Split(raw, "\n")
	for i, l := range lines {
		if strings.Contains(l, marker) {
			lines[i] = strings.ReplaceAll(l, old, new)
		}
	}
	return strings.Join(lines, "\n")
}

// subagentLine is an assistant event from a subagent on another model, which
// does not say what the session ran on.
const subagentLine = `{"type":"assistant","message":{"id":"msg_sub","model":"claude-haiku-5-5","role":"assistant","type":"message","content":[{"type":"text","text":""}]},"parent_tool_use_id":"toolu_01","session_id":"b14c1ec8-2372-413d-ab7f-c481cd77c9c9"}`

func TestClaudeSession_ReportsModel(t *testing.T) {
	sonnet := readFixture(t, "claude-model-sonnet.jsonl")
	const assistant = `"type":"assistant"`
	const initEvent = `"subtype":"init"`
	tests := []struct {
		name  string
		raw   string
		turns []string
	}{
		{
			name:  "pinned alias: init names the resolved id",
			raw:   sonnet,
			turns: []string{"claude-sonnet-5-5"},
		},
		{
			name:  "unpinned: init names the backend's default, on every turn",
			raw:   readFixture(t, "claude-cost-fresh.jsonl"),
			turns: []string{"claude-opus-5-5", "claude-opus-5-5"},
		},
		{
			name:  "no init event: the main-thread message names it",
			raw:   withoutLine(sonnet, initEvent),
			turns: []string{"claude-sonnet-5-5"},
		},
		{
			name:  "a main-thread message on another model, as after a fallback: the message wins",
			raw:   replaceInLine(sonnet, assistant, "claude-sonnet-5-5", "claude-haiku-5-5"),
			turns: []string{"claude-haiku-5-5"},
		},
		{
			name:  "a subagent on another model: the session's model stands",
			raw:   strings.Replace(sonnet, "\n{\"type\":\"result\"", "\n"+subagentLine+"\n{\"type\":\"result\"", 1),
			turns: []string{"claude-sonnet-5-5"},
		},
		{
			name:  "a synthetic message is never the model",
			raw:   replaceInLine(sonnet, assistant, "claude-sonnet-5-5", claudeSyntheticModel),
			turns: []string{"claude-sonnet-5-5"},
		},
		{
			name:  "nothing names a model: none is reported",
			raw:   replaceInLine(replaceInLine(withoutLine(sonnet, initEvent), assistant, `"model":"claude-sonnet-5-5",`, ""), "control_response", "claude-sonnet-5-5", "x"),
			turns: []string{""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := claudeSessionOver(tt.raw)
			s.costBaseKnown = true
			for i, want := range tt.turns {
				turn, err := s.Next()
				if err != nil {
					t.Fatalf("turn %d: %v", i+1, err)
				}
				if turn.Model != want {
					t.Errorf("turn %d: model = %q, want %q", i+1, turn.Model, want)
				}
			}
		})
	}
}

// claude's own "Not logged in" answer names the model "<synthetic>". The turn
// is a worker error, and the session has no model to report.
func TestClaudeSession_SyntheticNotLoggedInReportsNoModel(t *testing.T) {
	s := claudeSessionOver(readFixture(t, "claude-not-logged-in.jsonl"))
	if _, err := s.Next(); err == nil {
		t.Fatal("want a worker error")
	}
	if s.model != "" {
		t.Errorf("model = %q, want none", s.model)
	}
}

func TestParseCursorOutput_ReportsInitModel(t *testing.T) {
	// The init event as cursor's stream-json output documents it.
	const initLine = `{"type":"system","subtype":"init","apiKeySource":"login","cwd":"/work","session_id":"c6b62c6f-7ead-4fd6-9922-e952131177ff","model":"Claude 4 Sonnet","permissionMode":"default"}`
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "init names the model", raw: initLine + "\n" + buildCursorResult("done"), want: "Claude 4 Sonnet"},
		{name: "no init event: none is reported", raw: buildCursorResult("done"), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turn, err := parseCursorOutput(strings.NewReader(tt.raw))
			if err != nil && err != io.EOF {
				t.Fatal(err)
			}
			if turn.Model != tt.want {
				t.Errorf("model = %q, want %q", turn.Model, tt.want)
			}
		})
	}
}

// No codex exec event names the model, so a codex turn reports none, even
// when --model asked for one.
func TestCodexSession_ReportsNoModel(t *testing.T) {
	s := &codexSession{totalsKnown: true, opts: StartOptions{Model: "gpt-5"}}
	turn, err := codexTurnFromFixture(t, s, readCodexFixture(t, "codex-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if turn.Model != "" {
		t.Errorf("model = %q, want none", turn.Model)
	}
}

// resultJSON returns the result as a consumer decoding the JSON sees it.
func resultJSON(t *testing.T, res *RunResult) map[string]any {
	t.Helper()
	_, top := consumedJSON(t, res)
	return top
}

func TestRunLoop_ReportsModelAndModelRequested(t *testing.T) {
	tests := []struct {
		name          string
		requested     string
		turnModel     string
		wantModel     any // nil: the key must be absent
		wantRequested any
	}{
		{name: "pinned alias: requested alias, resolved id ran", requested: "sonnet", turnModel: "claude-sonnet-5-5",
			wantModel: "claude-sonnet-5-5", wantRequested: "sonnet"},
		{name: "unpinned: only the model that ran", turnModel: "claude-opus-5-5",
			wantModel: "claude-opus-5-5", wantRequested: nil},
		{name: "backend reports none: model absent, never the requested one", requested: "gpt-5",
			wantModel: nil, wantRequested: "gpt-5"},
		{name: "nothing pinned, nothing reported: both absent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done", Model: tt.turnModel}}}
			opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
			opts.Model = tt.requested
			opts.EmitJSONL = filepath.Join(t.TempDir(), "trajectory.jsonl")
			res, err := RunLoop(opts)
			if err != nil {
				t.Fatalf("RunLoop: %v", err)
			}
			if got := mb.startOpts[0].Model; got != tt.requested {
				t.Errorf("worker asked for %q, want %q", got, tt.requested)
			}
			top := resultJSON(t, res)
			for key, want := range map[string]any{"model": tt.wantModel, "model_requested": tt.wantRequested} {
				got, present := top[key]
				if want == nil {
					if present {
						t.Errorf("%s = %v, want the key absent", key, got)
					}
				} else if got != want {
					t.Errorf("%s = %v, want %v", key, got, want)
				}
			}

			// The header is written before the worker reports a model, so
			// both keys hold the request: model is its deprecated copy.
			start := readSessionStart(t, opts.EmitJSONL)
			for _, key := range []string{"model_requested", "model"} {
				if got, present := start[key]; (tt.wantRequested == nil) == present || (present && got != tt.wantRequested) {
					t.Errorf("session_start %s = %v (present %v), want %v", key, got, present, tt.wantRequested)
				}
			}
			var reported []string
			for _, e := range readTrajectoryFile(t, opts.EmitJSONL) {
				if e.Kind == KindWorkerModel {
					reported = append(reported, decodeData[WorkerModelData](t, e).Model)
				}
			}
			if tt.turnModel == "" && len(reported) != 0 {
				t.Errorf("worker_model events %v, want none for a backend that reports no model", reported)
			}
			if tt.turnModel != "" && (len(reported) != 1 || reported[0] != tt.turnModel) {
				t.Errorf("worker_model events %v, want one naming %q", reported, tt.turnModel)
			}
		})
	}
}

// A worker that switches model mid-run is recorded where it switched, and
// the result names the latest. A turn that reports none changes nothing.
func TestRunLoop_ModelChangeIsRecorded(t *testing.T) {
	failSensor(t)
	mb := &mockBackend{name: "mock", turns: []Turn{
		{Content: "1", Model: "claude-opus-5-5"},
		{Content: "2"},
		{Content: "3", Model: "claude-opus-5-5"},
		{Content: "4", Model: "claude-sonnet-5-5"},
	}}
	var stdout bytes.Buffer
	opts := baseOpts(mb, &stdout, io.Discard, strings.NewReader(""))
	opts.testSensorNames = []string{"build"}
	opts.MaxTurns = 4
	opts.EmitJSONL = "-"
	res, _ := RunLoop(opts)
	if res == nil || res.Model != "claude-sonnet-5-5" {
		t.Fatalf("result model = %v, want claude-sonnet-5-5", res)
	}
	type seen struct {
		turn  int
		model string
	}
	var got []seen
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var e Event
		if json.Unmarshal([]byte(line), &e) != nil || e.Kind != KindWorkerModel {
			continue
		}
		got = append(got, seen{e.Turn, decodeData[WorkerModelData](t, e).Model})
	}
	want := []seen{{1, "claude-opus-5-5"}, {4, "claude-sonnet-5-5"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("worker_model events = %v, want %v", got, want)
	}
}

// The model reported before an interrupt carries across --resume through the
// checkpoint, until the relaunched worker reports its own. --model is not
// restored, so the resumed process requests only what it is given.
func TestRunLoop_ResumeCarriesModel(t *testing.T) {
	tests := []struct {
		name        string
		resumeModel string // --model on the resume
		turnModel   string // what the relaunched worker reports
		wantModel   string
		wantEvent   bool
	}{
		{name: "relaunched worker has not reported: the checkpoint's stands", wantModel: "claude-opus-5-5"},
		{name: "relaunched worker reports another: the latest process's wins", resumeModel: "sonnet",
			turnModel: "claude-sonnet-5-5", wantModel: "claude-sonnet-5-5", wantEvent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			failSensor(t)
			mb1 := &mockBackend{name: "mock", resumeToken: "tok-1", turns: []Turn{{Content: "r1", Model: "claude-opus-5-5"}}}
			opts1 := resumeOpts(mb1, dir)
			opts1.Model = "opus"
			opts1.MaxTurns = 1
			if _, err := RunLoop(opts1); err == nil {
				t.Fatal("run 1 should stop at the turn cap")
			}
			cp, err := readCheckpoint(dir)
			if err != nil {
				t.Fatal(err)
			}
			if cp.Model != "claude-opus-5-5" {
				t.Errorf("checkpoint model = %q, want claude-opus-5-5", cp.Model)
			}
			before := len(readTrajectoryFile(t, filepath.Join(dir, "trajectory.jsonl")))

			passSensor(t)
			mb2 := &mockBackend{name: "mock", resumeToken: "tok-1", turns: []Turn{{Content: "done", Model: tt.turnModel}}}
			opts2 := resumeOpts(mb2, dir)
			opts2.Resume = dir
			opts2.Model = tt.resumeModel
			opts2.MaxTurns = 5
			res, err := RunLoop(opts2)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if mb2.startOpts[0].Model != tt.resumeModel {
				t.Errorf("resumed worker asked for %q, want %q: --model is not restored", mb2.startOpts[0].Model, tt.resumeModel)
			}
			if res.Model != tt.wantModel || res.ModelRequested != tt.resumeModel {
				t.Errorf("model = %q requested = %q, want %q and %q", res.Model, res.ModelRequested, tt.wantModel, tt.resumeModel)
			}

			var events int
			for _, e := range readTrajectoryFile(t, filepath.Join(dir, "trajectory.jsonl"))[before:] {
				switch e.Kind {
				case KindSessionResumed:
					if got := decodeData[SessionResumedData](t, e).ModelRequested; got != tt.resumeModel {
						t.Errorf("session_resumed model_requested = %q, want %q", got, tt.resumeModel)
					}
				case KindWorkerModel:
					events++
				}
			}
			if (events == 1) != tt.wantEvent || events > 1 {
				t.Errorf("%d worker_model events after the resume, want event=%v", events, tt.wantEvent)
			}
		})
	}
}

// A checkpoint written before the model was recorded still loads, and the
// resumed run reports no model until its worker says.
func TestReadCheckpoint_OldFormatLoadsWithoutModel(t *testing.T) {
	dir := t.TempDir()
	old := `{"version":1,"session_id":"s-old","backend":"claude","phase":"act","plan_finalized":true,` +
		`"last_completed_turn":1,"budget":{"turns":1,"tokens":10,"wall_consumed_ms":5,"plan_iterations":0},` +
		`"resume_token":"tok-1","effort":"high","updated_at":"2026-09-01T00:00:00Z"}`
	if err := os.WriteFile(checkpointPath(dir), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	cp, err := readCheckpoint(dir)
	if err != nil {
		t.Fatalf("old checkpoint must load: %v", err)
	}
	if cp.Model != "" {
		t.Errorf("model = %q, want empty", cp.Model)
	}

	passSensor(t)
	mb := &mockBackend{name: "mock", resumeToken: "tok-1", turns: []Turn{{Content: "done"}}}
	opts := resumeOpts(mb, dir)
	opts.Resume = dir
	opts.MaxTurns = 5
	res, err := RunLoop(opts)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, present := resultJSON(t, res)["model"]; present {
		t.Errorf("model = %q, want the key absent", res.Model)
	}
}
