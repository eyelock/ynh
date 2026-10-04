package agent

import (
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// withoutLine drops every fixture line containing marker.
func withoutLine(raw, marker string) string {
	var keep []string
	for _, l := range strings.Split(raw, "\n") {
		if !strings.Contains(l, marker) {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// claude's total_cost_usd is a running total for the process, so a turn's
// cost is the difference from the previous result; a resumed process starts
// from the total claude reports for the session, not from zero.
func TestClaudeSession_CostSplitAndEffort(t *testing.T) {
	type want struct {
		in, out, cache int64
		cost           float64
		costReported   bool
		effort         string
	}
	tests := []struct {
		name    string
		raw     string
		resumed bool
		turns   []want
	}{
		{
			name: "fresh session: running total becomes per-turn cost",
			raw:  readFixture(t, "claude-cost-fresh.jsonl"),
			turns: []want{
				{in: 10, out: 5, cache: 100, cost: 0.05, costReported: true, effort: "high"},
				{in: 20, out: 7, cache: 200, cost: 0.07, costReported: true, effort: "high"},
			},
		},
		{
			name:    "resumed session: the starting total is not this run's",
			raw:     readFixture(t, "claude-cost-resumed.jsonl"),
			resumed: true,
			turns: []want{
				{in: 10, out: 5, cache: 100, cost: 0.12, costReported: true, effort: "high"},
				{in: 10, out: 5, cache: 100, cost: 0.08, costReported: true, effort: "high"},
			},
		},
		{
			name:    "resumed session with no starting total: first turn reports no cost rather than double counting",
			raw:     withoutLine(readFixture(t, "claude-cost-resumed.jsonl"), "ynh-get-usage"),
			resumed: true,
			turns: []want{
				{in: 10, out: 5, cache: 100, effort: "high"},
				{in: 10, out: 5, cache: 100, cost: 0.08, costReported: true, effort: "high"},
			},
		},
		{
			name: "no cost in the result and a null effort: neither is reported",
			raw:  readFixture(t, "claude-no-cost.jsonl"),
			turns: []want{
				{in: 10, out: 5, cache: 100},
				{in: 20, out: 7, cache: 200},
			},
		},
		{
			name: "no get_settings answer: effort is not reported",
			raw:  withoutLine(readFixture(t, "claude-cost-fresh.jsonl"), "ynh-get-settings"),
			turns: []want{
				{in: 10, out: 5, cache: 100, cost: 0.05, costReported: true},
				{in: 20, out: 7, cache: 200, cost: 0.07, costReported: true},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := claudeSessionOver(tt.raw)
			s.costBaseKnown = !tt.resumed
			for i, w := range tt.turns {
				turn, err := s.Next()
				if err != nil {
					t.Fatalf("turn %d: %v", i+1, err)
				}
				if !turn.UsageReported || !turn.CacheReported {
					t.Errorf("turn %d: usage reported=%v cache reported=%v, want both", i+1, turn.UsageReported, turn.CacheReported)
				}
				if turn.Usage.InputTokens != w.in || turn.Usage.OutputTokens != w.out || turn.Usage.CacheTokens != w.cache {
					t.Errorf("turn %d: usage = %+v, want in=%d out=%d cache=%d", i+1, turn.Usage, w.in, w.out, w.cache)
				}
				if turn.CostReported != w.costReported || !near(turn.CostUSD, w.cost) {
					t.Errorf("turn %d: cost = %v (reported %v), want %v (reported %v)", i+1, turn.CostUSD, turn.CostReported, w.cost, w.costReported)
				}
				if turn.Effort != w.effort {
					t.Errorf("turn %d: effort = %q, want %q", i+1, turn.Effort, w.effort)
				}
			}
		})
	}
}

func TestClaudeControlRequest_IsOneStreamJSONLine(t *testing.T) {
	line := claudeControlRequest(claudeSettingsRequest, "get_settings")
	if !strings.HasSuffix(string(line), "\n") || strings.Count(string(line), "\n") != 1 {
		t.Fatalf("want exactly one NDJSON line, got %q", line)
	}
	var got struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "control_request" || got.RequestID != claudeSettingsRequest || got.Request.Subtype != "get_settings" {
		t.Errorf("unexpected request: %+v", got)
	}
}

// consumedJSON runs the loop and returns the result's consumed object and the
// result itself, as a consumer decoding the JSON would see them.
func consumedJSON(t *testing.T, res *RunResult) (map[string]any, map[string]any) {
	t.Helper()
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	consumed, ok := top["consumed"].(map[string]any)
	if !ok {
		t.Fatalf("no consumed object in %s", data)
	}
	return consumed, top
}

func TestRunLoop_ResultReportsCostSplitAndEffort(t *testing.T) {
	usage1 := Usage{InputTokens: 100, OutputTokens: 40, CacheTokens: 900}
	usage2 := Usage{InputTokens: 50, OutputTokens: 10, CacheTokens: 300}
	tests := []struct {
		name       string
		turns      []Turn
		wantTokens float64
		want       map[string]any // key -> value; nil value means the key must be absent
	}{
		{
			name: "claude-like: usage, cache, cost and effort reported",
			turns: []Turn{
				{Content: "1", Usage: usage1, UsageReported: true, CacheReported: true, CostUSD: 0.25, CostReported: true, Effort: "high"},
				{Content: "2", Usage: usage2, UsageReported: true, CacheReported: true, CostUSD: 0.5, CostReported: true, Effort: "high"},
			},
			wantTokens: 200,
			want: map[string]any{
				"input_tokens": 150.0, "output_tokens": 50.0, "cache_read_tokens": 1200.0, "cost_usd": 0.75, "effort": "high",
			},
		},
		{
			name: "codex-like: usage without cache or cost",
			turns: []Turn{
				{Content: "1", Usage: Usage{InputTokens: 100, OutputTokens: 40}, UsageReported: true},
				{Content: "2", Usage: Usage{InputTokens: 50, OutputTokens: 10}, UsageReported: true},
			},
			wantTokens: 200,
			want: map[string]any{
				"input_tokens": 150.0, "output_tokens": 50.0, "cache_read_tokens": nil, "cost_usd": nil, "effort": nil,
			},
		},
		{
			name: "nothing reported: the total is unchanged and the new keys are absent",
			turns: []Turn{
				{Content: "1", Usage: usage1},
				{Content: "2", Usage: usage2},
			},
			wantTokens: 200,
			want: map[string]any{
				"input_tokens": nil, "output_tokens": nil, "cache_read_tokens": nil, "cost_usd": nil, "effort": nil,
			},
		},
		{
			name: "a reported cost of zero is reported, not absent",
			turns: []Turn{
				{Content: "1", CostReported: true},
				{Content: "2", CostReported: true},
			},
			wantTokens: 0,
			want:       map[string]any{"cost_usd": 0.0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failSensor(t)
			mb := &mockBackend{name: "mock", turns: tt.turns}
			opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
			opts.testSensorNames = []string{"build"}
			opts.MaxTurns = 2
			res, _ := RunLoop(opts)
			if res == nil {
				t.Fatal("a result must be returned")
			}
			consumed, top := consumedJSON(t, res)
			if consumed["tokens"] != tt.wantTokens {
				t.Errorf("tokens = %v, want %v (input plus output, cache excluded, as before)", consumed["tokens"], tt.wantTokens)
			}
			for key, want := range tt.want {
				src := consumed
				if key == "effort" {
					src = top
				}
				got, present := src[key]
				if want == nil {
					if present {
						t.Errorf("%s = %v, want the key absent", key, got)
					}
					continue
				}
				if !present {
					t.Errorf("%s is absent, want %v", key, want)
					continue
				}
				if f, ok := want.(float64); ok {
					if g, ok := got.(float64); !ok || !near(g, f) {
						t.Errorf("%s = %v, want %v", key, got, want)
					}
				} else if got != want {
					t.Errorf("%s = %v, want %v", key, got, want)
				}
			}
		})
	}
}

// The split, the cost and the effort carry across --resume the way turns and
// tokens do, through the checkpoint.
func TestRunLoop_ResumeCarriesCostSplitAndEffort(t *testing.T) {
	dir := t.TempDir()

	failSensor(t)
	mb1 := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "r1", Usage: Usage{InputTokens: 10, OutputTokens: 5, CacheTokens: 100}, UsageReported: true, CacheReported: true, CostUSD: 0.1, CostReported: true, Effort: "high"},
			{Content: "r2", Usage: Usage{InputTokens: 20, OutputTokens: 5, CacheTokens: 200}, UsageReported: true, CacheReported: true, CostUSD: 0.2, CostReported: true, Effort: "high"},
		},
		resumeToken: "tok-1",
	}
	opts1 := resumeOpts(mb1, dir)
	opts1.MaxTurns = 2
	if _, err := RunLoop(opts1); err == nil {
		t.Fatal("run 1 should stop at the turn cap")
	}
	cp, err := readCheckpoint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Budget.InputTokens != 30 || cp.Budget.OutputTokens != 10 || cp.Budget.CacheReadTokens != 300 ||
		!cp.Budget.UsageReported || !cp.Budget.CacheReported || !cp.Budget.CostReported || !near(cp.Budget.CostUSD, 0.3) {
		t.Errorf("checkpoint budget does not carry the split and cost: %+v", cp.Budget)
	}
	if cp.Effort != "high" {
		t.Errorf("checkpoint effort = %q, want high", cp.Effort)
	}

	// The relaunched worker has not reported its effort by the time the run
	// ends; the checkpoint's still stands.
	passSensor(t)
	mb2 := &mockBackend{
		name:        "mock",
		turns:       []Turn{{Content: "done", Usage: Usage{InputTokens: 1, OutputTokens: 2, CacheTokens: 3}, UsageReported: true, CacheReported: true, CostUSD: 0.05, CostReported: true}},
		resumeToken: "tok-1",
	}
	opts2 := resumeOpts(mb2, dir)
	opts2.Resume = dir
	opts2.MaxTurns = 5
	res, err := RunLoop(opts2)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	c := res.Consumed
	if c.Tokens != 43 {
		t.Errorf("tokens = %d, want 43", c.Tokens)
	}
	if c.InputTokens == nil || *c.InputTokens != 31 || c.OutputTokens == nil || *c.OutputTokens != 12 ||
		c.CacheReadTokens == nil || *c.CacheReadTokens != 303 {
		t.Errorf("split did not sum across the resume: %+v", c)
	}
	if c.CostUSD == nil || !near(*c.CostUSD, 0.35) {
		t.Errorf("cost did not sum across the resume: %v", c.CostUSD)
	}
	if res.Effort != "high" {
		t.Errorf("effort = %q, want high carried from the checkpoint", res.Effort)
	}
}

// Plan-phase turns count toward the split and cost exactly as they count
// toward the token total.
func TestRunLoop_PlanPhaseCountsCostAndSplit(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "the plan", Usage: Usage{InputTokens: 7, OutputTokens: 3, CacheTokens: 11}, UsageReported: true, CacheReported: true, CostUSD: 0.01, CostReported: true, Effort: "low"},
			{Content: "act done", Usage: Usage{InputTokens: 5, OutputTokens: 5, CacheTokens: 9}, UsageReported: true, CacheReported: true, CostUSD: 0.02, CostReported: true},
		},
	}
	opts := planOpts(mb, io.Discard, strings.NewReader(`{"action":"approve_plan"}`+"\n"))
	res, err := RunLoop(opts)
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	c := res.Consumed
	if c.Tokens != 20 {
		t.Errorf("tokens = %d, want 20 including the plan turn", c.Tokens)
	}
	if c.InputTokens == nil || *c.InputTokens != 12 || c.OutputTokens == nil || *c.OutputTokens != 8 ||
		c.CacheReadTokens == nil || *c.CacheReadTokens != 20 {
		t.Errorf("split does not include the plan turn: %+v", c)
	}
	if c.CostUSD == nil || !near(*c.CostUSD, 0.03) {
		t.Errorf("cost does not include the plan turn: %v", c.CostUSD)
	}
	if res.Effort != "low" {
		t.Errorf("effort = %q, want low as reported on the plan turn", res.Effort)
	}
}

