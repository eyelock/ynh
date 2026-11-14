package main

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
)

// endpointSetup installs a trusted delegate "reviewer" and returns a parent
// whose delegates_to has one entry per transport given.
func endpointSetup(t *testing.T, transports ...string) (*harness.Harness, trustStore) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	// Whatever a test leaves in the environment goes back as it was.
	for _, k := range []string{"YNH_DELEGATES", "YNH_DELEGATE_REVIEWER_CARD", "YNH_DELEGATE_REVIEWER_AUTH", "YNH_DELEGATE_STALE_CARD", "A2ACLI_AGENT_CARD", "A2ACLI_AUTH", "A2ACLI_CREDENTIAL_HELPER"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	root := t.TempDir()
	writeIncludedHarness(t, root, "reviewer", mcpFields("r"))
	id := installLocalHarness(t, filepath.Join(root, "reviewer"), "reviewer")
	parent := &harness.Harness{Name: "parent", Dir: filepath.Join(root, "parent")}
	for _, tr := range transports {
		parent.DelegatesTo = append(parent.DelegatesTo, harness.Delegate{GitSource: harness.GitSource{Local: "../reviewer"}, Transport: tr})
	}
	return parent, accepted(t, id)
}

func TestRunOpensAnEndpointForEachA2ADelegateAndSetsTheEnvironment(t *testing.T) {
	parent, store := endpointSetup(t, "a2a")
	t.Setenv("YNH_DELEGATE_STALE_CARD", "http://stale") // from an enclosing session
	var stderr bytes.Buffer
	closeFn, err := openDelegateEndpointsWith(parent, "local/parent", t.TempDir(), &store, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	card, auth := os.Getenv("YNH_DELEGATE_REVIEWER_CARD"), os.Getenv("YNH_DELEGATE_REVIEWER_AUTH")
	if os.Getenv("YNH_DELEGATES") != "reviewer" || !strings.HasPrefix(card, "http://127.0.0.1:") || !strings.HasPrefix(auth, "Bearer ") {
		t.Fatalf("environment: %q %q %q", os.Getenv("YNH_DELEGATES"), card, auth)
	}
	if os.Getenv("A2ACLI_AGENT_CARD") != card || os.Getenv("A2ACLI_AUTH") != auth {
		t.Fatal("with one delegate the a2a CLI's own variables must carry the same values")
	}
	if _, ok := os.LookupEnv("YNH_DELEGATE_STALE_CARD"); ok {
		t.Fatal("an enclosing session's endpoint was left in the environment")
	}
	resp, err := http.Get(card + "/.well-known/agent-card.json")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("card: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	closeFn()
	if resp, err := http.Get(card + "/.well-known/agent-card.json"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("the endpoint is still open after the run ended")
	}
}

func TestRunOpensNoEndpointForNativeDelegatesAndClearsStaleOnes(t *testing.T) {
	parent, store := endpointSetup(t, "native")
	t.Setenv("YNH_DELEGATES", "stale")
	t.Setenv("A2ACLI_AUTH", "Bearer stale")
	closeFn, err := openDelegateEndpointsWith(parent, "local/parent", t.TempDir(), &store, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	for _, k := range []string{"YNH_DELEGATES", "A2ACLI_AUTH", "YNH_DELEGATE_REVIEWER_CARD"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s=%q is set with no a2a delegate", k, v)
		}
	}
}

func TestRunWithTwoEntriesOfOneNameIsRefusedNamingBoth(t *testing.T) {
	parent, store := endpointSetup(t, "a2a", "a2a")
	_, err := openDelegateEndpointsWith(parent, "local/parent", t.TempDir(), &store, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "REVIEWER") || strings.Count(err.Error(), "/reviewer") < 2 {
		t.Fatalf("want a refusal naming both entries, got %v", err)
	}
	if _, ok := os.LookupEnv("YNH_DELEGATES"); ok {
		t.Fatal("a refused run left delegate environment behind")
	}
}

func TestRunRefusesADelegateThatIsNotTrusted(t *testing.T) {
	parent, _ := endpointSetup(t, "a2a")
	_, err := openDelegateEndpointsWith(parent, "local/parent", t.TempDir(), &trustStore{Harnesses: map[string]trustRecord{}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "ynh trust") {
		t.Fatalf("want a refusal naming ynh trust, got %v", err)
	}
}

func TestRunSetsACredentialHelperThatGivesTheEndpointItsToken(t *testing.T) {
	parent, store := endpointSetup(t, "a2a")
	// A helper of an enclosing session is replaced, never kept.
	t.Setenv("A2ACLI_CREDENTIAL_HELPER", "127.0.0.1=/tmp/ynh-a2a-credentials-old/helper")
	closeFn, err := openDelegateEndpointsWith(parent, "local/parent", t.TempDir(), &store, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	card, auth := os.Getenv("YNH_DELEGATE_REVIEWER_CARD"), os.Getenv("YNH_DELEGATE_REVIEWER_AUTH")
	scope, helper, ok := strings.Cut(os.Getenv("A2ACLI_CREDENTIAL_HELPER"), "=")
	if !ok || scope != "127.0.0.1" || strings.Contains(helper, "-old/") {
		t.Fatalf("A2ACLI_CREDENTIAL_HELPER = %q", os.Getenv("A2ACLI_CREDENTIAL_HELPER"))
	}
	cmd := exec.Command(helper, "get")
	cmd.Stdin = strings.NewReader(`{"uri":"` + card + `/a2a"}`)
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), auth) {
		t.Fatalf("helper answered %q (%v), want the header %q", out, err, auth)
	}
	closeFn()
	if _, err := os.Stat(helper); err == nil {
		t.Error("the helper was left behind after the run ended")
	}
}

func TestRunWithNoA2ADelegateClearsItsOwnStaleCredentialHelperButKeepsTheUsers(t *testing.T) {
	parent, store := endpointSetup(t, "native")
	t.Setenv("A2ACLI_CREDENTIAL_HELPER", "127.0.0.1=/tmp/ynh-a2a-credentials-old/helper")
	closeFn, err := openDelegateEndpointsWith(parent, "local/parent", t.TempDir(), &store, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	closeFn()
	if v, ok := os.LookupEnv("A2ACLI_CREDENTIAL_HELPER"); ok {
		t.Errorf("a stale helper of an enclosing session was kept: %q", v)
	}
	t.Setenv("A2ACLI_CREDENTIAL_HELPER", "example.com=/usr/local/bin/corp-helper")
	closeFn, err = openDelegateEndpointsWith(parent, "local/parent", t.TempDir(), &store, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	closeFn()
	if os.Getenv("A2ACLI_CREDENTIAL_HELPER") != "example.com=/usr/local/bin/corp-helper" {
		t.Error("the user's own credential helper was cleared")
	}
}
