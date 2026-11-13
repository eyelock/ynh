package delegatemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/a2aserve"
	"github.com/eyelock/ynh/internal/delegation"
)

// A parent that has elicitation but no Tasks (Codex, Copilot CLI and OpenCode in
// their interactive UIs) gets a delegate's question as a server-initiated
// elicitation/create, sent during a call that waits. An answer is recorded like
// any other. A parent with no elicitation, and one whose client does not
// answer the request (Cursor's CLI refuses it, and Copilot CLI's -p declines
// it at once, with no one to ask) leaves the question open in the delegate's
// result, for the parent's model to answer with delegate_reply: a decline here
// is not the delegate's answer, because a headless client declines for want of
// a person, not for a person's choice.

// elicitReply is the client's response to a server-initiated request.
type elicitReply struct {
	result json.RawMessage
	errMsg string
}

func offersElicitation(caps map[string]any) bool {
	_, ok := caps["elicitation"]
	return ok
}

// handleOfPath is the delegation handle of the record at path.
func handleOfPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".json")
}

// deliver routes a response from the client to the request waiting on it.
func (s *Server) deliver(raw []byte) {
	var r struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return
	}
	var id string
	if json.Unmarshal(r.ID, &id) != nil {
		return
	}
	s.mu.Lock()
	ch := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if ch == nil {
		return
	}
	rep := elicitReply{result: r.Result}
	if r.Error != nil {
		rep.errMsg = r.Error.Message
	}
	ch <- rep
}

// elicit sends elicitation/create for the question and returns the channel its
// response arrives on.
func (s *Server) elicit(question string) <-chan elicitReply {
	ch := make(chan elicitReply, 1)
	s.mu.Lock()
	s.nextReq++
	id := fmt.Sprintf("ynh-elicit-%d", s.nextReq)
	s.pending[id] = ch
	s.mu.Unlock()
	idJSON, _ := json.Marshal(id)
	s.send(rpc{ID: idJSON, Method: "elicitation/create", Params: mustJSON(map[string]any{
		"message": "A delegate asks: " + question,
		"requestedSchema": map[string]any{"type": "object",
			"properties": map[string]any{"answer": map[string]any{"type": "string", "title": "Answer"}},
			"required":   []string{"answer"}},
	})})
	return ch
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// waitAsking is waitFor for a client that may be asked: when the delegate asks
// a question during the wait and the client offers elicitation, it puts the
// question to the client and records the answer, then goes on waiting for the
// delegate. Past the wait it returns as waitFor does; an elicitation still
// unanswered then is answered when the client does answer.
func (s *Server) waitAsking(ctx context.Context, handle, path string, wait time.Duration, caps map[string]any) (Result, error) {
	deadline := time.Now().Add(wait)
	for {
		r, err := s.waitFor(ctx, path, time.Until(deadline))
		if err != nil || r.State != "input-required" || !offersElicitation(caps) {
			return r, err
		}
		if !time.Now().Before(deadline) {
			return s.noteElicited(handle, path, r), nil
		}
		rec, err := delegation.Load(path)
		if err != nil {
			return r, err
		}
		q := a2aserve.OpenQuestion(rec)
		if q == nil {
			continue
		}
		key := handle + "/" + q.ID
		s.mu.Lock()
		again := s.asked[key]
		s.asked[key] = true
		s.mu.Unlock()
		if again {
			// Put to the client already, and left open: the client refused or
			// declined it. The parent's model answers it.
			return s.noteElicited(handle, path, r), nil
		}
		ch := s.elicit(q.Text)
		select {
		case rep := <-ch:
			if !s.applyElicited(ctx, handle, rep) {
				r, err := s.waitFor(ctx, path, 0)
				return s.noteElicited(handle, path, r), err
			}
		case <-ctx.Done():
			return r, nil
		case <-time.After(time.Until(deadline)):
			// The client may take longer than the call is held. The answer is
			// recorded whenever it comes.
			go func() {
				select {
				case rep := <-ch:
					s.applyElicited(ctx, handle, rep)
				case <-ctx.Done():
				}
			}()
			return s.waitFor(ctx, path, 0)
		}
	}
}

// noteElicited says, on a result that is still input-required, that the
// question went to the client by elicitation and was not answered there.
func (s *Server) noteElicited(handle, path string, r Result) Result {
	if r.State != "input-required" {
		return r
	}
	rec, err := delegation.Load(path)
	if err != nil {
		return r
	}
	q := a2aserve.OpenQuestion(rec)
	if q == nil {
		return r
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.asked[handle+"/"+q.ID] {
		r.Message += " (put to your client as an elicitation and not answered there: answer it yourself with delegate_reply)"
	}
	return r
}

// applyElicited records the client's response as the answer. It reports false
// when the client gave none: it refused the request (an error response),
// declined or cancelled it. The question then stays open.
func (s *Server) applyElicited(ctx context.Context, handle string, rep elicitReply) bool {
	if rep.errMsg != "" {
		return false
	}
	var e struct {
		Action  string         `json:"action"`
		Content map[string]any `json:"content"`
	}
	if json.Unmarshal(rep.result, &e) == nil && (e.Action == "decline" || e.Action == "cancel") {
		return false
	}
	answer := elicitedAnswer(rep.result)
	if strings.TrimSpace(answer) == "" {
		return false
	}
	_, err := s.replyTo(ctx, handle, answer, 0, nil)
	return err == nil
}
