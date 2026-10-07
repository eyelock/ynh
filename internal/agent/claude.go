package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/vendor"
)

// ClaudeBackend implements WorkerBackend for Claude Code CLI.
// All stream-json wire-format details are encapsulated here — the loop
// driver never touches them.
type ClaudeBackend struct{}

func (b *ClaudeBackend) Name() string { return "claude" }

// Start spawns a claude subprocess in stream-json mode.
func (b *ClaudeBackend) Start(ctx context.Context, opts StartOptions) (WorkerSession, error) {
	claudeBin, err := lookWorkerCLI(b.Name())
	if err != nil {
		return nil, err
	}

	args := buildClaudeStreamArgs(opts)

	// Resume token = claude session id. We control it explicitly: a fresh run
	// passes --session-id <uuid> so the id is known up front (and persisted to
	// the checkpoint before the first turn); a resume passes --resume <uuid> to
	// reload the prior conversation. claude persists sessions on disk by
	// default, keyed by cwd — which is stable across relaunches (WorktreeDir).
	sessionID := opts.ResumeToken
	if sessionID == "" {
		sessionID = newClaudeSessionID()
		args = append(args, "--session-id", sessionID)
	} else {
		args = append(args, "--resume", sessionID)
	}

	var cmd *exec.Cmd
	cleanup := func() {}
	if opts.Sandbox == "srt" {
		policy := claudeSrtPolicy(workerEnvFor(opts.Env))
		cmd, cleanup, err = srtCommand(ctx, opts, policy, claudeBin, args)
		if err != nil {
			return nil, err
		}
	} else {
		cmd = exec.CommandContext(ctx, claudeBin, args...)
	}

	if opts.WorktreeDir != "" {
		cmd.Dir = opts.WorktreeDir
	}
	cmd.Env = workerEnvFor(opts.Env)
	tail := &stderrTail{}
	cmd.Stderr = stderrSink(opts.Stderr, tail)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, fmt.Errorf("starting claude: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 2<<20), 2<<20) // 2 MB — handles large tool outputs

	s := &claudeSession{
		cmd:       cmd,
		stdin:     stdinPipe,
		scanner:   scanner,
		sessionID: sessionID,
		stderr:    tail,
		wantMode:  claudePermissionMode(opts.AutoApprove),
		cleanup:   cleanup,
		// A fresh session's running cost starts at zero. A resumed one may
		// continue from the total its transcript saved, so it is asked.
		costBaseKnown: opts.ResumeToken == "",
	}

	// Ask claude what it runs with, before the first turn. Both answers are
	// best effort: a write that fails means claude has already exited, and
	// the first Send reports how.
	_ = s.writeLine(claudeControlRequest(claudeSettingsRequest, "get_settings"))
	if !s.costBaseKnown {
		_ = s.writeLine(claudeControlRequest(claudeUsageRequest, "get_usage"))
	}
	return s, nil
}

// Request ids for the control requests a session sends at start.
const (
	claudeSettingsRequest = "ynh-get-settings"
	claudeUsageRequest    = "ynh-get-usage"
)

// claudeControlRequest returns one stream-json control request line.
func claudeControlRequest(id, subtype string) []byte {
	data, _ := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": id,
		"request":    map[string]string{"subtype": subtype},
	})
	return append(data, '\n')
}

