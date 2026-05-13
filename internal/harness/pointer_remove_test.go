package harness

import (
	"os"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestRemovePointerByID_RemovesExisting(t *testing.T) {
	overrideHarnessesDir(t)
	id := "local/h"
	ptr := &Pointer{
		ID:            id,
		Name:          "h",
		InstalledJSON: plugin.InstalledJSON{SourceType: "local", Source: "/some/path"},
	}
	if err := SavePointerByID(ptr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PointerPathByID(id)); err != nil {
		t.Fatalf("pointer should exist before remove: %v", err)
	}

	if err := RemovePointerByID(id); err != nil {
		t.Fatalf("RemovePointerByID: %v", err)
	}
	if _, err := os.Stat(PointerPathByID(id)); !os.IsNotExist(err) {
		t.Errorf("pointer should be gone after remove, got: %v", err)
	}
}

func TestRemovePointerByID_MissingIsNoop(t *testing.T) {
	overrideHarnessesDir(t)
	if err := RemovePointerByID("local/never-existed"); err != nil {
		t.Errorf("removing missing pointer should be no-op, got: %v", err)
	}
}
