package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

// Session is one `ynh run` launch of a vendor CLI as a unit of work: a
// ynh.session span with a started event when it begins and a finished event
// when it ends, for every vendor. It is a Run under other names, so the
// destination order, the spool, the late look for the spool and the resource
// are agent run's. ynh.run stays agent run's alone.
type Session struct {
	*Run
	id string
}

// NewSessionID returns a random id for a launch.
func NewSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// StartSession joins the trace named by TRACEPARENT and TRACESTATE, or starts
// one, opens the launch's span and emits ynh.session.started, flushed before
// it returns. The id is a new random one for each launch; attrs describe the
// launch as requested and go on the event and the span.
func (t *Telemetry) StartSession(attrs ...attribute.KeyValue) *Session {
	id := NewSessionID()
	all := append([]attribute.KeyValue{AttrSessionID.String(id)}, attrs...)
	return &Session{Run: t.start(sessionKind, all), id: id}
}

// ID is the launch's session id.
func (s *Session) ID() string { return s.id }

// Env is what every process the launch starts should carry, or nil when
// telemetry is off, so the vendor's environment is then exactly what it was:
//
//   - TRACEPARENT (and TRACESTATE, if any) naming the ynh.session span
//   - OTEL_RESOURCE_ATTRIBUTES: the process's current value, an operator's
//     kept, with ynh.session.id appended and percent-encoded
//
// getenv reads the current environment, as os.Getenv does.
func (s *Session) Env(getenv func(string) string) []string {
	st := s.t.state()
	if !st.active {
		return nil
	}
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	var env []string
	if v := carrier.Get("traceparent"); v != "" {
		env = append(env, "TRACEPARENT="+v)
	}
	if v := carrier.Get("tracestate"); v != "" {
		env = append(env, "TRACESTATE="+v)
	}
	return append(env, resourceEnv+"="+withSessionID(getenv(resourceEnv), s.id))
}

// ApplyEnv sets Env in this process's environment, which the vendor CLI and
// everything else `ynh run` starts inherit. It reports whether it set
// anything. TRACESTATE is removed when the launch has none, so the vendor
// never pairs the launch's TRACEPARENT with a stale state.
func (s *Session) ApplyEnv() bool {
	env := s.Env(os.Getenv)
	if env == nil {
		return false
	}
	hasState := false
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if k == "TRACESTATE" {
			hasState = true
		}
		_ = os.Setenv(k, v)
	}
	if !hasState {
		_ = os.Unsetenv("TRACESTATE")
	}
	return true
}

// withSessionID appends ynh.session.id=<id> to an OTEL_RESOURCE_ATTRIBUTES
// value, keeping every pair already there but an earlier ynh.session.id of an
// inherited launch (a delegate's session is its own).
func withSessionID(raw, id string) string {
	var kept []string
	for _, pair := range strings.Split(raw, ",") {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		key, _, _ := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if k, err := url.PathUnescape(key); err == nil {
			key = k
		}
		if key == string(AttrSessionID) {
			continue
		}
		kept = append(kept, pair)
	}
	kept = append(kept, url.PathEscape(string(AttrSessionID))+"="+url.PathEscape(id))
	return strings.Join(kept, ",")
}