// newClaudeSessionID returns a fresh RFC 4122 version-4 UUID string, the
// format claude's --session-id flag expects.
func newClaudeSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// buildClaudeStreamArgs constructs arguments for stream-json mode.
func buildClaudeStreamArgs(opts StartOptions) []string {
	// claude-code refuses --print --output-format=stream-json without
	// --verbose. The flag is mandatory for the streaming wire format the
	// loop driver depends on, so always include it.
	args := []string{
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--print",
		"--verbose",
	}

	if opts.ConfigPath != "" {
		pluginDir := filepath.Join(opts.ConfigPath, (&vendor.Claude{}).ConfigDir())
		args = append(args, "--plugin-dir", pluginDir, "--add-dir", opts.ConfigPath)

		instructionsPath := filepath.Join(opts.ConfigPath, "CLAUDE.md")
		if data, err := os.ReadFile(instructionsPath); err == nil && len(data) > 0 {
			args = append(args, "--append-system-prompt", string(data))
		}
	}

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	// The neutral levels are Claude Code's own words for them.
	if opts.Effort != "" {
		args = append(args, "--effort", opts.Effort)
	}

	// No --auto-approve, no permission flag: the worker gets what claude and
	// the project grant it, and nothing more.
	if mode := claudePermissionMode(opts.AutoApprove); mode != "" {
		args = append(args, "--permission-mode", mode)
	}

	// The relay's settings again, at the highest precedence ynh can set
	// for one session, so no settings file but the organisation's managed
	// settings can turn content on or move the endpoint.
	if opts.TelemetryEndpoint != "" {
		args = append(args, "--settings", claudeSettingsArg(opts.TelemetryEndpoint))
	}

	return args
}

// claudeSession is a running Claude Code subprocess in stream-json mode.
type claudeSession struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	scanner   *bufio.Scanner
	sessionID string
	// stderr holds the tail of claude's stderr, for the reason a refusal gives.
	stderr *stderrTail
	// wantMode is the --permission-mode this session asked for, or "".
	wantMode string
	// cleanup removes what starting the session left behind for the
	// process (srt's settings, when they had no session directory), once
	// the process has exited.
	cleanup func()
	waited  bool
	waitErr error
	// turnOpen is true from a message sent to claude until its result is
	// read: claude may be working on, or have queued, a turn nobody will
	// read. Close must not wait for one.
	turnOpen bool

	// effort is the reasoning effort claude reports it applies, once its
	// get_settings answer arrives. Init does not carry it.
	effort string
	// model is the model claude reports running: the system init event's,
	// then each main-thread API response's (see noteModel).
	model string
	// total_cost_usd is a running total for the process, not a per-turn
	// figure: each result carries the total so far, and a resumed session
	// may start from the total its transcript saved. costBase is the part of
	// that total already accounted for, so a turn's cost is the difference.
	costBase      float64
	costBaseKnown bool
	costSeen      bool
}

// wait reaps the claude process once; later calls return the first result.
func (s *claudeSession) wait() error {
	if s.cmd == nil {
		return nil
	}
	if !s.waited {
		s.waitErr = s.cmd.Wait()
		s.waited = true
		if s.cleanup != nil {
			s.cleanup()
		}
	}
	return s.waitErr
}

// ResumeToken returns the claude session id driving this conversation.
func (s *claudeSession) ResumeToken() string { return s.sessionID }

// stream-json input message shapes.
type claudeUserMsg struct {
	Type    string          `json:"type"`
	Message claudeUserInner `json:"message"`
}

type claudeUserInner struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// stream-json output event shapes.
// Unknown fields are silently ignored for graceful degradation on protocol
// changes — we use json.Decoder's default behaviour (skip unknown keys).
//
// Error and Result are held raw: decoding a field of an unexpected type would
// fail the whole event, and a dropped result event loses the end of the turn.
type claudeOutputEvent struct {
	Type    string           `json:"type"`
	Subtype string           `json:"subtype,omitempty"`
	Message *claudeOutputMsg `json:"message,omitempty"`
	// PermissionMode is the mode the system init event reports the session
	// actually runs in, which is not always the mode it was asked for.
	PermissionMode string `json:"permissionMode,omitempty"`
	// Model is the system init event's model: the one the session runs on,
	// with an alias such as "sonnet" already resolved to its id.
	Model string `json:"model,omitempty"`
	// ParentToolUseID is set on an assistant event from a subagent, which
	// may run on another model than the session's.
	ParentToolUseID *string      `json:"parent_tool_use_id,omitempty"`
	IsError         bool         `json:"is_error,omitempty"`
	Usage           *claudeUsage `json:"usage,omitempty"`
	// Error is set on an assistant event claude synthesised from an API
	// failure rather than received from the model, e.g. "authentication_failed".
	Error json.RawMessage `json:"error,omitempty"`
	// Result is the result event's summary text; on an error result it is the
	// failure message.
	Result json.RawMessage `json:"result,omitempty"`
	// TotalCostUSD is the result event's running cost for this process.
	TotalCostUSD json.RawMessage `json:"total_cost_usd,omitempty"`
	// Response is a control_response event's body.
	Response json.RawMessage `json:"response,omitempty"`
}

