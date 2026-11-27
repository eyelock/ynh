package inboxmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/presence"
)

// rig runs a server over pipes and calls its tools as a vendor would.
type rig struct {
	t   *testing.T
	in  io.Writer
	out *bufio.Reader
	n   int
}

func start(t *testing.T, cfg Config) *rig {
	t.Helper()
	ir, iw := io.Pipe()
	or, ow := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = New(cfg).Serve(ctx, ir, ow); close(done) }()
	t.Cleanup(func() { _ = iw.Close(); cancel(); <-done })
	return &rig{t: t, in: iw, out: bufio.NewReader(or)}
}

func (r *rig) call(tool string, args map[string]any) (map[string]any, bool) {
	r.t.Helper()
	r.n++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.n, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	if _, err := fmt.Fprintln(r.in, string(b)); err != nil {
		r.t.Fatal(err)
	}
	line, err := r.out.ReadString('\n')
	if err != nil {
		r.t.Fatal(err)
	}
	var resp struct {
		Result struct {
			Structured map[string]any `json:"structuredContent"`
			IsError    bool           `json:"isError"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		r.t.Fatal(err, line)
	}
	if resp.Result.IsError {
		return map[string]any{"error": resp.Result.Content[0].Text}, true
	}
	return resp.Result.Structured, false
}

func setup(t *testing.T) (presence.Entry, string) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	s, err := presence.Start(presence.Record{SessionID: "eno-1", Harness: "local--eno", Vendor: "claude", Cwd: "/work", Mode: presence.ModeInteractive, AcceptsTasks: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.End)
	live := delegation.LiveSessionsOf("local/eno")
	if len(live) != 1 {
		t.Fatalf("live = %v", live)
	}
	rec, path, err := delegation.Queue(delegation.QueueOptions{Parent: "local/ynw", Delegate: "local/eno", Task: "design the thing", Folder: t.TempDir(), Session: live[0], Supervisor: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	return live[0], rec.Handle + "|" + path
}

func TestListTakeAskDone(t *testing.T) {
	_, hp := setup(t)
	handle, path, _ := strings.Cut(hp, "|")
	r := start(t, Config{Harness: "local--eno", Session: "eno-1", Cwd: "/work", PollEvery: 10 * time.Millisecond})

	got, isErr := r.call(ToolList, nil)
	tasks, _ := got["tasks"].([]any)
	if isErr || len(tasks) != 1 || tasks[0].(map[string]any)["task"] != "design the thing" {
		t.Fatalf("list = %v", got)
	}
	got, isErr = r.call(ToolTake, nil)
	if isErr || got["task_id"] != handle || got["state"] != "running" {
		t.Fatalf("take = %v", got)
	}
	if _, isErr := r.call(ToolTake, nil); !isErr {
		t.Fatal("a second take of an empty inbox should be an error")
	}

	// A question goes to the sender's side, which answers it on the record.
	go func() {
		for {
			rec, _ := delegation.Load(path)
			if len(rec.Questions) == 1 {
				ans := "yes"
				_, _ = delegation.Update(path, func(rr *delegation.Record) { rr.Questions[0].Answer = &ans })
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	got, isErr = r.call(ToolAsk, map[string]any{"task_id": handle, "question": "shall I?", "wait": 5})
	if isErr || got["state"] != "answered" || got["answer"] != "yes" {
		t.Fatalf("ask = %v", got)
	}
	got, isErr = r.call(ToolDone, map[string]any{"task_id": handle, "result": "designed"})
	if isErr || got["state"] != "succeeded" {
		t.Fatalf("done = %v", got)
	}
	if rec, _ := delegation.Load(path); rec.Result != "designed" {
		t.Fatalf("record = %+v", rec)
	}
}

func TestAnotherSessionCannotFinishATaskItDidNotTake(t *testing.T) {
	_, hp := setup(t)
	handle, _, _ := strings.Cut(hp, "|")
	mine := start(t, Config{Harness: "local--eno", Session: "eno-1", Cwd: "/work"})
	other := start(t, Config{Harness: "local--eno", Session: "eno-2", Cwd: "/elsewhere"})
	if _, isErr := mine.call(ToolTake, map[string]any{"task_id": handle}); isErr {
		t.Fatal("take failed")
	}
	if got, isErr := other.call(ToolDone, map[string]any{"task_id": handle, "result": "x"}); !isErr {
		t.Fatalf("done by a session that did not take it = %v", got)
	}
	if got, isErr := other.call(ToolTake, map[string]any{"task_id": handle}); !isErr {
		t.Fatalf("a second take = %v", got)
	}
}

func TestDeclineRejects(t *testing.T) {
	_, hp := setup(t)
	handle, path, _ := strings.Cut(hp, "|")
	r := start(t, Config{Harness: "local--eno", Session: "eno-1", Cwd: "/work"})
	got, isErr := r.call(ToolDecline, map[string]any{"task_id": handle, "reason": "not mine"})
	if isErr || got["state"] != "rejected" {
		t.Fatalf("decline = %v", got)
	}
	if rec, _ := delegation.Load(path); rec.Status != delegation.StatusRejected || rec.Error != "not mine" {
		t.Fatalf("record = %+v", rec)
	}
}
