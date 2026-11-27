// Package inboxmcp is the MCP server ynh-inbox, which `ynh run` adds to an
// interactive session so that a task another session delegated to its harness
// can be taken there (docs/delegate-sessions.md, "Delegating to a live
// session"). The tools:
//
//	inbox_list     the tasks waiting for this harness
//	inbox_take     take one (atomic: the first live session to take it owns it)
//	inbox_ask      ask the sender a question about a task taken
//	inbox_done     report the result and complete a task taken
//	inbox_decline  refuse a task, so the sender may resend it headless
//
// The mailbox is ynh's own record under $YNH_HOME, read and written only by
// ynh processes: nothing listens, and no token is shared. It is started as
// `ynh inbox-mcp --harness <run name> --session <session id>`.
package inboxmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/parentmcp"
	"github.com/eyelock/ynh/internal/presence"
)

// ServerName is the server's name in the session's MCP config. Claude Code
// prefixes it with the plugin: mcp__plugin_<harness>_ynh-inbox__inbox_take.
const ServerName = "ynh-inbox"

// Tool names.
const (
	ToolList    = "inbox_list"
	ToolTake    = "inbox_take"
	ToolAsk     = "inbox_ask"
	ToolDone    = "inbox_done"
	ToolDecline = "inbox_decline"
)

// Config is what the server is started with.
type Config struct {
	// Harness is the harness's run name, the key of its mailbox and the
	// presence records of its sessions.
	Harness string
	// Session is this session's id (its presence record), Cwd its folder.
	Session, Cwd string
	MaxWait      time.Duration
	PollEvery    time.Duration
	Version      string
}

// Task is a task as the tools show it.
type Task struct {
	TaskID   string `json:"task_id"`
	From     string `json:"from"`
	Task     string `json:"task"`
	Folder   string `json:"folder"`
	QueuedAt string `json:"queued_at,omitempty"`
	State    string `json:"state"`
}

func taskOf(r *delegation.Record) Task {
	t := Task{TaskID: r.Handle, From: r.Parent, Task: r.Task, Folder: r.Folder, State: string(r.Status)}
	if r.QueuedAt != nil {
		t.QueuedAt = r.QueuedAt.Format(time.RFC3339)
	}
	return t
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
		cfg.MaxWait = parentmcp.DefaultMaxWait
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
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) send(r rpc) {
	r.JSONRPC = "2.0"
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.out.Encode(r)
}

// Serve reads requests from in and writes responses to out until in ends.
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

func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

func (s *Server) tools() []any {
	tool := func(name, desc string, props map[string]any, required ...string) map[string]any {
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return map[string]any{"name": name, "description": desc, "inputSchema": schema}
	}
	id := prop("string", "the task's id, from inbox_list")
	return []any{
		tool(ToolList, "List the tasks other sessions have delegated to this harness and that nobody has taken yet. Finish what the person asked of you first, and tell them, before you take one.", map[string]any{}),
		tool(ToolTake, "Take a waiting task, and so own it: the first live session of this harness to take a task owns it, and a second call for it is refused. With no task_id it takes the oldest. Tell the person what you took, then do it.", map[string]any{"task_id": id}),
		tool(ToolAsk, "Ask the session that sent a task you took a question, and wait for its answer. If no answer comes within the wait the question stays open: call again with its question_id to keep waiting, or with withdraw true to go on without one.", map[string]any{
			"task_id": id, "question": prop("string", "what to ask; say what you need decided and the options"),
			"wait": map[string]any{"type": "integer", "minimum": 0, "description": "seconds to wait, at most the default"}, "question_id": prop("string", "an earlier question's id, to wait on it again"),
			"withdraw": prop("boolean", "with question_id: withdraw the question and go on")}, "task_id"),
		tool(ToolDone, "Report the result of a task you took and complete it. The result goes back to the session that sent it.", map[string]any{"task_id": id, "result": prop("string", "what you did and found")}, "task_id", "result"),
		tool(ToolDecline, "Refuse a task you took or that is waiting, with a reason. The sender is told it was rejected and may resend it to run in a session of its own.", map[string]any{"task_id": id, "reason": prop("string", "why you will not do it")}, "task_id", "reason"),
	}
}

