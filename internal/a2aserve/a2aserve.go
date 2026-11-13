// Package a2aserve opens a localhost A2A endpoint in front of a delegate
// (docs/delegate-sessions.md). An endpoint belongs to one delegates_to entry of
// the harness being run, has its own bearer token, and starts the delegate's
// detached session (package delegation) only when its first message arrives:
// fetching its agent card, or being refused for a wrong token, starts nothing.
//
// The endpoint is the parent's whole view of the delegate. Task state, cancel
// and a question the delegate asks all map onto the delegation record, which
// the session writes as it goes.
package a2aserve

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/eyelock/ynh/internal/delegation"
)

// LogTailLines is how much of a failed run's log goes into the task's status
// message, so a delegate that wrote nothing to its output still says why.
const LogTailLines = 40

// pollEvery is how often the executor reads the delegation record.
var pollEvery = 150 * time.Millisecond

// Options describe one endpoint.
type Options struct {
	// Name is the delegate's name (its harness name), used for the card and
	// for the environment names.
	Name string
	// Label names the delegates_to entry in messages.
	Label string
	// Parent and Delegate are the canonical ids (see delegation.StartOptions).
	Parent, Delegate string
	Profile, Focus   string
	// Folder is the working folder the delegate runs in: the parent's.
	Folder string
	// Supervisor starts the session process (see delegation.StartOptions).
	Supervisor []string
}

// Endpoint is one open A2A endpoint.
type Endpoint struct {
	Opts Options
	// CardURL is what a client is given: the base URL, from which the agent
	// card is fetched. Token is the bearer token every call but the card needs.
	CardURL string
	Token   string

	srv *http.Server
	ex  *executor
}

// Open starts an endpoint on 127.0.0.1 on a free port, with a fresh token.
func Open(o Options) (*Endpoint, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, fmt.Errorf("a2a endpoint for %s: token: %w", o.Label, err)
	}
	token := hex.EncodeToString(b[:])
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("a2a endpoint for %s: %w", o.Label, err)
	}
	base := "http://" + ln.Addr().String()
	card := &a2a.AgentCard{
		Name:                o.Name,
		Description:         "ynh delegate " + o.Label,
		SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(base+"/a2a", a2a.TransportProtocolJSONRPC)},
		DefaultInputModes:   []string{"text/plain"},
		DefaultOutputModes:  []string{"text/plain"},
		Capabilities:        a2a.AgentCapabilities{Streaming: true},
		Skills:              []a2a.AgentSkill{{ID: "run", Name: o.Name, Description: "runs the delegate " + o.Label + " as its own session", Tags: []string{"delegate"}}},
		SecuritySchemes:     a2a.NamedSecuritySchemes{"bearer": a2a.HTTPAuthSecurityScheme{Scheme: "Bearer"}},
		SecurityRequirements: a2a.SecurityRequirementsOptions{
			{"bearer": a2a.SecuritySchemeScopes{}},
		},
	}
	ex := &executor{o: o, paths: map[a2a.TaskID]string{}}
	mux := http.NewServeMux()
	mux.Handle("/a2a", a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(ex)))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	e := &Endpoint{Opts: o, CardURL: base, Token: token, ex: ex,
		srv: &http.Server{Handler: bearer(token, mux), ReadHeaderTimeout: 10 * time.Second}}
	go func() { _ = e.srv.Serve(ln) }()
	return e, nil
}

// Close stops the endpoint. A delegation already running is a detached
// session and goes on (delegation.Start); only the way to reach it closes.
func (e *Endpoint) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := e.srv.Shutdown(ctx)
	if err != nil {
		err = e.srv.Close()
	}
	return err
}

// bearer refuses every request but the agent card's without the token.
func bearer(token string, next http.Handler) http.Handler {
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == a2asrv.WellKnownAgentCardPath {
			next.ServeHTTP(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type executor struct {
	o     Options
	mu    sync.Mutex
	paths map[a2a.TaskID]string // task -> delegation record path
}

func (x *executor) path(id a2a.TaskID) string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.paths[id]
}

func messageText(m *a2a.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		b.WriteString(p.Text())
	}
	return b.String()
}

func agentMessage(ec a2a.TaskInfoProvider, text string) *a2a.Message {
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewTextPart(text))
}