// claudeControlResponse is the body of a control_response event, holding the
// fields of the get_settings and get_usage answers ynh reads.
type claudeControlResponse struct {
	Subtype   string `json:"subtype"`
	RequestID string `json:"request_id"`
	Response  struct {
		Applied *struct {
			Effort *string `json:"effort"`
		} `json:"applied"`
		Session *struct {
			TotalCostUSD *float64 `json:"total_cost_usd"`
		} `json:"session"`
	} `json:"response"`
}

// rawFloat returns raw as a number and true when it is a JSON number.
func rawFloat(raw json.RawMessage) (float64, bool) {
	var f float64
	if len(raw) == 0 || json.Unmarshal(raw, &f) != nil {
		return 0, false
	}
	return f, true
}

// claudeAuthError is the error code claude puts on the assistant event it
// synthesises when it has no usable credentials.
const claudeAuthError = "authentication_failed"

// claudeAuthMarkers are the texts claude answers with when it has no usable
// credentials. They are a fallback for a claude that does not mark the turn as
// an error in its structured output; the structured signal is preferred.
var claudeAuthMarkers = []string{"Not logged in", "Invalid API key"}

// rawString returns raw as a string when it is a JSON string, else "".
func rawString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// hasClaudeAuthMarker reports whether text opens with one of claude's
// not-authenticated answers.
func hasClaudeAuthMarker(text string) bool {
	text = strings.TrimSpace(text)
	for _, m := range claudeAuthMarkers {
		if strings.HasPrefix(text, m) {
			return true
		}
	}
	return false
}

// claudeTurnError classifies a completed claude turn. It returns a
// *WorkerError when claude reported the turn as failed: an error result, or an
// assistant message claude synthesised from an API failure. As a fallback for
// a claude that marks neither, an unmetered turn whose text is one of claude's
// not-authenticated answers is an authentication failure too.
func claudeTurnError(ev claudeOutputEvent, apiError string, turn Turn) error {
	result := rawString(ev.Result)
	if ev.IsError || apiError != "" {
		msg := firstNonBlank(result, turn.Content, apiError, "turn failed")
		return &WorkerError{
			Backend: "claude",
			Message: msg,
			Auth:    apiError == claudeAuthError || hasClaudeAuthMarker(msg),
		}
	}
	if turn.Usage == (Usage{}) && hasClaudeAuthMarker(turn.Content) {
		return &WorkerError{Backend: "claude", Message: strings.TrimSpace(turn.Content), Auth: true}
	}
	return nil
}

type claudeOutputMsg struct {
	// ID is the API message id. Claude Code emits one assistant event per
	// content block, each repeating that message's usage.
	ID string `json:"id,omitempty"`
	// Model is the model that wrote this message, or claudeSyntheticModel
	// on a message claude made up itself.
	Model   string          `json:"model,omitempty"`
	Role    string          `json:"role"`
	Content []claudeContent `json:"content"`
	Usage   *claudeUsage    `json:"usage,omitempty"`
}

type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type claudeUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheTokens  int64 `json:"cache_read_input_tokens,omitempty"`
	// CacheCreationTokens is the total written to the cache. The usage
	// record also carries a cache_creation object splitting that total by
	// cache lifetime; it is not decoded, so the writes count once. A pointer,
	// so a record without the field reports no cache writes rather than zero.
	CacheCreationTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
}

// usage converts the record to a turn's usage.
func (u *claudeUsage) usage() Usage {
	out := Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheTokens: u.CacheTokens}
	if u.CacheCreationTokens != nil {
		out.CacheCreationTokens = *u.CacheCreationTokens
	}
	return out
}

