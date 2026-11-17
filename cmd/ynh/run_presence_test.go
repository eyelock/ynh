//go:build unix

package main

import (
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/presence"
)

// A run is listed by `ynh ps` while its vendor session runs, and marked ended
// once it exits.
func TestRunRecordsPresenceForTheLifeOfTheSession(t *testing.T) {
	isolateRun(t)
	work := t.TempDir()
	harnessDir := filepath.Join(work, "h")
	writeSkillHarness(t, harnessDir, "h")
	t.Chdir(work)

	var during []presence.Entry
	stubLaunch(t, func(string, runArgs) error {
		var err error
		during, err = presence.List(false)
		return err
	})
	if err := cmdRun([]string{harnessDir, "-v", "claude"}); err != nil {
		t.Fatal(err)
	}
	if len(during) != 1 || during[0].Status != presence.StatusLive || during[0].Vendor != "claude" || during[0].Mode != presence.ModeInteractive {
		t.Fatalf("during the run: %+v", during)
	}
	if live, _ := presence.List(false); len(live) != 0 {
		t.Errorf("a finished run is still listed: %+v", live)
	}
	all, _ := presence.List(true)
	if len(all) != 1 || all[0].Status != presence.StatusEnded {
		t.Errorf("history: %+v", all)
	}
}