// Execute runs one message. A message with no task starts the delegation (the
// session starts here, never before); a message on a task that asked a question
// answers it. Either way it then follows the record until the task ends or
// needs input.
func (x *executor) Execute(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		var path string
		if ec.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
				return
			}
			_, p, err := delegation.Start(delegation.StartOptions{
				Parent: x.o.Parent, Delegate: x.o.Delegate, Task: messageText(ec.Message),
				Profile: x.o.Profile, Focus: x.o.Focus, Folder: x.o.Folder, Supervisor: x.o.Supervisor,
			})
			if err != nil {
				yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateFailed, agentMessage(ec, err.Error())), nil)
				return
			}
			path = p
			x.mu.Lock()
			x.paths[ec.TaskID] = p
			x.mu.Unlock()
		} else {
			path = x.path(ec.TaskID)
			if path == "" {
				yield(nil, errors.New("this endpoint has no record of that task"))
				return
			}
			if err := answer(path, messageText(ec.Message)); err != nil {
				yield(nil, err)
				return
			}
		}
		if !yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateWorking, nil), nil) {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollEvery):
			}
			rec, err := delegation.Reconcile(path)
			if err != nil {
				yield(nil, err)
				return
			}
			switch rec.Status {
			case delegation.StatusSucceeded:
				if !yield(a2a.NewArtifactEvent(ec, a2a.NewTextPart(rec.Result)), nil) {
					return
				}
				yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, nil), nil)
				return
			case delegation.StatusFailed:
				yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateFailed, agentMessage(ec, failureText(rec))), nil)
				return
			case delegation.StatusCancelled:
				yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCanceled, nil), nil)
				return
			}
			if q := openQuestion(rec); q != nil {
				yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateInputRequired, agentMessage(ec, q.Text)), nil)
				return
			}
		}
	}
}

// Cancel stops the delegation's process group and ends the task.
func (x *executor) Cancel(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if p := x.path(ec.TaskID); p != "" {
			if _, err := delegation.Cancel(p); err != nil {
				yield(nil, err)
				return
			}
		}
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCanceled, nil), nil)
	}
}

// openQuestion is the first question the delegate asked that has no answer.
func openQuestion(rec *delegation.Record) *delegation.Question {
	for i := range rec.Questions {
		if rec.Questions[i].Answer == nil {
			return &rec.Questions[i]
		}
	}
	return nil
}

// answer records text as the answer to the open question.
func answer(path, text string) error {
	var found bool
	_, err := delegation.Update(path, func(r *delegation.Record) {
		for i := range r.Questions {
			if r.Questions[i].Answer == nil {
				now := time.Now().UTC()
				r.Questions[i].Answer, r.Questions[i].AnsweredAt = &text, &now
				found = true
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if !found {
		return errors.New("the delegate has no open question: a message on a task is an answer, and this task is not asking")
	}
	return nil
}

// failureText is why a task failed: the record's error, then the tail of the
// run's log, so a delegate that wrote nothing to its output still says why.
func failureText(rec *delegation.Record) string {
	var parts []string
	if rec.Error != "" {
		parts = append(parts, rec.Error)
	}
	if rec.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit code %d", *rec.ExitCode))
	}
	if tail := logTail(rec.Log, LogTailLines); tail != "" {
		parts = append(parts, "log tail:\n"+tail)
	}
	if len(parts) == 0 {
		return "the delegate failed and left no output"
	}
	return strings.Join(parts, "\n")
}

// logTail is the last n lines of the file at path, or "" when it cannot be read.
func logTail(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// EnvName is the part of an environment variable's name that identifies the
// delegate: its name upper-cased, every character outside A-Z and 0-9 turned
// into an underscore.
func EnvName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Env is the environment the parent's session gets for its open endpoints:
// YNH_DELEGATES (the names, comma-separated), and per delegate
// YNH_DELEGATE_<NAME>_CARD and YNH_DELEGATE_<NAME>_AUTH. With exactly one
// delegate it also sets A2ACLI_AGENT_CARD and A2ACLI_AUTH, which the a2a CLI
// reads by itself; with several it sets neither, so the CLI never silently
// picks one. Two entries whose names map to the same <NAME> are refused,
// naming both.
func Env(eps []*Endpoint) ([]string, error) {
	if len(eps) == 0 {
		return nil, nil
	}
	seen := map[string]*Endpoint{}
	for _, e := range eps {
		k := EnvName(e.Opts.Name)
		if prev, dup := seen[k]; dup {
			return nil, fmt.Errorf("delegates_to entries %s and %s both map to the environment name %s (from %q and %q)\n  Fix: give the delegates different names, or set transport: native on one",
				prev.Opts.Label, e.Opts.Label, k, prev.Opts.Name, e.Opts.Name)
		}
		seen[k] = e
	}
	sorted := append([]*Endpoint(nil), eps...)
	sort.Slice(sorted, func(i, j int) bool { return EnvName(sorted[i].Opts.Name) < EnvName(sorted[j].Opts.Name) })
	var names []string
	var env []string
	for _, e := range sorted {
		k := EnvName(e.Opts.Name)
		names = append(names, e.Opts.Name)
		env = append(env, "YNH_DELEGATE_"+k+"_CARD="+e.CardURL, "YNH_DELEGATE_"+k+"_AUTH=Bearer "+e.Token)
	}
	env = append([]string{"YNH_DELEGATES=" + strings.Join(names, ",")}, env...)
	if len(sorted) == 1 {
		env = append(env, "A2ACLI_AGENT_CARD="+sorted[0].CardURL, "A2ACLI_AUTH=Bearer "+sorted[0].Token)
	}
	return env, nil
}
