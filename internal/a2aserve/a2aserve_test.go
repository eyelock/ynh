package a2aserve

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/delegation/delegationtest"
	"github.com/eyelock/ynh/internal/parentmcp"
)

// The delegate's session is this test binary in a helper mode, and its "ynh
// run" is `sh -c <task>`, except that a task "ask:<question>" is a delegate
// that asks its parent a question through the delegation record and waits for
// the answer there.
func TestMain(m *testing.M) {
	switch os.Getenv("A2ASERVE_HELPER") {
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
				c.Env = append(os.Environ(), "A2ASERVE_HELPER=ask", "A2ASERVE_RECORD="+delegation.RecordPath(r.Parent, r.Folder, r.Handle), "A2ASERVE_QUESTION="+q)
				if short {
					c.Env = append(c.Env, "A2ASERVE_WAIT=short")
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
		path := os.Getenv("A2ASERVE_RECORD")
		wait := 20 * time.Second
		if os.Getenv("A2ASERVE_WAIT") != "" {
			wait = time.Second
		}
		o, err := parentmcp.Ask(context.Background(), path, os.Getenv("A2ASERVE_QUESTION"), "", false, wait, 20*time.Millisecond)
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

func setup(t *testing.T) (folder string) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("A2ASERVE_HELPER", "supervise")
	folder = t.TempDir()
	// Registered after the temp folders, so it runs before they are removed:
	// a delegation's supervisor still writes its record after the run ends.
	t.Cleanup(func() { delegationtest.StopAll(t) })
	return folder
}

func open(t *testing.T, folder, name string) *Endpoint {
	t.Helper()
	e, err := Open(Options{
		Name: name, Label: "github.com/x/" + name, Parent: "local/parent", Delegate: "local/" + name,
		Folder: folder, Supervisor: []string{os.Args[0]},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func records(t *testing.T) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(delegation.Dir(), "*", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func client(t *testing.T, e *Endpoint, token string) (*a2aclient.Client, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	card, err := agentcard.DefaultResolver.Resolve(ctx, e.CardURL)
	if err != nil {
		t.Fatal(err)
	}
	store := a2aclient.NewInMemoryCredentialsStore()
	store.Set("s", "bearer", a2aclient.AuthCredential(token))
	c, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithCallInterceptors(&a2aclient.AuthInterceptor{Service: store}))
	if err != nil {
		t.Fatal(err)
	}
	return c, a2aclient.AttachSessionID(ctx, "s")
}

func send(t *testing.T, c *a2aclient.Client, ctx context.Context, taskID, text string, async bool) *a2a.Task {
	t.Helper()
	m := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text))
	if taskID != "" {
		m.TaskID = a2a.TaskID(taskID)
	}
	req := &a2a.SendMessageRequest{Message: m}
	if async {
		req.Config = &a2a.SendMessageConfig{ReturnImmediately: true}
	}
	r, err := c.SendMessage(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	task, ok := r.(*a2a.Task)
	if !ok {
		t.Fatalf("want a task, got %T", r)
	}
	return task
}

func artifactText(task *a2a.Task) string {
	var b strings.Builder
	for _, a := range task.Artifacts {
		for _, p := range a.Parts {
			b.WriteString(p.Text())
		}
	}
	return b.String()
}

func waitState(t *testing.T, c *a2aclient.Client, ctx context.Context, id a2a.TaskID, want a2a.TaskState) *a2a.Task {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		task, err := c.GetTask(ctx, &a2a.GetTaskRequest{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if task.Status.State == want {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task stayed %s, want %s", task.Status.State, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestACardFetchAndARefusedCallStartNoSession(t *testing.T) {
	folder := setup(t)
	e := open(t, folder, "reviewer")

	resp, err := http.Get(e.CardURL + "/.well-known/agent-card.json")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("card: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m","role":"ROLE_USER","parts":[{"text":"echo hi"}]}}}`
	for name, auth := range map[string]string{"wrong token": "Bearer nope", "no token": ""} {
		req, _ := http.NewRequest("POST", e.CardURL+"/a2a", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: HTTP %d, want 401", name, resp.StatusCode)
		}
	}
	if got := records(t); len(got) != 0 {
		t.Fatalf("a card fetch or a refused call wrote a record: %v", got)
	}
}

func TestWrongTokenThroughTheClientIsRefused(t *testing.T) {
	folder := setup(t)
	e := open(t, folder, "reviewer")
	c, ctx := client(t, e, "wrong")
	_, err := c.SendMessage(ctx, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("echo hi"))})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want a 401, got %v", err)
	}
	if len(records(t)) != 0 {
		t.Fatal("a refused call started a session")
	}
}

func TestFirstMessageStartsTheSessionAndStatusFollowsIt(t *testing.T) {
	folder := setup(t)
	e := open(t, folder, "reviewer")
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "echo hello", false)
	if task.Status.State != a2a.TaskStateCompleted || artifactText(task) != "hello\n" {
		t.Fatalf("got %s %q", task.Status.State, artifactText(task))
	}
	if task.ID == "" || task.ContextID == "" {
		t.Fatalf("a result must carry the task and context ids: %q %q", task.ID, task.ContextID)
	}
	if len(records(t)) != 1 {
		t.Fatalf("want one record, got %v", records(t))
	}
	got := waitState(t, c, ctx, task.ID, a2a.TaskStateCompleted)
	if artifactText(got) != "hello\n" {
		t.Fatalf("status: %q", artifactText(got))
	}
}

func TestTaskCarriesTheDelegationHandle(t *testing.T) {
	folder := setup(t)
	e := open(t, folder, "reviewer")
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "sleep 30", true)
	handle, _ := task.Metadata[MetaHandle].(string)
	recs := records(t)
	if len(recs) != 1 || handle == "" || !strings.HasSuffix(recs[0], handle+".json") {
		t.Fatalf("the first task's metadata handle %q does not name the record %v", handle, recs)
	}
	// And a later read of the task still carries it.
	if got := waitState(t, c, ctx, task.ID, a2a.TaskStateWorking); got.Metadata[MetaHandle] != handle {
		t.Errorf("a read of the task lost the handle: %v", got.Metadata)
	}
	path, err := delegation.FindHandle(handle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := delegation.Cancel(path); err != nil {
		t.Fatal(err)
	}
}

func TestInputRequiredIsAnsweredOnTheSameTask(t *testing.T) {
	folder := setup(t)
	e := open(t, folder, "reviewer")
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "ask:Which colour?", false)
	if task.Status.State != a2a.TaskStateInputRequired || task.Status.Message == nil || messageText(task.Status.Message) != "Which colour?" {
		t.Fatalf("got %s %+v", task.Status.State, task.Status.Message)
	}
	done := send(t, c, ctx, string(task.ID), "blue", false)
	if done.ID != task.ID || done.Status.State != a2a.TaskStateCompleted || !strings.Contains(artifactText(done), "you said blue") {
		t.Fatalf("got %s %q on %s", done.Status.State, artifactText(done), done.ID)
	}
}

func TestCancelStopsTheRunsProcessGroup(t *testing.T) {
	folder := setup(t)
	e := open(t, folder, "reviewer")
	c, ctx := client(t, e, e.Token)
	// A non-blocking send returns at once; the task goes on.
	task := send(t, c, ctx, "", "sleep 60", true)
	waitState(t, c, ctx, task.ID, a2a.TaskStateWorking)
	rec := waitRecord(t, func(r *delegation.Record) bool { return r.Status == delegation.StatusRunning && r.Pgid > 1 })
	got, err := c.CancelTask(ctx, &a2a.CancelTaskRequest{ID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("got %s", got.Status.State)
	}
	deadline := time.Now().Add(10 * time.Second)
	for syscall.Kill(-rec.Pgid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if syscall.Kill(-rec.Pgid, 0) == nil {
		t.Fatal("the run's process group is still alive after cancel")
	}
	// Let the session finish writing before the temp dir goes.
	for i := 0; i < 250 && syscall.Kill(rec.Pid, 0) == nil; i++ {
		time.Sleep(20 * time.Millisecond)
	}
}

func waitRecord(t *testing.T, ok func(*delegation.Record) bool) *delegation.Record {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range records(t) {
			if r, err := delegation.Load(p); err == nil && ok(r) {
				return r
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("no record reached the wanted state")
	return nil
}

func TestAFailedTaskCarriesTheLogTail(t *testing.T) {
	folder := setup(t)
	e := open(t, folder, "reviewer")
	c, ctx := client(t, e, e.Token)
	// Nothing on stdout: the reason is only in the log.
	task := send(t, c, ctx, "", "echo oops-from-stderr >&2; exit 3", false)
	if task.Status.State != a2a.TaskStateFailed || task.Status.Message == nil {
		t.Fatalf("got %s %+v", task.Status.State, task.Status.Message)
	}
	msg := messageText(task.Status.Message)
	if !strings.Contains(msg, "oops-from-stderr") || !strings.Contains(msg, "exit code 3") {
		t.Fatalf("the failure message lacks the log tail and exit code: %q", msg)
	}
}

func TestLogTailKeepsOnlyTheLastLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	_ = os.WriteFile(p, []byte(b.String()), 0o600)
	got := LogTail(p, LogTailLines)
	lines := strings.Split(got, "\n")
	if len(lines) != LogTailLines || lines[0] != "line 61" || lines[len(lines)-1] != "line 100" {
		t.Fatalf("got %d lines, %q ... %q", len(lines), lines[0], lines[len(lines)-1])
	}
}

func TestADelegateDoesNotInheritItsParentsEndpoints(t *testing.T) {
	folder := setup(t)
	t.Setenv("YNH_DELEGATES", "other")
	t.Setenv("YNH_DELEGATE_OTHER_CARD", "http://127.0.0.1:1")
	t.Setenv("YNH_DELEGATE_OTHER_AUTH", "Bearer secret")
	t.Setenv("A2ACLI_AGENT_CARD", "http://127.0.0.1:1")
	t.Setenv("A2ACLI_AUTH", "Bearer secret")
	t.Setenv("KEEP_ME", "yes")
	e := open(t, folder, "reviewer")
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "env", false)
	out := artifactText(task)
	if strings.Contains(out, "YNH_DELEGATE") || strings.Contains(out, "A2ACLI_") || strings.Contains(out, "secret") {
		t.Fatalf("the delegate inherited its parent's endpoints:\n%s", out)
	}
	if !strings.Contains(out, "KEEP_ME=yes") {
		t.Fatal("the delegate lost its ordinary environment")
	}
}

func ep(name, card, token string) *Endpoint {
	return &Endpoint{Opts: Options{Name: name, Label: "github.com/x/" + name}, CardURL: card, Token: token}
}

func TestEnvForOneDelegateAlsoSetsTheA2ACLIValues(t *testing.T) {
	env, err := Env([]*Endpoint{ep("code-reviewer", "http://127.0.0.1:5001", "tok1")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"YNH_DELEGATES=code-reviewer",
		"YNH_DELEGATE_CODE_REVIEWER_CARD=http://127.0.0.1:5001",
		"YNH_DELEGATE_CODE_REVIEWER_AUTH=Bearer tok1",
		"A2ACLI_AGENT_CARD=http://127.0.0.1:5001",
		"A2ACLI_AUTH=Bearer tok1",
	}
	if strings.Join(env, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(env, "\n"), strings.Join(want, "\n"))
	}
}

func TestEnvForSeveralDelegatesSetsNoA2ACLIValues(t *testing.T) {
	env, err := Env([]*Endpoint{ep("tester", "http://127.0.0.1:5002", "t2"), ep("reviewer", "http://127.0.0.1:5001", "t1")})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "A2ACLI_") {
		t.Fatalf("the a2a CLI would silently pick one:\n%s", joined)
	}
	for _, w := range []string{"YNH_DELEGATES=reviewer,tester", "YNH_DELEGATE_REVIEWER_CARD=http://127.0.0.1:5001", "YNH_DELEGATE_TESTER_AUTH=Bearer t2"} {
		if !strings.Contains(joined, w) {
			t.Errorf("missing %q in\n%s", w, joined)
		}
	}
}

func TestEnvWithNoDelegatesSetsNothing(t *testing.T) {
	if env, err := Env(nil); err != nil || len(env) != 0 {
		t.Fatalf("got %v %v", env, err)
	}
}

func TestEnvNameCollisionIsRefusedNamingBoth(t *testing.T) {
	_, err := Env([]*Endpoint{ep("code-reviewer", "c1", "t1"), ep("code.reviewer", "c2", "t2")})
	if err == nil {
		t.Fatal("a collision was accepted")
	}
	for _, w := range []string{"github.com/x/code-reviewer", "github.com/x/code.reviewer", "CODE_REVIEWER"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("the refusal does not name %q: %v", w, err)
		}
	}
}

func TestEnvName(t *testing.T) {
	for in, want := range map[string]string{"reviewer": "REVIEWER", "code-reviewer": "CODE_REVIEWER", "a.b/c d": "A_B_C_D", "v2": "V2"} {
		if got := EnvName(in); got != want {
			t.Errorf("EnvName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEachEndpointHasItsOwnTokenAndPort(t *testing.T) {
	folder := setup(t)
	a, b := open(t, folder, "a"), open(t, folder, "b")
	if a.Token == b.Token || a.CardURL == b.CardURL || len(a.Token) < 32 {
		t.Fatalf("endpoints share or weaken credentials: %q %q", a.Token, b.Token)
	}
	// One endpoint's token does not open the other.
	c, ctx := client(t, b, a.Token)
	if _, err := c.SendMessage(ctx, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("echo hi"))}); err == nil {
		t.Fatal("another delegate's token was accepted")
	}
}

func TestAQuestionNobodyAnswersEndsWithTheDelegatesWait(t *testing.T) {
	// The a2a CLI route: the task shows input-required while the delegate
	// waits, and carries on to completion when its wait runs out.
	folder := setup(t)
	e := open(t, folder, "reviewer")
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "askshort:Which colour?", false)
	if task.Status.State != a2a.TaskStateInputRequired {
		t.Fatalf("got %s", task.Status.State)
	}
	// The task shows input-required until it is next spoken to; a message
	// then is not an answer, and the task follows the record to its end.
	waitRecord(t, func(r *delegation.Record) bool { return r.Status.Final() })
	done := send(t, c, ctx, string(task.ID), "never mind", false)
	if done.Status.State != a2a.TaskStateCompleted || !strings.Contains(artifactText(done), "no answer: unanswered") {
		t.Errorf("got %s %q", done.Status.State, artifactText(done))
	}
}