// Send delivers a user-turn message to the worker via NDJSON.
func (s *claudeSession) Send(msg string) error {
	payload := claudeUserMsg{
		Type: "user",
		Message: claudeUserInner{
			Role:    "user",
			Content: msg,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	s.turnOpen = true
	return s.writeLine(append(data, '\n'))
}

// writeLine writes one NDJSON line to claude's stdin.
func (s *claudeSession) writeLine(data []byte) error {
	if _, err := s.stdin.Write(data); err != nil {
		// A write fails when claude has already exited, as it does when it
		// refuses to start. How it exited is the useful error.
		if exitErr := exitedWorkerError("claude", s.wait(), s.stderr); exitErr != io.EOF {
			return exitErr
		}
		return err
	}
	return nil
}

// applyControlResponse takes what ynh reads from claude's answers to the
// control requests sent at start.
func (s *claudeSession) applyControlResponse(raw json.RawMessage) {
	var r claudeControlResponse
	if len(raw) == 0 || json.Unmarshal(raw, &r) != nil || r.Subtype != "success" {
		// An older claude may not know the request. Nothing is reported.
		return
	}
	switch r.RequestID {
	case claudeSettingsRequest:
		if a := r.Response.Applied; a != nil && a.Effort != nil {
			s.effort = *a.Effort
		}
	case claudeUsageRequest:
		// Only before the first result: after it the base has been taken
		// from that result instead.
		if sess := r.Response.Session; sess != nil && sess.TotalCostUSD != nil && !s.costSeen {
			s.costBase = *sess.TotalCostUSD
			s.costBaseKnown = true
		}
	}
}

// recordCost turns a result's running total into this turn's cost.
func (s *claudeSession) recordCost(raw json.RawMessage, turn *Turn) {
	total, ok := rawFloat(raw)
	if !ok {
		return
	}
	s.costSeen = true
	if !s.costBaseKnown {
		// A resumed session whose starting total never arrived. This
		// result's total includes turns already counted before the resume,
		// so it cannot be split; it becomes the base and the turn reports
		// no cost rather than counting earlier turns twice.
		s.costBase, s.costBaseKnown = total, true
		return
	}
	cost := total - s.costBase
	if cost < 0 {
		// The running total was reset (claude's /clear does this), so the
		// whole of it is new.
		cost = total
	}
	s.costBase = total
	turn.CostUSD = cost
	turn.CostReported = true
}

// Next reads output events until the worker completes the current turn.
// Returns io.EOF when the subprocess exits cleanly.
func (s *claudeSession) Next() (Turn, error) {
	var turn Turn
	var contentBuf bytes.Buffer
	var apiError string
	// The assistant events' usage, the fallback for a result without its
	// own: one entry per API message, since every content-block event of a
	// message repeats the same usage. Events without an id each count.
	msgUsage := map[string]Usage{}
	var anonUsage Usage
	var assistantUsage, assistantCacheCreation bool

	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev claudeOutputEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// Unknown event shape — skip gracefully.
			continue
		}

		switch ev.Type {
		case "control_response":
			s.applyControlResponse(ev.Response)

		case "system":
			if ev.Subtype == "init" {
				s.noteModel(ev.Model)
			}
			if err := claudeModeMismatch(ev, s.wantMode); err != nil {
				// Stop it now: left running, it would work through the turn
				// in a mode nobody chose.
				if s.cmd != nil && s.cmd.Process != nil {
					_ = s.cmd.Process.Kill()
				}
				return Turn{}, err
			}

		case "assistant":
			if e := rawString(ev.Error); e != "" {
				apiError = e
			}
			if ev.Message != nil {
				if ev.ParentToolUseID == nil || *ev.ParentToolUseID == "" {
					s.noteModel(ev.Message.Model)
				}
				for _, block := range ev.Message.Content {
					if block.Type == "text" {
						contentBuf.WriteString(block.Text)
					}
				}
				if mu := ev.Message.Usage; mu != nil {
					assistantUsage = true
					assistantCacheCreation = assistantCacheCreation || mu.CacheCreationTokens != nil
					u := mu.usage()
					if ev.Message.ID == "" {
						anonUsage = addUsage(anonUsage, u)
					} else {
						msgUsage[ev.Message.ID] = u
					}
				}
			}

		case "result":
			// result signals end of this turn. Its usage is the turn's own
			// (per turn in a streaming-input session) and is authoritative;
			// adding the assistant events' usage to it counted every turn
			// at least twice.
			switch {
			case ev.Usage != nil:
				turn.UsageReported = true
				turn.CacheCreationReported = ev.Usage.CacheCreationTokens != nil
				turn.Usage = ev.Usage.usage()
			case assistantUsage:
				turn.UsageReported = true
				turn.CacheCreationReported = assistantCacheCreation
				turn.Usage = anonUsage
				for _, u := range msgUsage {
					turn.Usage = addUsage(turn.Usage, u)
				}
			}
			s.turnOpen = false
			turn.Content = contentBuf.String()
			if err := claudeTurnError(ev, apiError, turn); err != nil {
				return Turn{}, err
			}
			// Claude's usage record always carries cache_read_input_tokens.
			turn.CacheReported = turn.UsageReported
			turn.Effort = s.effort
			turn.Model = s.model
			s.recordCost(ev.TotalCostUSD, &turn)
			return turn, nil

			// All other types (system, tool_use, tool_result, stream_event, etc.)
			// are intentionally ignored — the loop driver doesn't need them.
		}
	}

	if err := s.scanner.Err(); err != nil {
		return Turn{}, fmt.Errorf("reading claude output: %w", err)
	}
	// stdout closed: claude has exited. A non-zero exit before any result is
	// claude refusing to run, and its stderr says why.
	return Turn{}, exitedWorkerError("claude", s.wait(), s.stderr)
}

