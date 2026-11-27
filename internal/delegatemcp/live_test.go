package delegatemcp

import (
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
)

// Every result of a task for a live session names where it went.
func TestResultNamesRouteSessionAndCwd(t *testing.T) {
	now := time.Now()
	rec := &delegation.Record{Handle: "dlg-0123456789abcdef", Delegate: "local/eno", Status: delegation.StatusQueued,
		Route: delegation.RouteLive, Session: "eno-1", SessionCwd: "/work", QueuedAt: &now}
	r := (&Server{}).resultOf(rec)
	if r.State != "submitted" || r.Route != "live" || r.Session != "eno-1" || r.Cwd != "/work" || !strings.Contains(r.Message, "queued at live session eno-1") {
		t.Fatalf("queued result = %+v", r)
	}
	if r.final() {
		t.Error("a queued task is not final")
	}
	rec.Status, rec.Error = delegation.StatusRejected, "busy"
	r = (&Server{}).resultOf(rec)
	if r.State != "rejected" || !r.final() || !strings.Contains(r.Message, "on_live headless") {
		t.Fatalf("rejected result = %+v", r)
	}
	if taskView(rec, "")["status"] != "failed" {
		t.Error("a rejected task is a failed MCP task")
	}
}
