// Package delegatemcp is ynh's MCP server for delegates: the one server `ynh
// run` adds to a parent's MCP config when the parent has A2A delegates
// (docs/delegate-sessions.md, "Reaching a delegate over MCP"). It speaks MCP
// over stdio, with the JSON-RPC written here, because the Go MCP SDK has no
// Tasks extension.
//
// It exposes one tool for each delegate the parent names, delegate_to_<name>,
// and five handle tools every parent gets: delegate_list, delegate_status,
// delegate_result, delegate_cancel and delegate_reply, and ynh_ps, the
// presence listing (docs/presence.md). A delegate's tool
// starts a task through the delegate's localhost A2A endpoint (with its
// bearer token) and returns at once with its handle; an optional wait lets it
// hold the call a bounded time. The server keeps no state that matters: the
// handle tools read the delegation record by handle, so a restarted parent's
// new server answers for old handles.
package delegatemcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"

	"github.com/eyelock/ynh/internal/a2aserve"
	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/permission"
	"github.com/eyelock/ynh/internal/presence"
)

const (
	// TasksExtension is the MCP Tasks extension's identifier.
	TasksExtension = "io.modelcontextprotocol/tasks"
	// clientCapsMeta is where a 2026-07-28 client puts its capabilities on
	// each request; an older one sends them once, in initialize.
	clientCapsMeta = "io.modelcontextprotocol/clientCapabilities"

	// DefaultMaxWait is the longest a call may be held, unless configured.
	// It sits under the shortest tool-call timeout known: Codex's default of
	// 60 seconds (tool_timeout_sec).
	DefaultMaxWait = 30 * time.Second
	// ToolPrefix starts the name of each delegate's tool.
	ToolPrefix = "delegate_to_"
)

// Delegate is one A2A endpoint the parent may reach.
type Delegate struct {
	// Name is the delegate's harness name.
	Name string
	// Card is the endpoint's base URL (its agent card is fetched from it).
	Card string
	// Auth is the Authorization header value, "Bearer <token>".
	Auth string
}

// Config is what the server is started with.
type Config struct {
	// Parent and Folder identify the parent session's delegations: the
	// records the handle tools may see are this parent's, in this folder.
	Parent, Folder string
	Delegates      []Delegate
	// MaxWait bounds the wait any call may ask for (DefaultMaxWait when 0).
	MaxWait time.Duration
	// PollEvery is how often a waiting call reads the record (200ms when 0).
	PollEvery time.Duration
	// Version is reported as the server's version.
	Version string
	// Questions is where a delegate's question goes: QuestionsModel (the
	// default of this server on its own) leaves it in the tool result for the
	// parent's model to put to the person; QuestionsClient first puts it to a
	// client that offers elicitation or Tasks. `ynh run` always passes one,
	// resolving an unset delegate_questions by session.
	Questions string
}

// Where a delegate's question goes (Config.Questions, the harness's
// delegate_questions).
const (
	QuestionsModel  = "model"
	QuestionsClient = "client"
)

// ValidQuestions reports whether v is a delegate_questions value; empty is auto
// by session, which `ynh run` resolves to model or client before it gets here.
func ValidQuestions(v string) bool { return v == "" || v == QuestionsModel || v == QuestionsClient }

// Server is the MCP server.
type Server struct {
	cfg Config

	outMu sync.Mutex
	out   *json.Encoder

	mu      sync.Mutex
	initCap map[string]any
	clients map[string]*a2aclient.Client
	a2a     map[string]a2aTask // handle -> its A2A task, for this server's own starts

	// Server-initiated requests (elicitation/create) wait here for the client's
	// response, by request id; asked remembers which questions were put to the
	// client, so one question is never elicited twice.
	pending map[string]chan elicitReply
	nextReq int
	asked   map[string]bool
	// outstanding holds the questions whose elicitation the client has not yet
	// answered, refused or declined.
	outstanding map[string]bool
}

type a2aTask struct {
	delegate  string
	id        a2a.TaskID
	contextID string
}

