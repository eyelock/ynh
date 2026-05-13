package harness

import (
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestIsLocalSource_Nil(t *testing.T) {
	if IsLocalSource(nil) {
		t.Error("nil record should not be local")
	}
}

func TestIsLocalSource_EmptySource(t *testing.T) {
	ins := &plugin.InstalledJSON{SourceType: "local", Source: ""}
	if IsLocalSource(ins) {
		t.Error("empty Source should not be classified local")
	}
}

func TestIsLocalSource_Local(t *testing.T) {
	ins := &plugin.InstalledJSON{SourceType: "local", Source: "/some/path"}
	if !IsLocalSource(ins) {
		t.Error("source_type=local with Source should be local")
	}
}

func TestIsLocalSource_Source(t *testing.T) {
	ins := &plugin.InstalledJSON{SourceType: "source", Source: "/some/path"}
	if !IsLocalSource(ins) {
		t.Error("source_type=source with Source should be local")
	}
}

func TestIsLocalSource_Git(t *testing.T) {
	ins := &plugin.InstalledJSON{SourceType: "git", Source: "github.com/x"}
	if IsLocalSource(ins) {
		t.Error("source_type=git should not be local")
	}
}

func TestIsLocalSource_Registry(t *testing.T) {
	ins := &plugin.InstalledJSON{SourceType: "registry", Source: "x"}
	if IsLocalSource(ins) {
		t.Error("source_type=registry should not be local")
	}
}

func TestLoadInstalledRecord_FromPointer(t *testing.T) {
	overrideHarnessesDir(t)
	id := "local/h"
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	ptr := &Pointer{
		ID:            id,
		Name:          "h",
		InstalledJSON: plugin.InstalledJSON{SourceType: "local", Source: dir},
	}
	if err := SavePointerByID(ptr); err != nil {
		t.Fatal(err)
	}
	h := &Harness{Dir: dir, Name: "h"}

	got, err := LoadInstalledRecord(id, h)
	if err != nil {
		t.Fatalf("LoadInstalledRecord: %v", err)
	}
	if got == nil || got.SourceType != "local" || got.Source != dir {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestLoadInstalledRecord_FromDisk(t *testing.T) {
	overrideHarnessesDir(t)
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	ins := &plugin.InstalledJSON{SourceType: "git", Source: "github.com/x"}
	if err := plugin.SaveInstalledJSON(dir, ins); err != nil {
		t.Fatal(err)
	}
	h := &Harness{Dir: dir, Name: "h"}

	got, err := LoadInstalledRecord("github.com/x/h", h)
	if err != nil {
		t.Fatalf("LoadInstalledRecord: %v", err)
	}
	if got == nil || got.SourceType != "git" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestLoadInstalledRecord_NoRecord(t *testing.T) {
	overrideHarnessesDir(t)
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	h := &Harness{Dir: dir, Name: "h"}

	got, err := LoadInstalledRecord("github.com/x/h", h)
	if err != nil {
		t.Fatalf("LoadInstalledRecord: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil record, got %+v", got)
	}
}

func TestSaveInstalledRecord_PointerForm(t *testing.T) {
	overrideHarnessesDir(t)
	id := "local/h"
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	ptr := &Pointer{
		ID:            id,
		Name:          "h",
		InstalledJSON: plugin.InstalledJSON{SourceType: "local", Source: dir},
	}
	if err := SavePointerByID(ptr); err != nil {
		t.Fatal(err)
	}
	h := &Harness{Dir: dir, Name: "h"}

	updated := &plugin.InstalledJSON{SourceType: "local", Source: dir, Ref: "v2"}
	if err := SaveInstalledRecord(id, h, updated); err != nil {
		t.Fatalf("SaveInstalledRecord: %v", err)
	}

	reread, err := LoadPointerByID(id)
	if err != nil || reread == nil {
		t.Fatalf("reread pointer: %v / %v", err, reread)
	}
	if reread.Ref != "v2" {
		t.Errorf("ref not persisted in pointer: %+v", reread)
	}
}

func TestSaveInstalledRecord_TreeForm(t *testing.T) {
	overrideHarnessesDir(t)
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	h := &Harness{Dir: dir, Name: "h"}

	ins := &plugin.InstalledJSON{SourceType: "git", Source: "github.com/x", Ref: "v3"}
	if err := SaveInstalledRecord("github.com/x/h", h, ins); err != nil {
		t.Fatalf("SaveInstalledRecord: %v", err)
	}

	reread, err := plugin.LoadInstalledJSON(dir)
	if err != nil {
		t.Fatalf("LoadInstalledJSON: %v", err)
	}
	if reread.Ref != "v3" {
		t.Errorf("ref not persisted on disk: %+v", reread)
	}
}