// A checkpoint written before the split and cost existed still loads, and
// restores them as not reported rather than as zero.
func TestReadCheckpoint_OldFormatLoadsWithoutSplitOrCost(t *testing.T) {
	dir := t.TempDir()
	old := `{
  "version": 1,
  "session_id": "s-old",
  "backend": "claude",
  "phase": "act",
  "plan_finalized": true,
  "last_completed_turn": 3,
  "budget": {"turns": 3, "tokens": 1234, "wall_consumed_ms": 5000, "plan_iterations": 1},
  "updated_at": "2026-09-01T00:00:00Z"
}`
	if err := os.WriteFile(checkpointPath(dir), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	cp, err := readCheckpoint(dir)
	if err != nil {
		t.Fatalf("old checkpoint must load: %v", err)
	}
	if cp.Effort != "" {
		t.Errorf("effort = %q, want empty", cp.Effort)
	}
	var b Budget
	b.Resume(cp.Budget)
	if b.Turns() != 3 || b.Tokens() != 1234 {
		t.Errorf("counters not restored: turns=%d tokens=%d", b.Turns(), b.Tokens())
	}
	var c RunConsumed
	b.fillConsumed(&c)
	if c.InputTokens != nil || c.OutputTokens != nil || c.CacheReadTokens != nil || c.CostUSD != nil {
		t.Errorf("an old checkpoint must restore the split and cost as absent: %+v", c)
	}
}

// Each claude turn's tokens count once. The result event's usage is the
// turn's own (per-turn in streaming-input sessions) and is authoritative.
// Without it, the assistant events are the fallback: Claude Code repeats an
// API call's usage on every content-block event of that message, so each
// message id counts once, and distinct ids (one per API call) are summed.
func TestClaudeSession_CountsEachTurnOnce(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    Usage
	}{
		{"result usage is authoritative", "claude-tool-turn.jsonl", Usage{InputTokens: 25, OutputTokens: 25, CacheTokens: 300}},
		{"no result usage: one count per message id", "claude-tool-turn-no-result-usage.jsonl", Usage{InputTokens: 25, OutputTokens: 25, CacheTokens: 300}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := claudeSessionOver(readFixture(t, tt.fixture))
			s.costBaseKnown = true
			turn, err := s.Next()
			if err != nil {
				t.Fatal(err)
			}
			if !turn.UsageReported {
				t.Error("usage must be reported")
			}
			if turn.Usage != tt.want {
				t.Errorf("usage = %+v, want %+v", turn.Usage, tt.want)
			}
			var b Budget
			b.RecordUsage(turn)
			if b.Tokens() != 50 {
				t.Errorf("tokens = %d, want 50", b.Tokens())
			}
		})
	}
}

// The #416 safety net still sees a turn that reported zero usage: taking the
// result's usage instead of adding to it must keep UsageReported and zero.
func TestClaudeSession_ZeroUsageStillReportedForUnmeteredTurn(t *testing.T) {
	raw := `{"type":"assistant","message":{"id":"msg_Z","role":"assistant","content":[{"type":"text","text":"Some banner"}],"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}}
{"type":"result","subtype":"success","is_error":false,"result":"Some banner","usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}
`
	s := claudeSessionOver(raw)
	s.costBaseKnown = true
	turn, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !turn.UsageReported || turn.Usage != (Usage{}) {
		t.Fatalf("turn = %+v, want usage reported and zero", turn)
	}
	if err := unmeteredTurn("claude", turn); err == nil {
		t.Error("a zero-usage turn with a response must be a worker error")
	}
}