// claudeSyntheticModel is the model claude names on an assistant message it
// wrote itself rather than received from the API, such as its "Not logged in"
// answer. No model ran, so it is never reported.
const claudeSyntheticModel = "<synthetic>"

// noteModel records a model claude reports. The init event names the model
// the session starts on. A main-thread assistant message names the model
// that wrote it, which is the session's unless claude switched mid-run (a
// fallback model taking over, say), and then the message is the better
// witness of what ran, so the latest one wins. Subagent messages are not
// passed here: a subagent may run on a smaller model while the session's
// model stays the run's.
func (s *claudeSession) noteModel(model string) {
	if model = strings.TrimSpace(model); model != "" && model != claudeSyntheticModel {
		s.model = model
	}
}

// addUsage returns the sum of two usage records.
func addUsage(a, b Usage) Usage {
	return Usage{
		InputTokens:         a.InputTokens + b.InputTokens,
		OutputTokens:        a.OutputTokens + b.OutputTokens,
		CacheTokens:         a.CacheTokens + b.CacheTokens,
		CacheCreationTokens: a.CacheCreationTokens + b.CacheCreationTokens,
	}
}

// claudeModeMismatch reports a session that did not start in the permission
// mode it asked for. Claude does not fail when a setting disables the mode: a
// managed permissions.disableBypassPermissionsMode downgrades a
// bypassPermissions session to another mode and carries on, so the grant the
// operator made would silently not apply.
func claudeModeMismatch(ev claudeOutputEvent, want string) error {
	if want == "" || ev.Subtype != "init" || ev.PermissionMode == "" || ev.PermissionMode == want {
		return nil
	}
	return &WorkerError{
		Backend: "claude",
		Message: fmt.Sprintf(
			"asked for --permission-mode %s but the session started in %q; a managed or user setting "+
				"(such as permissions.disableBypassPermissionsMode) overrides it, so --auto-approve does not apply",
			want, ev.PermissionMode),
	}
}

// Close ends the claude subprocess. With no turn open it closes stdin and
// lets claude exit, as it does on EOF. With a turn open (a message was sent
// and its result never read) claude would finish that turn before it read the
// EOF, acting on a request the run has already given up on, so it is
// terminated instead.
func (s *claudeSession) Close() error {
	if s.waited {
		// Next already reaped the process and reported how it ended.
		return nil
	}
	closeErr := s.stdin.Close()
	err := reapWorker(s.cmd, s.wait, closeErr == nil && !s.turnOpen)
	if closeErr != nil {
		return closeErr
	}
	return err
}
