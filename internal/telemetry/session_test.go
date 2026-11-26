package telemetry

import (
	"strings"
	"testing"
)

func TestWithSessionID(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "ynh.session.id=abc"},
		{"deployment.environment.name=dev", "deployment.environment.name=dev,ynh.session.id=abc"},
		{"a=b,,c=d", "a=b,c=d,ynh.session.id=abc"},
		// An inherited launch's id is replaced: a delegate's session is its own.
		{"ynh.session.id=old,a=b", "a=b,ynh.session.id=abc"},
		{"ynh%2Esession.id=old", "ynh.session.id=abc"},
	} {
		if got := withSessionID(tc.in, "abc"); got != tc.want {
			t.Errorf("withSessionID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := withSessionID("", "a b,c=d"); got != "ynh.session.id=a%20b%2Cc=d" {
		t.Errorf("the id is percent-encoded, got %q", got)
	}
}

func TestSessionIDsAreNewPerLaunch(t *testing.T) {
	a, b := NewSessionID(), NewSessionID()
	if a == b || len(a) != 32 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("ids %q %q", a, b)
	}
}

func TestSession_OffGivesNoEnvironment(t *testing.T) {
	clearTelemetryEnv(t)
	tel := Setup("test", Options{}, nil)
	defer tel.Shutdown()
	s := tel.StartSession()
	if env := s.Env(func(string) string { return "x=y" }); env != nil {
		t.Errorf("off, env = %v", env)
	}
	if s.ApplyEnv() {
		t.Error("off, ApplyEnv set something")
	}
	s.Finish(SessionOutcomeExited, true)
}
