package parentmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
)

// record makes a delegation record for a delegate whose session is a short sleep.
func record(t *testing.T) string {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	_, path, err := delegation.Start(delegation.StartOptions{
		Parent: "local/parent", Delegate: "local/delegate", Task: "t", Folder: t.TempDir(),
		Supervisor: []string{"sh", "-c", "sleep 3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func answerLater(t *testing.T, path, text string, after time.Duration) {
	t.Helper()
	go func() {
		time.Sleep(after)
		_, _ = delegation.Update(path, func(r *delegation.Record) {
			now := time.Now().UTC()
			r.Questions[0].Answer, r.Questions[0].AnsweredAt = &text, &now
		})
	}()
}

func TestAnAnsweredQuestionComesBack(t *testing.T) {
	path := record(t)
	answerLater(t, path, "main", 100*time.Millisecond)
	o, err := Ask(context.Background(), path, "which branch?", "", false, 5*time.Second, 10*time.Millisecond)
	if err != nil || o.State != StateAnswered || o.Answer != "main" || o.QuestionID != "q1" {
		t.Fatalf("got %+v, %v", o, err)
	}
	rec, _ := delegation.Load(path)
	if len(rec.Questions) != 1 || rec.Questions[0].Text != "which branch?" {
		t.Errorf("record = %+v", rec.Questions)
	}
}

func TestADeclinedQuestionSaysSo(t *testing.T) {
	path := record(t)
	answerLater(t, path, delegation.DeclinedAnswer, 50*time.Millisecond)
	o, err := Ask(context.Background(), path, "which branch?", "", false, 5*time.Second, 10*time.Millisecond)
	if err != nil || o.State != StateDeclined || o.Answer != "" || !strings.Contains(o.Message, "declined") {
		t.Fatalf("got %+v, %v", o, err)
	}
}

func TestAQuestionWithNoAnswerStaysOpenUntilWaitedOnAgainOrWithdrawn(t *testing.T) {
	path := record(t)
	o, err := Ask(context.Background(), path, "which branch?", "", false, 100*time.Millisecond, 10*time.Millisecond)
	if err != nil || o.State != StateUnanswered || !strings.Contains(o.Message, "No answer came") || !strings.Contains(o.Message, `"q1"`) {
		t.Fatalf("got %+v, %v", o, err)
	}
	rec, _ := delegation.Load(path)
	if !rec.Questions[0].Open() {
		t.Fatal("an unanswered question should stay open, for a parent that returns")
	}

	// A parent returns later and answers; the delegate waits on the same question.
	answerLater(t, path, "develop", 50*time.Millisecond)
	o, err = Ask(context.Background(), path, "", "q1", false, 5*time.Second, 10*time.Millisecond)
	if err != nil || o.State != StateAnswered || o.Answer != "develop" {
		t.Fatalf("collected = %+v, %v", o, err)
	}
	rec, _ = delegation.Load(path)
	if len(rec.Questions) != 1 {
		t.Errorf("waiting again asked a new question: %+v", rec.Questions)
	}

	// Another question, withdrawn: no longer open.
	o, _ = Ask(context.Background(), path, "and the tag?", "", false, 50*time.Millisecond, 10*time.Millisecond)
	w, err := Ask(context.Background(), path, "", o.QuestionID, true, 0, 0)
	if err != nil || w.State != StateWithdrawn {
		t.Fatalf("withdraw = %+v, %v", w, err)
	}
	rec, _ = delegation.Load(path)
	if rec.Questions[1].Open() || rec.Questions[1].LapsedAt == nil {
		t.Errorf("withdrawn question = %+v", rec.Questions[1])
	}
}

func TestACancelledDelegationEndsTheWait(t *testing.T) {
	path := record(t)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = delegation.Update(path, func(r *delegation.Record) { r.Status = delegation.StatusCancelled })
	}()
	o, err := Ask(context.Background(), path, "q?", "", false, 5*time.Second, 10*time.Millisecond)
	if err != nil || o.State != StateEnded {
		t.Fatalf("got %+v, %v", o, err)
	}
}

func TestAskRefusesWhatItCannotDo(t *testing.T) {
	path := record(t)
	for name, c := range map[string]struct {
		text, id string
		withdraw bool
	}{"no question": {}, "unknown id": {id: "q9"}, "withdraw with no id": {withdraw: true}} {
		if o, err := Ask(context.Background(), path, c.text, c.id, c.withdraw, 0, 0); err == nil {
			t.Errorf("%s: got %+v, want an error", name, o)
		}
	}
}

func TestTheServerExposesOneToolAndRoundTrips(t *testing.T) {
	path := record(t)
	answerLater(t, path, "main", 150*time.Millisecond)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = New(Config{Record: path, PollEvery: 10 * time.Millisecond}).Serve(ctx, inR, outW)
		_ = outW.Close()
		close(done)
	}()
	defer func() { _ = inW.Close(); cancel(); <-done }()
	sc := bufio.NewScanner(outR)
	rpc := func(id int, method string, params any) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if _, err := inW.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
		if !sc.Scan() {
			t.Fatalf("no response to %s", method)
		}
		var r struct {
			Result json.RawMessage `json:"result"`
		}
		_ = json.Unmarshal(sc.Bytes(), &r)
		return r.Result
	}
	l := string(rpc(1, "tools/list", map[string]any{}))
	if strings.Count(l, `"name":"`) != 1 || !strings.Contains(l, `"name":"ask_parent"`) || !strings.Contains(l, "outputSchema") {
		t.Errorf("tools/list = %s", l)
	}
	var res struct {
		StructuredContent Outcome `json:"structuredContent"`
		IsError           bool    `json:"isError"`
	}
	_ = json.Unmarshal(rpc(2, "tools/call", map[string]any{"name": "ask_parent", "arguments": map[string]any{"question": "which branch?", "wait": 5}}), &res)
	if res.IsError || res.StructuredContent.State != StateAnswered || res.StructuredContent.Answer != "main" {
		t.Errorf("tools/call = %+v", res)
	}
	var bad struct {
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(rpc(3, "tools/call", map[string]any{"name": "other"}), &bad)
	if !bad.IsError {
		t.Error("an unknown tool should be a tool error")
	}
}