func toolResult(v any) map[string]any {
	b, _ := json.Marshal(v)
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}, "structuredContent": v, "isError": false}
}

func toolError(msg string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": msg}}, "isError": true}
}

func (s *Server) info() map[string]any {
	return map[string]any{"name": ServerName, "version": s.cfg.Version}
}

type args struct {
	TaskID     string `json:"task_id"`
	Result     string `json:"result"`
	Reason     string `json:"reason"`
	Question   string `json:"question"`
	QuestionID string `json:"question_id"`
	Withdraw   bool   `json:"withdraw"`
	Wait       *int   `json:"wait"`
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
		reply(map[string]any{"tools": s.tools()})
	case "tools/call":
		var p struct {
			Name      string `json:"name"`
			Arguments args   `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			s.send(rpc{ID: req.ID, Error: &rpcError{-32602, "invalid params"}})
			return
		}
		v, err := s.call(ctx, p.Name, p.Arguments)
		if err != nil {
			reply(toolError(err.Error()))
			return
		}
		reply(toolResult(v))
	default:
		s.send(rpc{ID: req.ID, Error: &rpcError{-32601, "method not found: " + req.Method}})
	}
}

func (s *Server) me() presence.Entry {
	return presence.Entry{Record: presence.Record{SessionID: s.cfg.Session, Cwd: s.cfg.Cwd}}
}

// owned loads the task at handle and checks this session took it.
func (s *Server) owned(handle string) (string, error) {
	path, err := delegation.FindHandle(handle)
	if err != nil {
		return "", err
	}
	rec, err := delegation.Load(path)
	if err != nil {
		return "", err
	}
	if rec.Route != delegation.RouteLive || rec.Session != s.cfg.Session {
		return "", errors.New("this session did not take that task")
	}
	return path, nil
}

func (s *Server) call(ctx context.Context, name string, a args) (any, error) {
	switch name {
	case ToolList:
		es, err := delegation.Inbox(s.cfg.Harness)
		if err != nil {
			return nil, err
		}
		tasks := []Task{}
		for _, e := range es {
			tasks = append(tasks, taskOf(e.Record))
		}
		return map[string]any{"tasks": tasks}, nil
	case ToolTake:
		es, err := delegation.Inbox(s.cfg.Harness)
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			if a.TaskID != "" && e.Handle != a.TaskID {
				continue
			}
			rec, err := delegation.Take(e.Path, s.me())
			if errors.Is(err, delegation.ErrNotQueued) {
				if a.TaskID != "" {
					return nil, err
				}
				continue // another session got it first: try the next
			}
			if err != nil {
				return nil, err
			}
			return taskOf(rec), nil
		}
		if a.TaskID != "" {
			return nil, fmt.Errorf("no task %s is waiting in the inbox", a.TaskID)
		}
		return nil, errors.New("no task is waiting in the inbox")
	case ToolAsk:
		path, err := s.owned(a.TaskID)
		if err != nil {
			return nil, err
		}
		wait := s.cfg.MaxWait
		if a.Wait != nil && time.Duration(*a.Wait)*time.Second < wait {
			wait = time.Duration(*a.Wait) * time.Second
		}
		return parentmcp.Ask(ctx, path, a.Question, a.QuestionID, a.Withdraw, wait, s.cfg.PollEvery)
	case ToolDone:
		path, err := s.owned(a.TaskID)
		if err != nil {
			return nil, err
		}
		rec, err := delegation.Done(path, s.cfg.Session, a.Result)
		if err != nil {
			return nil, err
		}
		return taskOf(rec), nil
	case ToolDecline:
		if a.Reason == "" {
			return nil, errors.New("reason is required")
		}
		// A task still waiting is declined by taking it first: the decline is
		// then this session's, and no other session can also take it.
		if es, err := delegation.Inbox(s.cfg.Harness); err == nil {
			for _, e := range es {
				if e.Handle == a.TaskID {
					_, _ = delegation.Take(e.Path, s.me())
				}
			}
		}
		path, err := s.owned(a.TaskID)
		if err != nil {
			return nil, err
		}
		rec, err := delegation.Decline(path, s.cfg.Session, a.Reason)
		if err != nil {
			return nil, err
		}
		return taskOf(rec), nil
	}
	return nil, errors.New("unknown tool " + name)
}
