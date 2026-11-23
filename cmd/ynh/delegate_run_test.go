package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/delegation"
)

func TestDelegateRunArgs(t *testing.T) {
	tests := []struct {
		name string
		rec  delegation.Record
		want []string
	}{
		{"task alone", delegation.Record{Delegate: "local/rev", Task: "review it"},
			[]string{"run", "local/rev", "--", "review it"}},
		{"focus and model", delegation.Record{Delegate: "local/rev", Task: "go", Focus: "deep", Model: "opus"},
			[]string{"run", "local/rev", "--focus", "deep", "--", "go", "--model=opus"}},
		{"profile", delegation.Record{Delegate: "local/rev", Task: "go", Profile: "ci"},
			[]string{"run", "local/rev", "--profile", "ci", "--", "go"}},
		{"a task that starts with a dash is still the prompt", delegation.Record{Delegate: "local/rev", Task: "--help me"},
			[]string{"run", "local/rev", "--", " --help me"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := delegateRunArgs(&tt.rec)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("args = %q, want %q", got, tt.want)
			}
			// The run must read them back as the task and the model.
			ra := parseRunArgs(got[1:])
			if ra.HarnessName != tt.rec.Delegate || strings.TrimSpace(ra.Prompt) != tt.rec.Task || ra.Interactive {
				t.Errorf("ynh run reads %+v", ra)
			}
			if tt.rec.Model != "" && !reflect.DeepEqual(ra.VendorArgs, []string{"--model=" + tt.rec.Model}) {
				t.Errorf("vendor args = %q", ra.VendorArgs)
			}
		})
	}
}

func TestDelegateLauncherDropsTheParentsSelectors(t *testing.T) {
	t.Setenv("YNH_FOCUS", "the-parents-focus")
	t.Setenv("YNH_HARNESS_FILE", "/elsewhere")
	t.Setenv("YNH_HOME", t.TempDir())
	rec := &delegation.Record{Handle: "dlg-1", Parent: "local/p", Delegate: "local/d", Folder: "/x", Task: "t"}
	cmd := delegateLauncher(rec)
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "YNH_FOCUS=") || strings.HasPrefix(kv, "YNH_HARNESS_FILE=") {
			t.Errorf("the delegate inherits %s", kv)
		}
	}
	if !contains(cmd.Env, "YNH_DELEGATION_HANDLE=dlg-1") {
		t.Errorf("env lacks the handle: %v", cmd.Env)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestDelegateRunStartRefusesWhatIsNotTrusted(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	root := t.TempDir()
	writeIncludedHarness(t, root, "reviewer", mcpFields("r"))
	installLocalHarness(t, filepath.Join(root, "reviewer"), "reviewer")
	writeIncludedHarness(t, root, "parent", `,"delegates_to":[{"git":"..","path":"reviewer"}]`)
	installLocalHarness(t, filepath.Join(root, "parent"), "parent")

	var out bytes.Buffer
	err := cmdDelegateRunTo([]string{"start", "--parent", "local/parent", "--entry", "0", "--task", "go", "--folder", t.TempDir()}, &out)
	if err == nil || !strings.Contains(err.Error(), "ynh trust") {
		t.Fatalf("an untrusted delegate must be refused naming ynh trust, got %v", err)
	}
	if entries, _ := os.ReadDir(delegation.Dir()); len(entries) != 0 {
		t.Errorf("a refused start left records behind: %v", entries)
	}

	err = cmdDelegateRunTo([]string{"start", "--parent", "local/parent", "--entry", "3", "--task", "go"}, &out)
	if err == nil || !strings.Contains(err.Error(), "no delegates_to entry 3") {
		t.Errorf("an entry out of range: %v", err)
	}
}

func TestDelegateRunUsage(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{
		{}, {"bogus"}, {"start"}, {"start", "--parent"}, {"start", "--bogus", "x"},
		{"start", "--parent", "p", "--entry", "-1", "--task", "t"}, {"status"}, {"cancel", "--record"}, {"supervise"},
	} {
		if err := cmdDelegateRunTo(args, &out); err == nil {
			t.Errorf("%q: want a usage error", args)
		}
	}
}

func TestDelegateRunStatusAndCancelReadTheRecord(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	folder := t.TempDir()
	path := delegation.RecordPath("local/p", folder, "dlg-x")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"handle":"dlg-x","status":"succeeded","history":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdDelegateRunTo([]string{"status", "--record", path}, &out); err != nil || !strings.Contains(out.String(), `"dlg-x"`) {
		t.Fatalf("status: %v %s", err, out.String())
	}
	out.Reset()
	// A final delegation is left as it is.
	if err := cmdDelegateRunTo([]string{"cancel", "--record", path}, &out); err != nil || !strings.Contains(out.String(), `"succeeded"`) {
		t.Fatalf("cancel: %v %s", err, out.String())
	}
}
