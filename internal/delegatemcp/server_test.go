package delegatemcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/a2aserve"
	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/parentmcp"
)

// The delegate's session is this test binary in a helper mode: its "ynh run"
// is `sh -c <task>`, except that a task "ask:<question>" asks its parent a
// question through the delegation record and waits for the answer there.
func TestMain(m *testing.M) {
	switch os.Getenv("DELEGATEMCP_HELPER") {
	case "supervise":
		err := delegation.Supervise(os.Args[len(os.Args)-1], func(r *delegation.Record) *exec.Cmd {
			q, ok := strings.CutPrefix(r.Task, "ask:")
			short := false
			if !ok {
				q, ok = strings.CutPrefix(r.Task, "askshort:")
				short = ok
			}
			if ok {
				c := exec.Command(os.Args[0])
				c.Env = append(os.Environ(), "DELEGATEMCP_HELPER=ask", "DELEGATEMCP_RECORD="+delegation.RecordPath(r.Parent, r.Folder, r.Handle), "DELEGATEMCP_QUESTION="+q)
				if short {
					c.Env = append(c.Env, "DELEGATEMCP_WAIT=short")
				}
				return c
			}
			return exec.Command("sh", "-c", r.Task)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	case "ask":
		// The delegate's own ask_parent tool, as the delegate's session calls it.
		path := os.Getenv("DELEGATEMCP_RECORD")
		wait := 20 * time.Second
		if os.Getenv("DELEGATEMCP_WAIT") != "" {
			wait = time.Second
		}
		o, err := parentmcp.Ask(context.Background(), path, os.Getenv("DELEGATEMCP_QUESTION"), "", false, wait, 20*time.Millisecond)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		switch o.State {
		case parentmcp.StateAnswered:
			fmt.Println("you said " + o.Answer)
		default:
			fmt.Println("no answer: " + o.State)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const parent = "local/parent"

// session is a parent: an endpoint for a delegate and a server in front of it,
// driven through its pipes.
type session struct {
	t      *testing.T
	in     io.WriteCloser
	lines  *bufio.Scanner
	nextID int
	ep     *a2aserve.Endpoint
}

func newSession(t *testing.T, folder, token string, mutate func(*Config)) *session {
	t.Helper()
	ep, err := a2aserve.Open(a2aserve.Options{
		Name: "reviewer", Label: "github.com/x/reviewer", Parent: parent, Delegate: "local/reviewer",
		Folder: folder, Supervisor: []string{os.Args[0]},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ep.Close() })
	t.Cleanup(func() { stopAll(t) })
	if token == "" {
		token = ep.Token
	}
	cfg := Config{Parent: parent, Folder: folder, PollEvery: 20 * time.Millisecond, MaxWait: 3 * time.Second,
		Delegates: []Delegate{{Name: "reviewer", Card: ep.CardURL, Auth: "Bearer " + token}}}
	if mutate != nil {
		mutate(&cfg)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = New(cfg).Serve(ctx, inR, outW); _ = outW.Close(); close(done) }()
	t.Cleanup(func() { _ = inW.Close(); cancel(); <-done })
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	return &session{t: t, in: inW, lines: sc, ep: ep}
}

// stopAll cancels every delegation the test left running, and waits for the
// supervisors to finish, so the test's directories can be removed.
func stopAll(t *testing.T) {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(delegation.Dir(), "*", "*", "dlg-*.json"))
	for _, p := range paths {
		_, _ = delegation.Cancel(p)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		for _, p := range paths {
			if r, err := delegation.Reconcile(p); err == nil && r.Pgid > 1 && syscall.Kill(-r.Pgid, 0) == nil {
				busy = true
			}
		}
		if !busy {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func setup(t *testing.T) string {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("DELEGATEMCP_HELPER", "supervise")
	return t.TempDir()
}

// rpc sends one request and returns its response. Responses arrive in order
// here because each test awaits one before sending the next.
func (s *session) rpc(method string, params any) rpcResp {
	s.t.Helper()
	s.nextID++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": s.nextID, "method": method, "params": params})
	if _, err := s.in.Write(append(b, '\n')); err != nil {
		s.t.Fatal(err)
	}
	if !s.lines.Scan() {
		s.t.Fatalf("no response to %s: %v", method, s.lines.Err())
	}
	var r rpcResp
	if err := json.Unmarshal(s.lines.Bytes(), &r); err != nil {
		s.t.Fatalf("bad response %q: %v", s.lines.Text(), err)
	}
	return r
}

type rpcResp struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type toolRes struct {
	Content           []struct{ Type, Text string } `json:"content"`
	StructuredContent Result                        `json:"structuredContent"`
	IsError           bool                          `json:"isError"`
	Raw               json.RawMessage               `json:"-"`
}

func (s *session) call(tool string, args map[string]any) toolRes {
	s.t.Helper()
	r := s.rpc("tools/call", map[string]any{"name": tool, "arguments": args})
	if r.Error != nil {
		s.t.Fatalf("%s: rpc error %v", tool, r.Error)
	}
	var tr toolRes
	if err := json.Unmarshal(r.Result, &tr); err != nil {
		s.t.Fatal(err)
	}
	tr.Raw = r.Result
	return tr
}

func TestStartReturnsAtOnceWithHandle(t *testing.T) {
	s := newSession(t, setup(t), "", nil)
	started := time.Now()
	r := s.call("delegate_to_reviewer", map[string]any{"task": "sleep 30"})
	if time.Since(started) > 3*time.Second {
		t.Errorf("start took %v, want it to return at once", time.Since(started))
	}
	if r.IsError {
		t.Fatalf("tool error: %s", r.Content[0].Text)
	}
	got := r.StructuredContent
	if !strings.HasPrefix(got.TaskID, "dlg-") || got.State != "working" || got.Delegate != "reviewer" || got.ContextID == "" {
		t.Errorf("start = %+v", got)
	}
	// The same as text.
	var asText Result
	if err := json.Unmarshal([]byte(r.Content[0].Text), &asText); err != nil || asText.TaskID != got.TaskID {
		t.Errorf("text content %q does not carry the structured result (%v)", r.Content[0].Text, err)
	}
	s.call("delegate_cancel", map[string]any{"task_id": got.TaskID})
}

func TestBoundedWaitRunsOut(t *testing.T) {
	s := newSession(t, setup(t), "", nil)
	r := s.call("delegate_to_reviewer", map[string]any{"task": "sleep 30", "wait": 1})
	got := r.StructuredContent
	if got.State != "working" || !strings.Contains(got.Message, "still running") {
		t.Errorf("after the wait ran out: %+v", got)
	}
	// A wait past the setting is cut to it.
	s2 := newSession(t, setup(t), "", func(c *Config) { c.MaxWait = time.Second })
	started := time.Now()
	s2.call("delegate_to_reviewer", map[string]any{"task": "sleep 30", "wait": 600})
	if d := time.Since(started); d > 5*time.Second {
		t.Errorf("a wait of 600s on a 1s setting held the call %v", d)
	}
	s.call("delegate_cancel", map[string]any{"task_id": got.TaskID})
}

func TestWaitReturnsTheResult(t *testing.T) {
	s := newSession(t, setup(t), "", nil)
	r := s.call("delegate_to_reviewer", map[string]any{"task": "echo all good", "wait": 5})
	got := r.StructuredContent
	if got.State != "completed" || !strings.Contains(got.Result, "all good") {
		t.Errorf("start with wait = %+v", got)
	}
}

func TestHandleTools(t *testing.T) {
	s := newSession(t, setup(t), "", nil)
	h := s.call("delegate_to_reviewer", map[string]any{"task": "sleep 1; echo done"}).StructuredContent.TaskID

	list := s.call("delegate_list", nil)
	var l ListResult
	b, _ := json.Marshal(json.RawMessage(list.Raw))
	var wrap struct {
		StructuredContent ListResult `json:"structuredContent"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		t.Fatal(err)
	}
	l = wrap.StructuredContent
	if len(l.Delegations) != 1 || l.Delegations[0].TaskID != h {
		t.Errorf("delegate_list = %+v", l)
	}

	if st := s.call("delegate_status", map[string]any{"task_id": h}).StructuredContent; st.State != "working" {
		t.Errorf("status = %+v", st)
	}
	res := s.call("delegate_result", map[string]any{"task_id": h, "wait": 3}).StructuredContent
	if res.State != "completed" || !strings.Contains(res.Result, "done") {
		t.Errorf("result = %+v", res)
	}

	h2 := s.call("delegate_to_reviewer", map[string]any{"task": "sleep 30"}).StructuredContent.TaskID
	c := s.call("delegate_cancel", map[string]any{"task_id": h2}).StructuredContent
	if c.State != "canceled" {
		t.Errorf("cancel = %+v", c)
	}

	// A failed run says why, with the log tail.
	h3 := s.call("delegate_to_reviewer", map[string]any{"task": "echo boom >&2; exit 3"}).StructuredContent.TaskID
	f := s.call("delegate_result", map[string]any{"task_id": h3, "wait": 3}).StructuredContent
	if f.State != "failed" || !strings.Contains(f.Message, "exit code 3") || !strings.Contains(f.LogTail, "boom") {
		t.Errorf("failed = %+v", f)
	}
}

func TestReplyAnswersAQuestion(t *testing.T) {
	s := newSession(t, setup(t), "", nil)
	r := s.call("delegate_to_reviewer", map[string]any{"task": "ask:which branch?", "wait": 5}).StructuredContent
	if r.State != "input-required" || r.Message != "which branch?" {
		t.Fatalf("asking = %+v", r)
	}
	done := s.call("delegate_reply", map[string]any{"task_id": r.TaskID, "message": "main", "wait": 5}).StructuredContent
	if done.State != "completed" || !strings.Contains(done.Result, "you said main") {
		t.Errorf("after the reply = %+v", done)
	}
	if e := s.call("delegate_reply", map[string]any{"task_id": r.TaskID, "message": "again"}); !e.IsError {
		t.Errorf("a reply to a finished task = %+v, want a tool error", e)
	}
}

func TestToolsDeclareOutputSchemas(t *testing.T) {
	s := newSession(t, setup(t), "", nil)
	r := s.rpc("tools/list", map[string]any{})
	var l struct {
		Tools []struct {
			Name         string         `json:"name"`
			OutputSchema map[string]any `json:"outputSchema"`
			InputSchema  map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(r.Result, &l); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range l.Tools {
		names = append(names, tl.Name)
		if tl.OutputSchema["type"] != "object" || tl.InputSchema["type"] != "object" {
			t.Errorf("%s: schemas %v / %v", tl.Name, tl.InputSchema, tl.OutputSchema)
		}
	}
	want := "delegate_cancel delegate_list delegate_reply delegate_result delegate_status delegate_to_reviewer"
	if strings.Join(sorted(names), " ") != want {
		t.Errorf("tools = %v, want %s", names, want)
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestWrongTokenIsAToolError(t *testing.T) {
	s := newSession(t, setup(t), "not-the-token", nil)
	r := s.call("delegate_to_reviewer", map[string]any{"task": "echo hi"})
	if !r.IsError || !strings.Contains(r.Content[0].Text, "401") {
		t.Errorf("wrong token = %+v", r)
	}
	// Nothing was started.
	recs, _ := delegation.List(parent, s.ep.Opts.Folder)
	if len(recs) != 0 {
		t.Errorf("a refused call started %d delegation(s)", len(recs))
	}
}

func TestAnotherParentsHandleIsRefused(t *testing.T) {
	folder := setup(t)
	rec, _, err := delegation.Start(delegation.StartOptions{Parent: "local/other", Delegate: "local/reviewer", Task: "sleep 30", Folder: folder, Supervisor: []string{os.Args[0]}})
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(t, folder, "", nil)
	for _, tool := range []string{"delegate_status", "delegate_result", "delegate_cancel"} {
		if r := s.call(tool, map[string]any{"task_id": rec.Handle}); !r.IsError {
			t.Errorf("%s on another parent's handle = %+v, want a tool error", tool, r)
		}
	}
	if r := s.call("delegate_status", map[string]any{"task_id": "../../etc/passwd"}); !r.IsError {
		t.Errorf("a path as a handle = %+v, want a tool error", r)
	}
	t.Cleanup(func() { stopAll(t) })
}

func TestRestartedParentAnswersForOldHandles(t *testing.T) {
	folder := setup(t)
	first := newSession(t, folder, "", nil)
	done := first.call("delegate_to_reviewer", map[string]any{"task": "echo first"}).StructuredContent.TaskID
	asking := first.call("delegate_to_reviewer", map[string]any{"task": "ask:which branch?", "wait": 5}).StructuredContent
	if asking.State != "input-required" {
		t.Fatalf("asking = %+v", asking)
	}
	// The parent goes away: its server and its endpoints. The delegate's
	// session is detached and goes on.
	_ = first.in.Close()
	_ = first.ep.Close()

	second := newSession(t, folder, "", nil)
	if r := second.call("delegate_result", map[string]any{"task_id": done, "wait": 3}).StructuredContent; r.State != "completed" || !strings.Contains(r.Result, "first") {
		t.Errorf("old handle's result = %+v", r)
	}
	if r := second.call("delegate_status", map[string]any{"task_id": asking.TaskID}).StructuredContent; r.State != "input-required" || r.Message != "which branch?" {
		t.Errorf("old question = %+v", r)
	}
	var wrap struct {
		StructuredContent ListResult `json:"structuredContent"`
	}
	l := second.call("delegate_list", nil)
	_ = json.Unmarshal(l.Raw, &wrap)
	if len(wrap.StructuredContent.Delegations) != 2 {
		t.Errorf("list after a restart = %+v", wrap.StructuredContent)
	}
	// The new endpoint never saw the old task, so the reply goes to the record.
	r := second.call("delegate_reply", map[string]any{"task_id": asking.TaskID, "message": "develop", "wait": 5}).StructuredContent
	if r.State != "completed" || !strings.Contains(r.Result, "you said develop") {
		t.Errorf("reply after a restart = %+v", r)
	}
}

// ---- MCP Tasks

func (s *session) initialize(tasks bool) {
	s.t.Helper()
	caps := map[string]any{}
	if tasks {
		caps["extensions"] = map[string]any{TasksExtension: map[string]any{}}
	}
	r := s.rpc("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": caps})
	if r.Error != nil || !strings.Contains(string(r.Result), TasksExtension) {
		s.t.Fatalf("initialize = %s %v: the server should advertise the tasks extension", r.Result, r.Error)
	}
}

func (s *session) taskGet(id string) map[string]any {
	s.t.Helper()
	r := s.rpc("tasks/get", map[string]any{"taskId": id})
	if r.Error != nil {
		s.t.Fatalf("tasks/get: %v", r.Error)
	}
	var m map[string]any
	_ = json.Unmarshal(r.Result, &m)
	return m
}

func TestTasksPath(t *testing.T) {
	s := newSession(t, setup(t), "", nil)
	s.initialize(true)
	r := s.rpc("tools/call", map[string]any{"name": "delegate_to_reviewer", "arguments": map[string]any{"task": "ask:which branch?"}})
	var task map[string]any
	_ = json.Unmarshal(r.Result, &task)
	id, _ := task["taskId"].(string)
	if task["resultType"] != "task" || !strings.HasPrefix(id, "dlg-") || task["status"] != "working" {
		t.Fatalf("a Tasks client's call = %v, want a task at once", task)
	}
	var got map[string]any
	for i := 0; i < 100; i++ {
		got = s.taskGet(id)
		if got["status"] == "input_required" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got["status"] != "input_required" || got["inputRequests"] == nil {
		t.Fatalf("tasks/get = %v, want input_required with inputRequests", got)
	}
	u := s.rpc("tasks/update", map[string]any{"taskId": id, "inputResponses": map[string]any{"q1": map[string]any{"action": "accept", "content": map[string]any{"answer": "main"}}}})
	if u.Error != nil {
		t.Fatalf("tasks/update: %v", u.Error)
	}
	for i := 0; i < 100; i++ {
		got = s.taskGet(id)
		if got["status"] == "completed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got["status"] != "completed" || !strings.Contains(fmt.Sprint(got["result"]), "you said main") {
		t.Errorf("tasks/get after the answer = %v", got)
	}

	// Cancel.
	r = s.rpc("tools/call", map[string]any{"name": "delegate_to_reviewer", "arguments": map[string]any{"task": "sleep 30"}})
	_ = json.Unmarshal(r.Result, &task)
	c := s.rpc("tasks/cancel", map[string]any{"taskId": task["taskId"]})
	var cv map[string]any
	_ = json.Unmarshal(c.Result, &cv)
	if cv["status"] != "cancelled" {
		t.Errorf("tasks/cancel = %v", cv)
	}
	if e := s.rpc("tasks/get", map[string]any{"taskId": "dlg-0000000000000000"}); e.Error == nil {
		t.Error("tasks/get of an unknown task should be an error")
	}
}

func TestTasksClientCapabilitiesPerRequest(t *testing.T) {
	// A 2026-07-28 client has no initialize: it says what it offers on each request.
	s := newSession(t, setup(t), "", nil)
	r := s.rpc("tools/call", map[string]any{
		"name": "delegate_to_reviewer", "arguments": map[string]any{"task": "sleep 30"},
		"_meta": map[string]any{clientCapsMeta: map[string]any{"extensions": map[string]any{TasksExtension: map[string]any{}}}},
	})
	var task map[string]any
	_ = json.Unmarshal(r.Result, &task)
	if task["resultType"] != "task" {
		t.Errorf("per-request capabilities: %v, want a task", task)
	}
	_ = s.rpc("tasks/cancel", map[string]any{"taskId": task["taskId"]})
}

func TestToolName(t *testing.T) {
	for in, want := range map[string]string{"reviewer": "delegate_to_reviewer", "Code-Reviewer": "delegate_to_code_reviewer", "a.b": "delegate_to_a_b"} {
		if got := ToolName(in); got != want {
			t.Errorf("ToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- elicitation during a wait (a client with elicitation but no Tasks)

// callServing is call for a client that answers the server's own requests: each
// elicitation/create it reads before the call's response goes to onElicit,
// whose result (or error message, when errMsg is set) is sent back.
func (s *session) callServing(tool string, args map[string]any, onElicit func(message string) (result any, errMsg string)) (toolRes, int) {
	s.t.Helper()
	s.nextID++
	id := s.nextID
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args, "_meta": map[string]any{clientCapsMeta: map[string]any{"elicitation": map[string]any{}}}}})
	if _, err := s.in.Write(append(b, '\n')); err != nil {
		s.t.Fatal(err)
	}
	asked := 0
	for s.lines.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Message string `json:"message"`
			} `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(s.lines.Bytes(), &m); err != nil {
			s.t.Fatalf("bad line %q: %v", s.lines.Text(), err)
		}
		if m.Method == "elicitation/create" {
			asked++
			res, errMsg := onElicit(m.Params.Message)
			reply := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(m.ID)}
			if errMsg != "" {
				reply["error"] = map[string]any{"code": -32601, "message": errMsg}
			} else {
				reply["result"] = res
			}
			rb, _ := json.Marshal(reply)
			if _, err := s.in.Write(append(rb, '\n')); err != nil {
				s.t.Fatal(err)
			}
			continue
		}
		var tr toolRes
		if err := json.Unmarshal(m.Result, &tr); err != nil {
			s.t.Fatal(err)
		}
		return tr, asked
	}
	s.t.Fatalf("no response to %s: %v", tool, s.lines.Err())
	return toolRes{}, 0
}

func TestElicitationDuringAWaitAnswersTheQuestion(t *testing.T) {
	s := newSession(t, setup(t), "", nil)
	var got string
	r, asked := s.callServing("delegate_to_reviewer", map[string]any{"task": "ask:which branch?", "wait": 10}, func(m string) (any, string) {
		got = m
		return map[string]any{"action": "accept", "content": map[string]any{"answer": "main"}}, ""
	})
	if asked != 1 || !strings.Contains(got, "which branch?") {
		t.Errorf("elicited %d times, message %q", asked, got)
	}
	if r.StructuredContent.State != "completed" || !strings.Contains(r.StructuredContent.Result, "you said main") {
		t.Errorf("after the elicited answer = %+v", r.StructuredContent)
	}
}

func TestADeclinedElicitationLeavesTheQuestionForTheModel(t *testing.T) {
	// Copilot CLI's -p declines at once, with no one to ask. That is not the
	// delegate's answer: the question stays open, and the parent's model
	// answers it with delegate_reply.
	s := newSession(t, setup(t), "", nil)
	r, asked := s.callServing("delegate_to_reviewer", map[string]any{"task": "ask:which branch?", "wait": 10}, func(string) (any, string) {
		return map[string]any{"action": "decline"}, ""
	})
	sc := r.StructuredContent
	if asked != 1 || sc.State != "input-required" || !strings.HasPrefix(sc.Message, "which branch?") || !strings.Contains(sc.Message, "delegate_reply") {
		t.Fatalf("declined: asked %d, %+v", asked, sc)
	}
	done := s.call("delegate_reply", map[string]any{"task_id": sc.TaskID, "message": "main", "wait": 5}).StructuredContent
	if done.State != "completed" || !strings.Contains(done.Result, "you said main") {
		t.Errorf("after delegate_reply = %+v", done)
	}
}

func TestARefusedElicitationLeavesTheQuestionForTheModel(t *testing.T) {
	// Cursor's CLI declines elicitation without asking: the request comes back
	// as an error. The question is not lost: it is in the result, and the
	// parent's model answers it with delegate_reply on the same task.
	s := newSession(t, setup(t), "", nil)
	r, asked := s.callServing("delegate_to_reviewer", map[string]any{"task": "ask:which branch?", "wait": 10}, func(string) (any, string) {
		return nil, "elicitation is not supported"
	})
	if asked != 1 || r.StructuredContent.State != "input-required" || !strings.HasPrefix(r.StructuredContent.Message, "which branch?") {
		t.Fatalf("refused: asked %d, %+v", asked, r.StructuredContent)
	}
	// Waiting again does not put the same question to the client twice.
	again, asked := s.callServing("delegate_status", map[string]any{"task_id": r.StructuredContent.TaskID, "wait": 1}, func(string) (any, string) {
		return nil, "elicitation is not supported"
	})
	if asked != 0 || again.StructuredContent.State != "input-required" {
		t.Errorf("again: asked %d, %+v", asked, again.StructuredContent)
	}
	done := s.call("delegate_reply", map[string]any{"task_id": r.StructuredContent.TaskID, "message": "develop", "wait": 5}).StructuredContent
	if done.State != "completed" || !strings.Contains(done.Result, "you said develop") {
		t.Errorf("after delegate_reply = %+v", done)
	}
}

func TestNoElicitationCapabilityMeansNoElicitation(t *testing.T) {
	// A client that does not offer it (claude -p) is never sent the request:
	// call() reads exactly one line per request, so an extra line would fail
	// the next call.
	s := newSession(t, setup(t), "", nil)
	r := s.call("delegate_to_reviewer", map[string]any{"task": "ask:which branch?", "wait": 2}).StructuredContent
	if r.State != "input-required" {
		t.Fatalf("headless = %+v", r)
	}
	if again := s.call("delegate_status", map[string]any{"task_id": r.TaskID}).StructuredContent; again.State != "input-required" {
		t.Errorf("status = %+v", again)
	}
}

func TestAQuestionNobodyAnswersLapsesWithTheDelegatesWait(t *testing.T) {
	// The delegate waits one second for its parent, then goes on: the parent,
	// which never answered, finds the delegation completed and its question
	// still on the record.
	s := newSession(t, setup(t), "", nil)
	r := s.call("delegate_to_reviewer", map[string]any{"task": "askshort:which branch?", "wait": 5}).StructuredContent
	if r.State != "input-required" {
		t.Fatalf("asking = %+v", r)
	}
	var done Result
	for i := 0; i < 100; i++ {
		done = s.call("delegate_status", map[string]any{"task_id": r.TaskID}).StructuredContent
		if done.State == "completed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if done.State != "completed" || !strings.Contains(done.Result, "no answer: unanswered") {
		t.Errorf("after the delegate's wait ran out = %+v", done)
	}
	path, _ := delegation.FindHandle(r.TaskID)
	rec, _ := delegation.Load(path)
	if len(rec.Questions) != 1 || rec.Questions[0].Answer != nil {
		t.Errorf("questions = %+v", rec.Questions)
	}
}
