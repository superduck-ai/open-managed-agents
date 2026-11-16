package environments

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerArtifactLinkRejectsDirectory(t *testing.T) {
	entry := t.TempDir()
	if err := runManagerArtifactShell(t, managerArtifactLinkCommand("/new/manager", entry)); err == nil {
		t.Fatal("replaced directory entry")
	}
	contents, err := os.ReadDir(entry)
	if err != nil || len(contents) != 0 {
		t.Fatalf("directory changed: %v %v", contents, err)
	}
}

func TestManagerArtifactLinkPreservesEntryAfterRenameFailure(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old")
	entry := filepath.Join(dir, "entry")
	if err := os.WriteFile(old, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, entry); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "mv"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := runManagerArtifactShell(t, managerArtifactLinkCommand("/new/manager", entry)); err == nil {
		t.Fatal("accepted failed rename")
	}
	if got, err := os.Readlink(entry); err != nil || got != old {
		t.Fatalf("entry changed: %s %v", got, err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".environment-manager-link.*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("staging leaked: %v %v", matches, err)
	}
}

func TestManagerArtifactLinkSwitchesEntryAndPreservesImageBinary(t *testing.T) {
	for _, kind := range []string{"symlink", "regular", "missing"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			old := filepath.Join(dir, "image-manager")
			target := filepath.Join(dir, "new manager with 'quote'")
			entry := filepath.Join(dir, "manager with 'quote'")
			if err := os.WriteFile(old, []byte("original"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("new"), 0o755); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if err := os.Symlink(old, entry); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "regular" {
				if err := os.WriteFile(entry, []byte("old entry"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := runManagerArtifactShell(t, managerArtifactLinkCommand(target, entry)); err != nil {
				t.Fatal(err)
			}
			if got, err := os.Readlink(entry); err != nil || got != target {
				t.Fatalf("link=%q error=%v", got, err)
			}
			if got, err := os.ReadFile(old); err != nil || string(got) != "original" {
				t.Fatalf("image changed: %q %v", got, err)
			}
			if got, err := os.ReadFile(entry); err != nil || string(got) != "new" {
				t.Fatalf("entry=%q error=%v", got, err)
			}
		})
	}
}