// New returns a server for cfg.
func New(cfg Config) *Server {
	if cfg.MaxWait <= 0 {
		cfg.MaxWait = DefaultMaxWait
	}
	if cfg.PollEvery <= 0 {
		cfg.PollEvery = 200 * time.Millisecond
	}
	if cfg.Version == "" {
		cfg.Version = "0"
	}
	return &Server{cfg: cfg, clients: map[string]*a2aclient.Client{}, a2a: map[string]a2aTask{}, pending: map[string]chan elicitReply{}, asked: map[string]bool{}, outstanding: map[string]bool{}}
}

// ToolName is the name of the tool for the delegate called name: the name
// lower-cased, every character outside a-z and 0-9 turned into an underscore.
func ToolName(name string) string {
	return ToolPrefix + strings.ToLower(a2aserve.EnvName(name))
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

// Serve reads newline-delimited JSON-RPC from in and writes to out until in
// ends. Requests are served concurrently, so a call that waits does not hold
// up a poll.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var wg sync.WaitGroup
	defer wg.Wait()
	for sc.Scan() {
		var req rpc
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		if req.Method == "" && len(req.ID) > 0 {
			s.deliver(sc.Bytes())
			continue
		}
		if req.Method == "" || len(req.ID) == 0 {
			// Not a request, or a notification.
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(ctx, req)
		}()
	}
	return sc.Err()
}

func (s *Server) send(r rpc) {
	r.JSONRPC = "2.0"
	s.outMu.Lock()
	defer s.outMu.Unlock()
	_ = s.out.Encode(r)
}

func (s *Server) reply(id json.RawMessage, result any) { s.send(rpc{ID: id, Result: result}) }
func (s *Server) fail(id json.RawMessage, code int, msg string) {
	s.send(rpc{ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *Server) capabilities() map[string]any {
	return map[string]any{
		"tools":      map[string]any{},
		"extensions": map[string]any{TasksExtension: map[string]any{}},
	}
}

func (s *Server) handle(ctx context.Context, req rpc) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Capabilities    map[string]any `json:"capabilities"`
		}
		_ = json.Unmarshal(req.Params, &p)
		s.mu.Lock()
		s.initCap = p.Capabilities
		s.mu.Unlock()
		s.reply(req.ID, map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    s.capabilities(),
			"serverInfo":      map[string]any{"name": "ynh-delegates", "version": s.cfg.Version},
		})
	case "server/discover":
		s.reply(req.ID, map[string]any{
			"supportedVersions": []string{"2026-07-28"},
			"capabilities":      s.capabilities(),
			"serverInfo":        map[string]any{"name": "ynh-delegates", "version": s.cfg.Version},
		})
	case "ping":
		s.reply(req.ID, map[string]any{})
	case "tools/list":
		s.reply(req.ID, map[string]any{"tools": s.tools()})
	case "tools/call":
		s.callTool(ctx, req)
	case "tasks/get", "tasks/update", "tasks/cancel":
		s.taskMethod(ctx, req)
	default:
		s.fail(req.ID, -32601, "method not found: "+req.Method)
	}
}

