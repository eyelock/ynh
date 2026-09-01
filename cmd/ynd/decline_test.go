package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A command that was asked to do something, asked permission, was told no, and
// then exited 0 is indistinguishable from one that did the work.
//
// `ynd migrate` did exactly that. It printed "Aborted." and returned nil. Worse
// than the interactive case: promptAction returns its refusing answer on EOF,
// so every piped or scripted invocation without -y took the refusing branch and
// reported success for having migrated nothing.
//
// These tests only ever point migrate at a directory under t.TempDir(). The
// dangerous paths go to refuseToMigrate, which decides and never deletes, per
// .claude/rules/destructive-operations.md.
func TestMigrateDeclinedExitsNonZero(t *testing.T) {
	legacy := `{"$schema":"https://eyelock.github.io/ynh/schema/harness.schema.json","name":"legacy","version":"0.1.0"}`

	newTree := func(t *testing.T) (root, harness string) {
		t.Helper()
		root = t.TempDir()
		harness = filepath.Join(root, "harness-1")
		if err := os.MkdirAll(harness, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(harness, ".harness.json"), []byte(legacy), 0o644); err != nil {
			t.Fatal(err)
		}
		return root, harness
	}

	// CI and YNH_YES are skip-confirm inputs. Clear them so the prompt is
	// actually reached: leaving them to the ambient environment is how
	// TestYnd_Migrate came to pass in CI and fail on a developer machine.
	clearSkipConfirm := func(t *testing.T) {
		t.Helper()
		t.Setenv("CI", "")
		t.Setenv("YNH_YES", "")
	}

	t.Run("declining returns errDeclined and changes nothing", func(t *testing.T) {
		clearSkipConfirm(t)
		root, harness := newTree(t)

		restore := promptActionFunc
		t.Cleanup(func() { promptActionFunc = restore })
		// Exactly what promptActionImpl does on EOF or a bare Enter.
		promptActionFunc = func(_ string, choices ...string) string { return choices[0] }

		err := cmdMigrate([]string{root})
		if !errors.Is(err, errDeclined) {
			t.Fatalf("declining migrate returned %v, want errDeclined", err)
		}
		if _, statErr := os.Stat(filepath.Join(harness, ".harness.json")); statErr != nil {
			t.Errorf("declined migrate removed the legacy file anyway: %v", statErr)
		}
		if _, statErr := os.Stat(filepath.Join(harness, ".ynh-plugin", "plugin.json")); statErr == nil {
			t.Error("declined migrate wrote the new layout anyway")
		}
	})

	t.Run("consenting migrates and returns nil", func(t *testing.T) {
		clearSkipConfirm(t)
		root, harness := newTree(t)

		restore := promptActionFunc
		t.Cleanup(func() { promptActionFunc = restore })
		promptActionFunc = func(_ string, _ ...string) string { return "y" }

		if err := cmdMigrate([]string{root}); err != nil {
			t.Fatalf("consented migrate failed: %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(harness, ".ynh-plugin", "plugin.json")); statErr != nil {
			t.Errorf("consented migrate did not write the new layout: %v", statErr)
		}
	})

	// --dry-run reports and changes nothing by design. That is the command
	// doing its job, not the operator refusing, so it stays zero.
	t.Run("dry run stays zero", func(t *testing.T) {
		clearSkipConfirm(t)
		root, harness := newTree(t)

		if err := cmdMigrate([]string{"--dry-run", root}); err != nil {
			t.Fatalf("--dry-run returned %v, want nil", err)
		}
		if _, statErr := os.Stat(filepath.Join(harness, ".harness.json")); statErr != nil {
			t.Errorf("--dry-run changed the tree: %v", statErr)
		}
	})

	// Nothing to migrate is a successful no-op, not a decline.
	t.Run("nothing to migrate stays zero", func(t *testing.T) {
		clearSkipConfirm(t)
		if err := cmdMigrate([]string{t.TempDir()}); err != nil {
			t.Fatalf("migrating an empty tree returned %v, want nil", err)
		}
	})
}

// errDeclined must stay distinguishable from an ordinary failure, otherwise a
// caller cannot tell "you said no" from "it broke" and the sentinel earns
// nothing over a plain error.
func TestDeclinedIsDistinguishableFromFailure(t *testing.T) {
	d := declined("Aborted. Nothing was migrated.")
	if !errors.Is(d, errDeclined) {
		t.Error("declined() does not match errDeclined")
	}
	if errors.Is(errors.New("disk on fire"), errDeclined) {
		t.Error("an ordinary error matched errDeclined")
	}
	if got, want := d.Error(), "Aborted. Nothing was migrated."; got != want {
		t.Errorf("message is %q, want %q; it is shown to the operator verbatim", got, want)
	}
}
