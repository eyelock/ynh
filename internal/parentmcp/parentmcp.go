// Package parentmcp is the one tool a delegate session gets from ynh: ask_parent,
// for asking the parent that started it a question (docs/delegate-sessions.md,
// "Questions from a delegate"). `ynh run` of a delegate (a run that carries
// YNH_DELEGATION_RECORD) adds it to the session's MCP config as the server
// ynh-parent, started as `ynh parent-mcp --record <path>`.
//
// A question is written to the delegation record, which puts the delegate's A2A
// task into input-required: the parent's side (package a2aserve for the
// endpoint, package delegatemcp for the MCP tools and elicitation) shows it
// and records the answer. The tool waits for that answer a bounded time. A
// question nobody answers stays open, and a parent that comes back later
// collects it; the delegate may wait on it again, or withdraw it and go on.
package parentmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
)

// ServerName is the name of the MCP server in the delegate's config. The
// vendors prefix the tool with it (mcp__ynh-parent__ask_parent on Claude Code).
const ServerName = "ynh-parent"

// ToolName is the tool's name.
const ToolName = "ask_parent"

// DefaultMaxWait is the longest one call may be held. It sits under the
// shortest tool-call timeout known: Codex's default of 60 seconds.
const DefaultMaxWait = 45 * time.Second

// Outcome states of a call.
const (
	StateAnswered   = "answered"
	StateDeclined   = "declined"
	StateUnanswered = "unanswered"
	StateWithdrawn  = "withdrawn"
	// StateEnded: the delegation was cancelled or ended while the question waited.
	StateEnded = "ended"
)

// Outcome is the structured result of the tool.
type Outcome struct {
	QuestionID string `json:"question_id"`
	State      string `json:"state"`
	// Answer is the parent's answer, when answered.
	Answer string `json:"answer,omitempty"`
	// Message says what happened and what the delegate may do next.
	Message string `json:"message"`
}

// Ask records a question on the delegation record at path and waits up to wait
// for its answer. With a questionID it waits on that question again instead of
// asking a new one; withdraw closes it for good.
func Ask(ctx context.Context, path, text, questionID string, withdraw bool, wait, poll time.Duration) (Outcome, error) {
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	switch {
	case questionID != "" && withdraw:
		var found bool
		var cur delegation.Question
		_, err := delegation.Update(path, func(r *delegation.Record) {
			for i := range r.Questions {
				if r.Questions[i].ID == questionID {
					found, cur = true, r.Questions[i]
					if cur.Open() {
						now := time.Now().UTC()
						r.Questions[i].LapsedAt = &now
					}
				}
			}
		})
		if err != nil {
			return Outcome{}, err
		}
		if !found {
			return Outcome{}, fmt.Errorf("no question %s in this delegation", questionID)
		}
		if !cur.Open() {
			return fromQuestion(cur), nil
		}
		return Outcome{QuestionID: questionID, State: StateWithdrawn, Message: "the question is withdrawn: go on with your own default"}, nil
	case withdraw:
		return Outcome{}, errors.New("withdraw needs the question_id of the question to withdraw")
	case questionID == "":
		if strings.TrimSpace(text) == "" {
			return Outcome{}, errors.New("question is required: it is what the parent is asked")
		}
		var id string
		_, err := delegation.Update(path, func(r *delegation.Record) {
			id = fmt.Sprintf("q%d", len(r.Questions)+1)
			r.Questions = append(r.Questions, delegation.Question{ID: id, Text: text, AskedAt: time.Now().UTC()})
		})
		if err != nil {
			return Outcome{}, err
		}
		questionID = id
	}
	deadline := time.Now().Add(wait)
	for {
		rec, err := delegation.Load(path)
		if err != nil {
			return Outcome{}, err
		}
		var q *delegation.Question
		for i := range rec.Questions {
			if rec.Questions[i].ID == questionID {
				q = &rec.Questions[i]
			}
		}
		if q == nil {
			return Outcome{}, fmt.Errorf("no question %s in this delegation", questionID)
		}
		if !q.Open() {
			return fromQuestion(*q), nil
		}
		if rec.Status.Final() {
			return Outcome{QuestionID: questionID, State: StateEnded, Message: "the delegation has ended: no answer will come"}, nil
		}
		if !time.Now().Before(deadline) {
			return Outcome{QuestionID: questionID, State: StateUnanswered, Message: fmt.Sprintf(
				"No answer came within %d seconds. The question is still open, and a parent that returns will find it: call %s again with question_id %q to keep waiting, or with withdraw true to go on without an answer, on your own default or by failing plainly.",
				int(wait.Seconds()), ToolName, questionID)}, nil
		}
		select {
		case <-ctx.Done():
			return Outcome{QuestionID: questionID, State: StateUnanswered, Message: "the call was interrupted; the question is still open"}, nil
		case <-time.After(poll):
		}
	}
}