// clientCaps are the client's capabilities for this request: per request for
// a 2026-07-28 client, from initialize for an older one.
func (s *Server) clientCaps(params json.RawMessage) map[string]any {
	var p struct {
		Meta map[string]any `json:"_meta"`
	}
	_ = json.Unmarshal(params, &p)
	if c, ok := p.Meta[clientCapsMeta].(map[string]any); ok {
		return c
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initCap
}

func offersTasks(caps map[string]any) bool {
	ext, _ := caps["extensions"].(map[string]any)
	_, ok := ext[TasksExtension]
	return ok
}

// ---- the result every tool returns

// Result is the structured result of every tool, declared as each tool's
// outputSchema. TaskID is the delegation's handle: the one thing to pass back
// to the handle tools.
type Result struct {
	TaskID    string `json:"task_id"`
	ContextID string `json:"context_id,omitempty"`
	Delegate  string `json:"delegate,omitempty"`
	// State is the A2A task state: submitted (queued at a live session, not
	// taken yet), working, input-required, completed, failed, canceled or
	// rejected (the live session declined it).
	State string `json:"state"`
	// Route is where the task went, live or headless; Session and Cwd name the
	// live session it is queued at or was taken by, and its folder.
	Route   string `json:"route,omitempty"`
	Session string `json:"session,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	// Message says in a sentence what the state means and what to do next; for
	// input-required it is the delegate's question.
	Message string `json:"message"`
	// Result is the delegate's output, once completed.
	Result string `json:"result,omitempty"`
	// AskedAt and WaitingSeconds say, for an input-required delegation, when
	// the open question was asked and how long it has waited.
	AskedAt        string `json:"asked_at,omitempty"`
	WaitingSeconds int    `json:"waiting_seconds,omitempty"`
	// LogTail is the end of the run's log, when it failed.
	LogTail string `json:"log_tail,omitempty"`
	// AutoApprove is the permission level the delegate opens at and
	// AutoApproveFrom where it came from: one of permission.Sources.
	// A task queued to a live interactive session has no level and
	// auto_approve_from "interactive": a person approves each action there.
	AutoApprove     string `json:"auto_approve,omitempty"`
	AutoApproveFrom string `json:"auto_approve_from,omitempty"`
}

// ListResult is delegate_list's structured result.
type ListResult struct {
	Delegations []Result `json:"delegations"`
}

var resultSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"task_id":           map[string]any{"type": "string", "description": "the delegation's handle; pass it to delegate_status, delegate_result, delegate_cancel and delegate_reply"},
		"context_id":        map[string]any{"type": "string"},
		"delegate":          map[string]any{"type": "string"},
		"state":             map[string]any{"type": "string", "enum": []string{"submitted", "working", "input-required", "completed", "failed", "canceled", "rejected"}},
		"route":             map[string]any{"type": "string", "enum": []string{"live", "headless"}, "description": "where the task went: a live interactive session of the delegate, or a headless session of its own"},
		"session":           map[string]any{"type": "string", "description": "the live session the task is queued at or was taken by"},
		"cwd":               map[string]any{"type": "string", "description": "that live session's working folder"},
		"message":           map[string]any{"type": "string", "description": "what the state means and what to do next; the delegate's question when input-required"},
		"result":            map[string]any{"type": "string", "description": "the delegate's output, when completed"},
		"log_tail":          map[string]any{"type": "string", "description": "the end of the run's log, when failed"},
		"auto_approve":      map[string]any{"type": "string", "enum": []string{"none", "edits", "all"}, "description": "the permission level the delegate opens at when it runs unattended"},
		"auto_approve_from": map[string]any{"type": "string", "enum": sourceWords(), "description": "where auto_approve came from; interactive is a task queued to a live interactive session, where a person approves each action and no level is recorded"},
		"asked_at":          map[string]any{"type": "string", "description": "when the open question was asked (RFC 3339), when input-required"},
		"waiting_seconds":   map[string]any{"type": "integer", "description": "how long the open question has waited, when input-required"},
	},
	"required": []string{"task_id", "state", "message"},
}

var listSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"delegations": map[string]any{"type": "array", "items": resultSchema},
	},
	"required": []string{"delegations"},
}

func stateOf(rec *delegation.Record) string {
	switch rec.Status {
	case delegation.StatusSucceeded:
		return "completed"
	case delegation.StatusFailed:
		return "failed"
	case delegation.StatusCancelled:
		return "canceled"
	case delegation.StatusRejected:
		return "rejected"
	case delegation.StatusQueued:
		return "submitted"
	}
	if a2aserve.OpenQuestion(rec) != nil {
		return "input-required"
	}
	return "working"
}

// delegateName is the harness name behind a record's delegate id.
func (s *Server) delegateName(rec *delegation.Record) string {
	s.mu.Lock()
	if t, ok := s.a2a[rec.Handle]; ok {
		s.mu.Unlock()
		return t.delegate
	}
	s.mu.Unlock()
	id := rec.Delegate
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	return id
}

func (s *Server) resultOf(rec *delegation.Record) Result {
	r := Result{TaskID: rec.Handle, State: stateOf(rec), Delegate: s.delegateName(rec), Route: rec.Route, Session: rec.Session, Cwd: rec.SessionCwd,
		AutoApprove: rec.AutoApprove, AutoApproveFrom: rec.AutoApproveFrom}
	s.mu.Lock()
	if t, ok := s.a2a[rec.Handle]; ok {
		r.ContextID = t.contextID
	}
	s.mu.Unlock()
	switch r.State {
	case "completed":
		r.Result = rec.Result
		r.Message = "the delegate finished; its output is in result"
	case "failed":
		r.Message = a2aserve.FailureText(rec)
		r.LogTail = a2aserve.LogTail(rec.Log, a2aserve.LogTailLines)
	case "submitted":
		r.Message = a2aserve.QueuedText(rec)
	case "rejected":
		r.Message = "the live session declined the task: " + rec.Error + ". Resend it with on_live headless to run it in a session of its own"
	case "canceled":
		r.Message = "the delegation was cancelled"
		if rec.Error != "" {
			r.Message += ": " + rec.Error
		}
	case "input-required":
		q := a2aserve.OpenQuestion(rec)
		r.Message = fmt.Sprintf("%s asks: %s", r.Delegate, q.Text)
		r.AskedAt = q.AskedAt.UTC().Format(time.RFC3339)
		r.WaitingSeconds = int(time.Since(q.AskedAt).Seconds())
	default:
		r.Message = "still running: call delegate_status or delegate_result with this task_id, with a wait if you want to hold the call"
	}
	return r
}

func (r Result) final() bool {
	return r.State == "completed" || r.State == "failed" || r.State == "canceled" || r.State == "rejected"
}

// toolResult wraps a structured value as a tool result: structuredContent and
// the same as text. isErr marks a tool error.
func toolResult(v any, isErr bool) map[string]any {
	b, _ := json.Marshal(v)
	m := map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}, "isError": isErr}
	if !isErr {
		m["structuredContent"] = v
	}
	return m
}

// toolError is a tool error: the message as text, no structured content.
func toolError(msg string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": msg}}, "isError": true}
}

// ---- tools

func (s *Server) tools() []any {
	var out []any
	names := make([]Delegate, len(s.cfg.Delegates))
	copy(names, s.cfg.Delegates)
	sort.Slice(names, func(i, j int) bool { return names[i].Name < names[j].Name })
	waitProp := map[string]any{"type": "integer", "minimum": 0, "description": fmt.Sprintf("seconds to hold the call for the delegate to finish or ask something, at most %d (with none the call returns at once; a client that offers MCP Tasks is given a task at once whatever the wait)", int(s.cfg.MaxWait.Seconds()))}
	handleProp := map[string]any{"type": "string", "description": "the task_id of a delegation"}
	tool := func(name, desc string, props map[string]any, required []string) map[string]any {
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return map[string]any{"name": name, "description": desc, "inputSchema": schema, "outputSchema": resultSchema}
	}
	for _, d := range names {
		out = append(out, tool(ToolName(d.Name),
			fmt.Sprintf("Give the delegate %q a task. If a live interactive session of it is open and accepts tasks, the task is queued in front of that session (state submitted, route live) and nothing starts; otherwise a session of its own starts (state working, route headless); on_live chooses. With no wait it returns at once with a task_id; with a wait it holds the call for that long, at most the server's limit. A client that offers MCP Tasks (Claude Code interactive) is given a task at once and wait is not used: hold with delegate_status and a wait instead. Poll with delegate_status or delegate_result, answer a question with delegate_reply.", d.Name),
			map[string]any{"task": map[string]any{"type": "string", "description": "what the delegate is to do"}, "wait": waitProp,
				"on_live":      map[string]any{"type": "string", "enum": []string{"queue", "headless"}, "description": "when a live interactive session of the delegate is open and accepts tasks: queue (the default) puts the task in front of that session and starts nothing, headless starts a session of its own. The result's route, session and cwd say which one got it"},
				"auto_approve": map[string]any{"type": "string", "enum": []string{"none", "edits", "all"}, "description": "narrow, for this call, the permission level the delegate opens at when it runs unattended. It may only narrow: a level wider than the delegate's own ceiling (its harness, the delegates_to entry, and this session's own level) is refused before the session starts. The result's auto_approve and auto_approve_from say what it opened at"}},
			[]string{"task"}))
	}
	list := tool("delegate_list", "List this session's delegations with their state.", map[string]any{}, nil)
	list["outputSchema"] = listSchema
	ps := tool("ynh_ps", "List the sessions ynh has launched that are live or orphaned, with harness, vendor, launch mode, pids, start time and address (the same listing as `ynh ps --json`). all=true adds ended and dead sessions.",
		map[string]any{"all": map[string]any{"type": "boolean", "description": "include ended and dead sessions"}}, nil)
	ps["outputSchema"] = map[string]any{"type": "object", "properties": map[string]any{"sessions": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}}, "required": []string{"sessions"}}
	out = append(out, list, ps,
		tool("delegate_status", "The state of a delegation: submitted (queued at a live session, not yet taken), working, input-required (message holds its question), completed, failed, canceled or rejected (the live session declined it).",
			map[string]any{"task_id": handleProp, "wait": waitProp}, []string{"task_id"}),
		tool("delegate_result", "A delegation's result once it has completed; for one still going, its state.",
			map[string]any{"task_id": handleProp, "wait": waitProp}, []string{"task_id"}),
		tool("delegate_cancel", "Cancel a delegation and stop its run.",
			map[string]any{"task_id": handleProp}, []string{"task_id"}),
		tool("delegate_reply", "Answer the question of a delegation in state input-required, with a message on the same task.",
			map[string]any{"task_id": handleProp, "message": map[string]any{"type": "string"}, "wait": waitProp}, []string{"task_id", "message"}))
	return out
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolArgs struct {
	Task    string `json:"task"`
	TaskID  string `json:"task_id"`
	Message string `json:"message"`
	Wait    *int   `json:"wait"`
	All     bool   `json:"all"`
	OnLive  string `json:"on_live"`
	// AutoApprove narrows, for this call, the level the delegate opens at.
	AutoApprove string `json:"auto_approve"`
}

func (s *Server) callTool(ctx context.Context, req rpc) {
	var p callParams
	if json.Unmarshal(req.Params, &p) != nil {
		s.fail(req.ID, -32602, "invalid params")
		return
	}
	var a toolArgs
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &a); err != nil {
			s.reply(req.ID, toolError("invalid arguments: "+err.Error()))
			return
		}
	}
	wait := time.Duration(0)
	if a.Wait != nil && *a.Wait > 0 {
		wait = time.Duration(*a.Wait) * time.Second
	}
	if wait > s.cfg.MaxWait {
		wait = s.cfg.MaxWait
	}
	for _, d := range s.cfg.Delegates {
		if p.Name == ToolName(d.Name) {
			s.startTool(ctx, req, d, a, wait)
			return
		}
	}
	switch p.Name {
	case "delegate_list":
		recs, err := delegation.List(s.cfg.Parent, s.cfg.Folder)
		if err != nil {
			s.reply(req.ID, toolError(err.Error()))
			return
		}
		out := ListResult{Delegations: []Result{}}
		for _, r := range recs {
			out.Delegations = append(out.Delegations, s.resultOf(r))
		}
		s.reply(req.ID, toolResult(out, false))
	case "ynh_ps":
		es, err := presence.List(a.All)
		if err != nil {
			s.reply(req.ID, toolError(err.Error()))
			return
		}
		s.reply(req.ID, toolResult(map[string]any{"sessions": es}, false))
	case "delegate_status", "delegate_result":
		path, err := s.find(a.TaskID)
		if err != nil {
			s.reply(req.ID, toolError(err.Error()))
			return
		}
		r, err := s.waitAsking(ctx, a.TaskID, path, wait, s.clientCaps(req.Params))
		if err != nil {
			s.reply(req.ID, toolError(err.Error()))
			return
		}
		s.reply(req.ID, toolResult(r, false))
	case "delegate_cancel":
		path, err := s.find(a.TaskID)
		if err != nil {
			s.reply(req.ID, toolError(err.Error()))
			return
		}
		rec, err := delegation.Cancel(path)
		if err != nil {
			s.reply(req.ID, toolError(err.Error()))
			return
		}
		s.reply(req.ID, toolResult(s.resultOf(rec), false))
	case "delegate_reply":
		r, err := s.replyTo(ctx, a.TaskID, a.Message, wait, s.clientCaps(req.Params))
		if err != nil {
			s.reply(req.ID, toolError(err.Error()))
			return
		}
		s.reply(req.ID, toolResult(r, false))
	default:
		s.reply(req.ID, toolError("unknown tool "+p.Name))
	}
}

// find is the record path of a handle that belongs to this parent. A handle
// of another parent's, or of another folder's, is refused as if it did not exist.
func (s *Server) find(handle string) (string, error) {
	path, err := delegation.FindHandle(handle)
	if err != nil {
		return "", err
	}
	rec, err := delegation.Load(path)
	if err != nil {
		return "", err
	}
	if rec.Parent != s.cfg.Parent {
		return "", fmt.Errorf("no delegation with handle %s", handle)
	}
	return path, nil
}

// waitFor reads the record at path, and while the delegation is live and
// wait has not run out, again, until it is final or asks a question.
func (s *Server) waitFor(ctx context.Context, path string, wait time.Duration) (Result, error) {
	deadline := time.Now().Add(wait)
	for {
		rec, err := delegation.Reconcile(path)
		if err != nil {
			return Result{}, err
		}
		r := s.resultOf(rec)
		if r.final() || r.State == "input-required" || !time.Now().Before(deadline) {
			return r, nil
		}
		select {
		case <-ctx.Done():
			return r, nil
		case <-time.After(s.cfg.PollEvery):
		}
	}
}

// ---- A2A

func (s *Server) delegate(name string) (Delegate, bool) {
	for _, d := range s.cfg.Delegates {
		if d.Name == name {
			return d, true
		}
	}
	return Delegate{}, false
}

// authCtx names the credentials store's session, which the client's
// interceptor reads the bearer token from.
func authCtx(ctx context.Context) context.Context { return a2aclient.AttachSessionID(ctx, "ynh") }

func (s *Server) client(ctx context.Context, d Delegate) (*a2aclient.Client, error) {
	s.mu.Lock()
	c := s.clients[d.Name]
	s.mu.Unlock()
	if c != nil {
		return c, nil
	}
	card, err := agentcard.DefaultResolver.Resolve(ctx, d.Card)
	if err != nil {
		return nil, fmt.Errorf("reaching the %s delegate's endpoint: %w", d.Name, err)
	}
	store := a2aclient.NewInMemoryCredentialsStore()
	store.Set("ynh", "bearer", a2aclient.AuthCredential(strings.TrimPrefix(d.Auth, "Bearer ")))
	c, err = a2aclient.NewFromCard(ctx, card, a2aclient.WithCallInterceptors(&a2aclient.AuthInterceptor{Service: store}))
	if err != nil {
		return nil, fmt.Errorf("reaching the %s delegate's endpoint: %w", d.Name, err)
	}
	s.mu.Lock()
	s.clients[d.Name] = c
	s.mu.Unlock()
	return c, nil
}

// errRefused marks a call the endpoint refused for its token.
var errRefused = errors.New("token refused")

// sendError turns an A2A client error into a tool error's text. A refused
// token says so, since it is the one failure the parent can do nothing about.
func sendError(d Delegate, err error) error {
	msg := err.Error()
	if errors.Is(err, a2a.ErrUnauthenticated) || strings.Contains(msg, "401") || strings.Contains(strings.ToLower(msg), "unauthorized") {
		return fmt.Errorf("the %s delegate's endpoint refused the token (401): this session's delegate token is wrong or missing: %w: %w", d.Name, errRefused, err)
	}
	return fmt.Errorf("the %s delegate's endpoint: %w", d.Name, err)
}

// start sends the task through the delegate's A2A endpoint and returns the
// delegation's record path and the result as it stands.
func (s *Server) start(ctx context.Context, d Delegate, task, onLive, autoApprove string) (string, Result, error) {
	c, err := s.client(ctx, d)
	if err != nil {
		return "", Result{}, err
	}
	ctx = authCtx(ctx)
	m := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(task))
	if onLive != "" || autoApprove != "" {
		m.Metadata = map[string]any{}
		if onLive != "" {
			m.Metadata[a2aserve.MetaOnLive] = onLive
		}
		if autoApprove != "" {
			m.Metadata[a2aserve.MetaAutoApprove] = autoApprove
		}
	}
	resp, err := c.SendMessage(ctx, &a2a.SendMessageRequest{Message: m, Config: &a2a.SendMessageConfig{ReturnImmediately: true}})
	if err != nil {
		return "", Result{}, sendError(d, err)
	}
	t, ok := resp.(*a2a.Task)
	if !ok {
		return "", Result{}, fmt.Errorf("the %s delegate's endpoint answered with a message, not a task", d.Name)
	}
	handle, _ := t.Metadata[a2aserve.MetaHandle].(string)
	if handle == "" {
		// The session never started: the task is failed, with the reason.
		msg := "the delegate did not start"
		if t.Status.Message != nil {
			msg = textOf(t.Status.Message)
		}
		return "", Result{TaskID: string(t.ID), ContextID: t.ContextID, Delegate: d.Name, State: "failed", Message: msg}, nil
	}
	s.mu.Lock()
	s.a2a[handle] = a2aTask{delegate: d.Name, id: t.ID, contextID: t.ContextID}
	s.mu.Unlock()
	path, err := s.find(handle)
	if err != nil {
		return "", Result{}, err
	}
	return path, Result{}, nil
}

func textOf(m *a2a.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		b.WriteString(p.Text())
	}
	return b.String()
}

func (s *Server) startTool(ctx context.Context, req rpc, d Delegate, a toolArgs, wait time.Duration) {
	if strings.TrimSpace(a.Task) == "" {
		s.reply(req.ID, toolError("task is required: say what the delegate is to do"))
		return
	}
	path, failed, err := s.start(ctx, d, a.Task, a.OnLive, a.AutoApprove)
	if err != nil {
		s.reply(req.ID, toolError(err.Error()))
		return
	}
	if path == "" {
		s.reply(req.ID, toolResult(failed, false))
		return
	}
	caps := s.clientCaps(req.Params)
	if s.cfg.Questions == QuestionsClient && offersTasks(caps) {
		rec, err := delegation.Load(path)
		if err != nil {
			s.reply(req.ID, toolError(err.Error()))
			return
		}
		v := taskView(rec, "delegate started")
		v["resultType"] = "task"
		s.reply(req.ID, v)
		return
	}
	r, err := s.waitAsking(ctx, handleOfPath(path), path, wait, caps)
	if err != nil {
		s.reply(req.ID, toolError(err.Error()))
		return
	}
	s.reply(req.ID, toolResult(r, false))
}

// replyTo answers the delegation's open question with a message on its task:
// through the A2A endpoint when this server started the task, and on the
// record directly when it did not (a parent that restarted: the new endpoint
// has no memory of the old task, but the record does).
func (s *Server) replyTo(ctx context.Context, handle, message string, wait time.Duration, caps map[string]any) (Result, error) {
	path, err := s.find(handle)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(message) == "" {
		return Result{}, errors.New("message is required: it is the answer to the delegate's question")
	}
	s.mu.Lock()
	t, known := s.a2a[handle]
	s.mu.Unlock()
	if known {
		d, ok := s.delegate(t.delegate)
		if !ok {
			return Result{}, fmt.Errorf("no delegate %s in this session", t.delegate)
		}
		c, err := s.client(ctx, d)
		if err != nil {
			return Result{}, err
		}
		ctx = authCtx(ctx)
		m := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(message))
		m.TaskID, m.ContextID = t.id, t.contextID
		if _, err := c.SendMessage(ctx, &a2a.SendMessageRequest{Message: m, Config: &a2a.SendMessageConfig{ReturnImmediately: true}}); err != nil {
			serr := sendError(d, err)
			if errors.Is(serr, errRefused) {
				return Result{}, serr
			}
			// The endpoint is still following the question's own message (it
			// notices an asked question a poll after the record shows it), so
			// it cannot take another on the task yet. The answer on the record
			// is the same answer, and the execution already going follows it.
			known = false
		}
	} else {
		known = false
	}
	if !known {
		if err := a2aserve.Answer(path, message); err != nil {
			return Result{}, err
		}
	}
	return s.waitAsking(ctx, handle, path, wait, caps)
}

// ---- MCP Tasks

func taskView(rec *delegation.Record, statusMessage string) map[string]any {
	r := Result{State: stateOf(rec)}
	status := map[string]string{"submitted": "working", "working": "working", "input-required": "input_required", "completed": "completed", "failed": "failed", "canceled": "cancelled", "rejected": "failed"}[r.State]
	m := map[string]any{
		"taskId":         rec.Handle,
		"status":         status,
		"createdAt":      rec.CreatedAt.UTC().Format(time.RFC3339),
		"lastUpdatedAt":  rec.UpdatedAt.UTC().Format(time.RFC3339),
		"ttlMs":          nil,
		"pollIntervalMs": 1000,
	}
	if statusMessage != "" {
		m["statusMessage"] = statusMessage
	}
	return m
}

// taskMethod answers tasks/get, tasks/update and tasks/cancel by mapping the
// MCP task onto the delegation: its id is the handle.
func (s *Server) taskMethod(ctx context.Context, req rpc) {
	var p struct {
		TaskID         string                     `json:"taskId"`
		InputResponses map[string]json.RawMessage `json:"inputResponses"`
	}
	_ = json.Unmarshal(req.Params, &p)
	path, err := s.find(p.TaskID)
	if err != nil {
		s.fail(req.ID, -32602, "unknown task")
		return
	}
	switch req.Method {
	case "tasks/update":
		var answer string
		for _, raw := range p.InputResponses {
			answer = elicitedAnswer(raw)
		}
		if _, err := s.replyTo(ctx, p.TaskID, answer, 0, nil); err != nil {
			s.fail(req.ID, -32602, err.Error())
			return
		}
		s.reply(req.ID, map[string]any{})
	case "tasks/cancel":
		rec, err := delegation.Cancel(path)
		if err != nil {
			s.fail(req.ID, -32603, err.Error())
			return
		}
		v := taskView(rec, "")
		v["resultType"] = "complete"
		s.reply(req.ID, v)
	default:
		rec, err := delegation.Reconcile(path)
		if err != nil {
			s.fail(req.ID, -32603, err.Error())
			return
		}
		r := s.resultOf(rec)
		v := taskView(rec, r.Message)
		v["resultType"] = "complete"
		switch r.State {
		case "input-required":
			v["inputRequests"] = map[string]any{"q1": map[string]any{"method": "elicitation/create", "params": map[string]any{
				"message": r.Message,
				"requestedSchema": map[string]any{"type": "object",
					"properties": map[string]any{"answer": map[string]any{"type": "string", "title": "Answer"}},
					"required":   []string{"answer"}},
			}}}
		case "completed", "failed":
			v["result"] = toolResult(r, false)
		}
		s.reply(req.ID, v)
	}
}

// elicitedAnswer is the text of an elicitation result: its content's "answer",
// or, for a result of another shape, the JSON as it came. A declined or
// cancelled elicitation answers that nobody could.
func elicitedAnswer(raw json.RawMessage) string {
	var e struct {
		Action  string         `json:"action"`
		Content map[string]any `json:"content"`
	}
	if json.Unmarshal(raw, &e) == nil {
		if e.Action == "decline" || e.Action == "cancel" {
			return delegation.DeclinedAnswer
		}
		if a, ok := e.Content["answer"].(string); ok {
			return a
		}
	}
	return string(raw)
}

// sourceWords is every auto_approve_from word, from the one definition.
func sourceWords() []string {
	out := make([]string, 0, len(permission.Sources))
	for _, s := range permission.Sources {
		out = append(out, string(s))
	}
	return out
}
