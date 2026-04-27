package migration

import (
	"errors"
	"testing"
)

type fakeMigrator struct {
	applies     bool
	runErr      error
	ran         bool
	description string
}

func (f *fakeMigrator) Applies(dir string) bool { return f.applies }
func (f *fakeMigrator) Run(dir string) error {
	f.ran = true
	return f.runErr
}
func (f *fakeMigrator) Description() string { return f.description }

func TestChain_Run_OnlyAppliesMatching(t *testing.T) {
	m1 := &fakeMigrator{applies: true, description: "m1"}
	m2 := &fakeMigrator{applies: false, description: "m2"}
	m3 := &fakeMigrator{applies: true, description: "m3"}

	c := Chain{m1, m2, m3}
	applied, err := c.Run("/some/dir")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !m1.ran {
		t.Error("m1 should have run")
	}
	if m2.ran {
		t.Error("m2 should not have run")
	}
	if !m3.ran {
		t.Error("m3 should have run")
	}

	want := []string{"m1", "m3"}
	if len(applied) != 2 || applied[0] != want[0] || applied[1] != want[1] {
		t.Errorf("applied = %v, want %v", applied, want)
	}
}

func TestChain_Run_StopsOnError(t *testing.T) {
	m1 := &fakeMigrator{applies: true, description: "m1"}
	m2 := &fakeMigrator{applies: true, runErr: errors.New("boom"), description: "m2"}
	m3 := &fakeMigrator{applies: true, description: "m3"}

	c := Chain{m1, m2, m3}
	applied, err := c.Run("/some/dir")
	if err == nil {
		t.Fatal("expected error")
	}
	if m3.ran {
		t.Error("m3 should not run after m2 errors")
	}
	if len(applied) != 1 || applied[0] != "m1" {
		t.Errorf("applied = %v, want [m1]", applied)
	}
}

func TestDefaultChain_Order(t *testing.T) {
	c := DefaultChain()
	// harness_format must precede harness_storage so installed.json is readable
	// when the storage migrator runs.
	var sawFormat, sawStorage bool
	for _, m := range c {
		switch m.(type) {
		case HarnessFormatMigrator:
			sawFormat = true
			if sawStorage {
				t.Error("HarnessFormatMigrator must come before HarnessStorageMigrator")
			}
		case HarnessStorageMigrator:
			sawStorage = true
			if !sawFormat {
				t.Error("HarnessStorageMigrator must come after HarnessFormatMigrator")
			}
		}
	}
	if !sawFormat || !sawStorage {
		t.Error("DefaultChain missing expected migrators")
	}
}

func TestFormatChain_ExcludesStorage(t *testing.T) {
	c := FormatChain()
	for _, m := range c {
		if _, ok := m.(HarnessStorageMigrator); ok {
			t.Error("FormatChain should not include HarnessStorageMigrator")
		}
	}
}