func fromQuestion(q delegation.Question) Outcome {
	switch {
	case q.LapsedAt != nil && q.Answer == nil:
		return Outcome{QuestionID: q.ID, State: StateWithdrawn, Message: "the question was withdrawn: go on with your own default"}
	case q.Answer != nil && *q.Answer == delegation.DeclinedAnswer:
		return Outcome{QuestionID: q.ID, State: StateDeclined, Message: "the parent declined to answer: go on with your own default, or fail plainly"}
	default:
		return Outcome{QuestionID: q.ID, State: StateAnswered, Answer: *q.Answer, Message: "the parent answered"}
	}
}

// Config is what the server is started with.
type Config struct {
	// Record is the delegation record the questions go to.
	Record string
	// MaxWait bounds the wait of one call (DefaultMaxWait when 0).
	MaxWait time.Duration
	// PollEvery is how often a waiting call reads the record (200ms when 0).
	PollEvery time.Duration
	Version   string
}

// Server is the MCP server, speaking newline-delimited JSON-RPC over stdio.
type Server struct {
	cfg Config
	mu  sync.Mutex
	out *json.Encoder
}

// New returns a server for cfg.
func New(cfg Config) *Server {
	if cfg.MaxWait <= 0 {
		cfg.MaxWait = DefaultMaxWait
	}
	if cfg.Version == "" {
		cfg.Version = "0"
	}
	return &Server{cfg: cfg}
}

type rpc struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (s *Server) send(r rpc) {
	r.JSONRPC = "2.0"
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.out.Encode(r)
}

// Serve reads requests from in and writes responses to out until in ends.
// Calls are served concurrently.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var wg sync.WaitGroup
	defer wg.Wait()
	for sc.Scan() {
		var req rpc
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.Method == "" || len(req.ID) == 0 {
			continue
		}
		wg.Add(1)
		go func() { defer wg.Done(); s.handle(ctx, req) }()
	}
	return sc.Err()
}

var outputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"question_id": map[string]any{"type": "string", "description": "the question's id; pass it back to wait on it again or to withdraw it"},
		"state":       map[string]any{"type": "string", "enum": []string{StateAnswered, StateDeclined, StateUnanswered, StateWithdrawn, StateEnded}},
		"answer":      map[string]any{"type": "string", "description": "the parent's answer, when state is answered"},
		"message":     map[string]any{"type": "string", "description": "what happened and what to do next"},
	},
	"required": []string{"question_id", "state", "message"},
}

func (s *Server) tool() map[string]any {
	return map[string]any{
		"name": ToolName,
		"description": "Ask the parent session that started you a question, and wait for its answer. Use it when you cannot go on without a decision only the parent can make. " +
			"The answer comes back in the result. If no answer comes within the wait, the question stays open: call again with its question_id to keep waiting, or with withdraw true to go on without one.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question":    map[string]any{"type": "string", "description": "what to ask the parent; say what you need decided and the options"},
				"wait":        map[string]any{"type": "integer", "minimum": 0, "description": fmt.Sprintf("seconds to wait for the answer, at most %d (the default)", int(s.cfg.MaxWait.Seconds()))},
				"question_id": map[string]any{"type": "string", "description": "an earlier question's id, to wait on it again instead of asking a new one"},
				"withdraw":    map[string]any{"type": "boolean", "description": "with question_id: withdraw the question and go on without an answer"},
			},
		},
		"outputSchema": outputSchema,
	}
}

func toolResult(v any, isErr bool) map[string]any {
	b, _ := json.Marshal(v)
	m := map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}, "isError": isErr}
	if !isErr {
		m["structuredContent"] = v
	}
	return m
}

func toolError(msg string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": msg}}, "isError": true}
}

func (s *Server) info() map[string]any {
	return map[string]any{"name": ServerName, "version": s.cfg.Version}
}

func (s *Server) handle(ctx context.Context, req rpc) {
	reply := func(v any) { s.send(rpc{ID: req.ID, Result: v}) }
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		reply(map[string]any{"protocolVersion": p.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": s.info()})
	case "server/discover":
		reply(map[string]any{"supportedVersions": []string{"2026-07-28"}, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": s.info()})
	case "ping":
		reply(map[string]any{})
	case "tools/list":
		reply(map[string]any{"tools": []any{s.tool()}})
	case "tools/call":
		var p struct {
			Name      string `json:"name"`
			Arguments struct {
				Question   string `json:"question"`
				Wait       *int   `json:"wait"`
				QuestionID string `json:"question_id"`
				Withdraw   bool   `json:"withdraw"`
			} `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			s.send(rpc{ID: req.ID, Error: &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{-32602, "invalid params"}})
			return
		}
		if p.Name != ToolName {
			reply(toolError("unknown tool " + p.Name))
			return
		}
		wait := s.cfg.MaxWait
		if p.Arguments.Wait != nil && time.Duration(*p.Arguments.Wait)*time.Second < wait {
			wait = time.Duration(*p.Arguments.Wait) * time.Second
		}
		o, err := Ask(ctx, s.cfg.Record, p.Arguments.Question, p.Arguments.QuestionID, p.Arguments.Withdraw, wait, s.cfg.PollEvery)
		if err != nil {
			reply(toolError(err.Error()))
			return
		}
		reply(toolResult(o, false))
	default:
		s.send(rpc{ID: req.ID, Error: &struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{-32601, "method not found: " + req.Method}})
	}
}
