package delegatemcp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
)

// askedTwice starts a delegation whose record has q1 answered (by the parent's
// model, with delegate_reply) and q2 open.
func askedTwice(t *testing.T) (*Server, string, string) {
	t.Helper()
	folder := setup(t)
	rec, _, err := delegation.Start(delegation.StartOptions{Parent: parent, Delegate: "local/reviewer", Task: "sleep 30", Folder: folder, Supervisor: []string{os.Args[0]}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopAll(t) })
	path := delegation.RecordPath(parent, folder, rec.Handle)
	now := time.Now().UTC()
	answer := "from delegate_reply"
	if _, err := delegation.Update(path, func(r *delegation.Record) {
		r.Questions = []delegation.Question{
			{ID: "q1", Text: "first?", AskedAt: now, Answer: &answer, AnsweredAt: &now},
			{ID: "q2", Text: "second?", AskedAt: now},
		}
	}); err != nil {
		t.Fatal(err)
	}
	return New(Config{Parent: parent, Folder: folder}), rec.Handle, path
}

func accepted(answer string) elicitReply {
	b, _ := json.Marshal(map[string]any{"action": "accept", "content": map[string]any{"answer": answer}})
	return elicitReply{result: b}
}

func TestALateElicitedAnswerNeverLandsOnALaterQuestion(t *testing.T) {
	s, _, path := askedTwice(t)
	// q1 was answered another way and q2 asked; the person's answer to q1's
	// elicitation arrives now.
	if s.applyElicited(path, "q1", accepted("late answer to q1")) {
		t.Error("an answer to a closed question was recorded")
	}
	rec, _ := delegation.Load(path)
	if rec.Questions[1].Answer != nil {
		t.Errorf("q2 took q1's late answer: %q", *rec.Questions[1].Answer)
	}
	if got := *rec.Questions[0].Answer; got != "from delegate_reply" {
		t.Errorf("q1's recorded answer was overwritten: %q", got)
	}
	// An answer to the open question itself is recorded.
	if !s.applyElicited(path, "q2", accepted("answer to q2")) {
		t.Error("an answer to the open question was dropped")
	}
	rec, _ = delegation.Load(path)
	if rec.Questions[1].Answer == nil || *rec.Questions[1].Answer != "answer to q2" {
		t.Errorf("q2 = %+v", rec.Questions[1])
	}
}

func TestTheNoteDoesNotSayAnOutstandingElicitationWasNotAnswered(t *testing.T) {
	s, handle, path := askedTwice(t)
	in := Result{State: "input-required", Message: "second?"}
	if got := s.noteElicited(handle, path, in); got.Message != "second?" {
		t.Errorf("a question never put to the client was noted: %q", got.Message)
	}
	s.mu.Lock()
	s.asked[handle+"/q2"], s.outstanding[handle+"/q2"] = true, true
	s.mu.Unlock()
	got := s.noteElicited(handle, path, in).Message
	if !strings.Contains(got, "still waiting") || strings.Contains(got, "not answered") {
		t.Errorf("outstanding elicitation noted as %q", got)
	}
	s.settle(handle + "/q2")
	if got := s.noteElicited(handle, path, in).Message; !strings.Contains(got, "not answered there") {
		t.Errorf("a settled, unanswered elicitation noted as %q", got)
	}
}
